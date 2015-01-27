// Copyright (c) 2015 RightScale, Inc. - see LICENSE

package main

//===== RightScale HTTP Requests

// This file implements the various HTTPS requests used to communicate with the RightScale
// API (V1.5 for now) through the RLL proxy or possibly directly. It is primarily a convenience
// layer on top of the std Go http client that provides a number of features, including:
// - keeping track of the global session auth cookie
// - adding the API version header
// - offering some logging capability

// The client has two ways to authenticate. The first is to read the RightLink10 proxy
// secret file in /var/run/rll-secret and then use the proxy built into RL10. The second is
// to perform a full authentication with the RS platform. The former is faster (no auth request
// required but only provides access to API calls that an "instance role" is allowed to perform.

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"os"
	"regexp"
	"strings"
	"time"
)

const requestTimeout = 300 * time.Second // overall timeout for HTTP requests to API

// Client is the handle onto a RightScle client interface.
// Create a Client object by calling NewClient()
type Client interface {
	Get(uri string) (resp *http.Response, err error)
	Post(uri string, bodyType string, body []byte) (*http.Response, error)
	SetInsecure()           // makes the client accept broken ssl certs, used in tests
	SetDebug(debug bool)    // causes each request and response to be logged
	RecordHttp(w io.Writer) // starts recording requests/resp to put into tests
}

//===== Client data structure and helper functions

// An rsclient.client is a handle to perform HTTP requests to the RightScale platform as
// well as send and receive RightNet messages over Websockets
type client struct {
	cl          http.Client // underlying std http client
	account     string      // RightScale account ID (needed to get cluster redirect)
	debug       bool        // whether to print request/response bodies
	authToken   string      // OAuth authentication token used in every request
	httpServer  string      // FQDN of RightScale HTTP API endpoint
	proxySecret string      // secret required to use HTTP handler/proxy
	recorder    io.Writer   // where to record req/resp to put into tests
}

// Set debugging
func (c *client) SetDebug(debug bool) {
	c.debug = debug
}

// Make client not check SSL cert, this is used in the test suite
func (c *client) SetInsecure() {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	c.cl = http.Client{Transport: tr}
}

// Add a recorder for HTTP requests, this is used to generate test fixtures
func (c *client) RecordHttp(w io.Writer) {
	c.recorder = w
}

// Given a URI such as /api/instances create a full URL
func (c *client) makeURL(uri string) string {
	if !strings.HasPrefix(uri, "/") {
		uri = "/" + uri
	}
	return "https://" + c.httpServer + uri
}

// Add std headers
func (c *client) setHeaders(h http.Header) {
	if c.authToken != "" {
		h.Set("Authorization", "Bearer "+c.authToken)
	} else if c.proxySecret != "" {
		h.Set("X-RLL-Secret", c.proxySecret)
	}

	if c.account != "" {
		h.Set("X-Account", c.account)
	}
	h.Set("X-API-VERSION", "1.5")
	h.Set("User-Agent", "rightlink_cmd")
}

//===== RightLink10 proxy secret

var reRllPort = regexp.MustCompile(`RS_RLL_PORT=(\d+)`)
var reRllSecret = regexp.MustCompile(`RS_RLL_SECRET=([A-Za-z0-9]+)`)

func NewProxyClient(path string) (Client, error) {
	// read the file content
	secrets, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Error reading proxy secret file %s: %s",
			path, err.Error())
	}

	// parse file using regexp
	rllPort := reRllPort.FindSubmatch(secrets)
	if len(rllPort) != 1 {
		return nil, fmt.Errorf("Cannot find or parse RS_RLL_PORT in %s", path)
	}
	rllSecret := reRllSecret.FindSubmatch(secrets)
	if len(rllSecret) != 1 {
		return nil, fmt.Errorf("Cannot find or parse RS_RLL_SECRET in %s", path)
	}

	// concoct client
	c := &client{
		httpServer:  "http://localhost:" + string(rllPort[0]),
		proxySecret: string(rllSecret[0]),
	}
	c.cl.Timeout = requestTimeout
	return c, nil
}

//===== Auth stuff =====

func NewDirectClient(httpServer, account string) Client {
	c := &client{httpServer: httpServer, account: account}
	c.cl.Timeout = requestTimeout
	return c
}

type authResponse struct {
	Mode        string `json:"mode"`
	ShardId     int    `json:"shard_id"`
	TokenType   string `json:"token_type"`
	ApiUrl      string `json:"api_url"`
	AccessToken string `json:"access_token"`
	RouterUrl   string `json:"router_url"`
	ExpiresIn   int    `json:"expires_in"`
}

/*
// Perform an authentication request and save the oauth token in the client
func (c *client) authenticate(clientId, clientSecret string) error {
	// issue the request
	body := fmt.Sprintf("grant_type=client_credentials&client_id=%s&client_secret=%s&r_s_version=23",
		clientId, clientSecret)
	resp, err := c.Post("/api/oauth2", "application/x-www-form-urlencoded", []byte(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// check out the response body
	if resp.StatusCode == 400 {
		return fmt.Errorf("Invalid oauth credentials")
	} else if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		if len(body) > 0 {
			return fmt.Errorf("%s (%d)", resp.Status, resp.StatusCode)
		} else {
			return fmt.Errorf("%s (%d). Server says <<%s>>",
				resp.Status, resp.StatusCode, body)
		}
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		return fmt.Errorf("got %s instead of application/json",
			resp.Header.Get("Content-Type"))
	}
	// Random feature: print the time-offset
	checkTimeOffset(resp, t0)
	// Now parse the JSON response
	respBody, err := ioutil.ReadAll(resp.Body) // note: unbounded read is security issue
	authResp := authResponse{}
	err = json.Unmarshal(respBody, &authResp)
	if err != nil {
		return fmt.Errorf("cannot parse JSON response: %s", err.Error())
	}
	if authResp.AccessToken == "" || authResp.RouterUrl == "" || authResp.ApiUrl == "" {
		return fmt.Errorf("incomplete auth response: %s ---> %#v", respBody, authResp)
	}
	apiUrl, err := url.Parse(authResp.ApiUrl)
	if err != nil {
		return fmt.Errorf("cannot parse API URL: %s", err.Error())
	}
	wsUrl, err := url.Parse(authResp.RouterUrl)
	if err != nil {
		return fmt.Errorf("cannot parse Websocket URL: %s", err.Error())
	}
	//log.Printf("**REMOVEME** OAuth successful: %#v\n", authResp)
	if c.httpServer != apiUrl.Host {
		log.Printf("OAuth redirects to: %#v\n", apiUrl.Host)
	}
	// Save what we got in the client
	c.authToken = authResp.AccessToken
	c.authExpires = timeNow().Add(time.Duration(authResp.ExpiresIn) * time.Second)
	c.httpServer = apiUrl.Host
	c.wsServer = wsUrl.Host
	c.agentId = fmt.Sprintf("rs-instance-%x-%s", sha1.Sum([]byte(clientSecret)), clientId)

	return nil
}
*/

//===== Actuall perform calls =====

// readBody reads the response body replaces it with a buffered copy for re-reading, and
// returns it
func readBody(resp *http.Response) ([]byte, error) {
	// ok to use ReadAll 'cause this is only used in test&debug, not production
	body, err := ioutil.ReadAll(resp.Body)
	if err == nil {
		resp.Body = ioutil.NopCloser(bytes.NewReader(body))
	}
	return body, err
}

// logRequest logs a request and dumps request & response if there was an error and we're in
// debug mode
func logRequest(debug bool, err error, req *http.Request, reqDump []byte, resp *http.Response) {
	// short-hand function to print to stderr
	logf := func(format string, args ...interface{}) {
		fmt.Fprintf(os.Stderr, format, args...)
	}

	if err != nil { // request didn't happen, got an error
		logf("HTTP %s '%s' error: %s", req.Method, req.URL.Path, err.Error())
	} else {
		if resp == nil { // nil response, not much we can log
			logf("HTTP %s '%s': null response ??", req.Method, req.URL.Path)
		} else if resp.StatusCode == 301 || resp.StatusCode == 302 { // redirect
			logf("HTTP %s redirect to %s", req.Method, resp.Header.Get("Location"))
		} else if resp.StatusCode > 399 { // a real error, log shtuff
			logf("HTTP %s %s returned %s", req.Method, req.URL.Path, resp.Status)
			if debug {
				// in debug mode dump everything
				logf("===== REQUEST =====\n%s", reqDump)
				dump, _ := httputil.DumpResponse(resp, true)
				logf("===== RESPONSE =====\n%s", dump)
			} else if resp.Body != nil {
				// in normal mode dump response (should contain error msg)
				body, err := readBody(resp)
				if err != nil {
					logf("HTTP %s %s error reading response body: %s",
						req.Method, req.URL.Path, err.Error())
				} else {
					logf("HTTP %s response body: %s", req.Method, string(body))
					resp.Body = ioutil.NopCloser(bytes.NewReader(body))
				}
			}
		} else if debug { // 2XX - all ok, only log if debug is set
			logf("HTTP %s %s -> %s", req.Method, req.URL.Path, resp.Status)
		}
	}
}

var noAuthHeader = regexp.MustCompile(`(?im)^Authorization:.*$`)

// Same as http.Client.Get but just pass URI, like /api/instances
func (c *client) Get(uri string) (resp *http.Response, err error) {
	req, err := http.NewRequest("GET", c.makeURL(uri), nil)
	if err != nil {
		return nil, err
	}

	c.setHeaders(req.Header)
	dump, _ := httputil.DumpRequestOut(req, true)
	dump = noAuthHeader.ReplaceAll(dump, []byte("Authorization: Bearer <hidden>"))

	// perform the request
	resp, err = c.cl.Do(req)

	// recording (ignore errors here)
	if c.recorder != nil {
		fmt.Fprintf(c.recorder, "{ \"GET\", \"%s\", \"\", %q }", uri, dump)
	}

	// logging
	logRequest(c.debug, err, req, dump, resp)

	return
}

// Same as http.Client.Post but just pass URI, like /api/instances
func (c *client) Post(uri string, bodyType string, body []byte) (resp *http.Response, err error) {
	req, err := http.NewRequest("POST", c.makeURL(uri), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	c.setHeaders(req.Header)
	req.Header.Set("Content-Type", bodyType)
	dump, _ := httputil.DumpRequestOut(req, true)
	dump = noAuthHeader.ReplaceAll(dump, []byte("Authorization: Bearer <hidden>"))

	// perform the request
	resp, err = c.cl.Do(req)

	// recording (ignore errors here)
	if c.recorder != nil {
		respBody, _ := readBody(resp)
		fmt.Fprintf(c.recorder, "{ \"GET\", \"%s\", %q, %q }", uri, dump, respBody)
	}

	// logging
	logRequest(c.debug, err, req, dump, resp)

	return
}
