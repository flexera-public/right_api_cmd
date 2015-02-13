// Copyright (c) 2015 RightScale, Inc. - see LICENSE

package main

//===== RightScale HTTP Requests

// This file implements the various HTTPS requests used to communicate with the RightScale
// API (V1.5 and V1.6) through the RLL proxy or possibly directly. It is primarily a convenience
// layer on top of the std Go http client that provides a number of features, including:
// - keeping track of the global session auth cookie
// - adding the API version header
// - performing retries where appropriate
// - returning an exit status on error
// - offering some logging capability
// - extracting fields from the response

// The client has two ways to authenticate. The first is to read the RightLink10 proxy
// secret file in /var/run/rll-secret and then use the proxy built into RL10. The second is
// to perform a full authentication with the RS platform. The former is faster (no auth request
// required but only provides access to API calls that an "instance role" is allowed to perform.

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const requestTimeout = 300 * time.Second // overall timeout for HTTP requests to API

// Client is the handle onto a RightScle client interface.
// Create a Client object by calling NewClient()
type Client interface {
	SetVersion(v string) // sets the RightApi version, either "1.5" or "1.6"
	Get(uri string, args map[string]interface{}) (*Response, error)
	Post(uri string, args map[string]interface{}) (*Response, error)
	Put(uri string, args map[string]interface{}, contentType, content string) (*Response, error)
	Delete(uri string, args map[string]interface{}) (*Response, error)
	SetInsecure()           // makes the client accept broken ssl certs, used in tests
	SetDebug(debug bool)    // causes each request and response to be logged
	RecordHttp(w io.Writer) // starts recording requests/resp to put into tests
}

type Response struct {
	statusCode   int
	errorMessage string
	data         map[string]interface{}
	raw          []byte
	location     string // value of Location: response header
}

//===== Client data structure and helper functions

// An rsclient.client is a handle to perform HTTP requests to the RightScale platform as
// well as send and receive RightNet messages over Websockets
type client struct {
	cl          http.Client // underlying std http client
	apiVersion  string      // "1.5" or "1.6"
	account     string      // RightScale account ID (needed to get cluster redirect)
	debug       bool        // whether to print request/response bodies
	authToken   string      // OAuth authentication token used in every request
	httpServer  string      // "http[s]://hostname" for RightScale HTTP API endpoint
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
	return c.httpServer + uri
}

// Set the API version
func (c *client) SetVersion(v string) {
	c.apiVersion = v
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
	h.Set("X-API-VERSION", c.apiVersion)
	h.Set("User-Agent", "rightlink_cmd")
}

//===== RightLink10 proxy secret

var reRllPort = regexp.MustCompile(`RS_RLL_PORT=(\d+)`)
var reRllSecret = regexp.MustCompile(`RS_RLL_SECRET=([A-Za-z0-9]+)`)
var reWhere = regexp.MustCompile(`([-A-Za-z0-9.]+):([0-9]+)/([A-Za-z0-9]+)`) // host:port/secret

func NewProxyClient(where string) (Client, error) {
	var rllHost, rllPort, rllSecret string

	if m := reWhere.FindStringSubmatch(where); len(m) == 4 {
		// explicit info about what to contact
		rllHost = m[1]
		rllPort = m[2]
		rllSecret = m[3]
	} else {
		// read file content to get the info
		secrets, err := ioutil.ReadFile(where)
		if err != nil {
			return nil, fmt.Errorf("Error reading proxy secret file %s: %s",
				where, err.Error())
		}

		// parse file using regexp
		p := reRllPort.FindSubmatch(secrets)
		if len(p) != 1 {
			return nil, fmt.Errorf("Cannot find or parse RS_RLL_PORT in %s", where)
		}
		rllPort = string(p[0])
		s := reRllSecret.FindSubmatch(secrets)
		if len(s) != 1 {
			return nil, fmt.Errorf("Cannot find or parse RS_RLL_SECRET in %s", where)
		}
		rllSecret = string(s[0])
		rllHost = "localhost"
	}

	// concoct client
	c := &client{
		httpServer:  "http://" + rllHost + ":" + rllPort,
		proxySecret: rllSecret,
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

//===== Actually perform calls =====

// readBody reads the response body and replaces it with a buffered copy for re-reading, and
// returns it. This is useful when the body needs to be consumed multiple times
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
		logf("HTTP %s '%s' error: %s\n", req.Method, req.URL.Path, err.Error())
	} else {
		if resp == nil { // nil response, not much we can log
			logf("HTTP %s '%s': null response ??\n", req.Method, req.URL.Path)
		} else if resp.StatusCode == 301 || resp.StatusCode == 302 { // redirect
			logf("HTTP %s redirect to %s\n", req.Method, resp.Header.Get("Location"))
		} else if resp.StatusCode > 399 { // a real error, log shtuff
			logf("HTTP %s %s returned %s\n", req.Method, req.URL.Path, resp.Status)
			if debug {
				// in debug mode dump everything
				logf("===== REQUEST =====\n%s\n", reqDump)
				dump, _ := httputil.DumpResponse(resp, true)
				logf("===== RESPONSE =====\n%s\n", dump)
			} else if resp.Body != nil {
				// in normal mode dump response (should contain error msg)
				body, err := readBody(resp)
				if err != nil {
					logf("HTTP %s %s error reading response body: %s\n",
						req.Method, req.URL.Path, err.Error())
				} else {
					logf("HTTP %s response body: %s\n", req.Method, string(body))
					resp.Body = ioutil.NopCloser(bytes.NewReader(body))
				}
			}
		} else if debug { // 2XX - all ok, only log if debug is set
			logf("HTTP %s %s -> %s\n", req.Method, req.URL.Path, resp.Status)
		}
	}
}

var noAuthHeader = regexp.MustCompile(`(?im)^Authorization:.*$`)

// construct a queryString from the args, which is a map to strings or arrays of strings, for
// example: { "view": "expanded", "filter[]": [ "name==my_name", "cloud_href==/api/clouds/1" ] }
// both the key and the value o fthe map will be URL-encoded
func queryStringArgs(args map[string]interface{}) string {
	qs := ""   // query string we're building
	conn := "" // connector between clauses, i.e., "&" after the first
	for k, v := range args {
		if s, ok := v.(string); ok {
			qs += conn + url.QueryEscape(k) + "=" + url.QueryEscape(s)
		} else if l, ok := v.([]string); ok {
			for _, s := range l {
				qs += conn + url.QueryEscape(k) + "=" + url.QueryEscape(s)
			}
		} else {
			fmt.Fprintf(os.Stderr, "Cannot make query string from %#v\n", v)
			os.Exit(1)
		}
		conn = "&"
	}
	return qs
}

func parseResponseBody(body io.Reader) (map[string]interface{}, error) {
	if body == nil {
		return nil, nil
	}

	var data interface{}
	err := json.NewDecoder(body).Decode(&data)
	if err == io.EOF {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("error decoding json: %s", err.Error())
	}
	if _, ok := data.(map[string]interface{}); !ok {
		return nil, fmt.Errorf("response is not a hash: %#v", data)
	}
	return data.(map[string]interface{}), nil
}

func processResponse(req *http.Request, resp *http.Response) (*Response, error) {
	r := Response{statusCode: resp.StatusCode, location: resp.Header.Get("Location")}
	if resp.StatusCode >= 200 && resp.StatusCode < 299 {
		var err error
		r.raw, err = readBody(resp)
		if err == nil {
			r.data, err = parseResponseBody(resp.Body)
		}
		if err != nil {
			return nil, fmt.Errorf("HTTP %s %s: %s",
				req.Method, req.URL.Path, err.Error())
		}
	} else if resp.Body != nil {
		var err error
		r.raw, err = readBody(resp)
		if err != nil {
			return nil, fmt.Errorf("HTTP %s %s error reading response body: %s",
				req.Method, req.URL.Path, err.Error())
		}
		r.errorMessage = string(r.raw)
	} else {
		r.errorMessage = resp.Status
	}

	return &r, nil
}

// Same as http.Client.Get but just pass URI, like /api/instances
func (c *client) Get(uri string, args map[string]interface{}) (*Response, error) {
	uri = c.makeURL(uri)
	if args != nil {
		uri += "?" + queryStringArgs(args)
	}

	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return nil, err
	}

	c.setHeaders(req.Header)
	dump, _ := httputil.DumpRequestOut(req, true)
	dump = noAuthHeader.ReplaceAll(dump, []byte("Authorization: Bearer <hidden>"))

	// perform the request
	resp, err := c.cl.Do(req)

	// recording (ignore errors here)
	if c.recorder != nil {
		fmt.Fprintf(c.recorder, "{ \"GET\", \"%s\", \"\", %q }\n", uri, dump)
	}

	// logging
	logRequest(c.debug, err, req, dump, resp)

	if err != nil {
		return nil, err
	}

	return processResponse(req, resp)
}

// Same as http.Client.Post but just pass URI, like /api/instances
func (c *client) Post(uri string, args map[string]interface{}) (*Response, error) {
	uri = c.makeURL(uri)
	if args != nil {
		uri += "?" + queryStringArgs(args)
	}
	req, err := http.NewRequest("POST", uri, nil)
	if err != nil {
		return nil, err
	}

	c.setHeaders(req.Header)
	//req.Header.Set("Content-Type", bodyType)
	dump, _ := httputil.DumpRequestOut(req, true)
	dump = noAuthHeader.ReplaceAll(dump, []byte("Authorization: Bearer <hidden>"))

	// perform the request
	resp, err := c.cl.Do(req)

	// recording (ignore errors here)
	if c.recorder != nil {
		respBody, _ := readBody(resp)
		fmt.Fprintf(c.recorder, "{ \"POST\", \"%s\", %q, %q }\n", uri, dump, respBody)
	}

	// logging
	logRequest(c.debug, err, req, dump, resp)

	if err != nil {
		return nil, err
	}

	return processResponse(req, resp)
}

// Same as http.Client.Put but just pass URI, like /api/instances
func (c *client) Put(uri string, args map[string]interface{}, contentType, content string) (
	*Response, error) {
	uri = c.makeURL(uri)
	if args != nil {
		uri += "?" + queryStringArgs(args)
	}
	req, err := http.NewRequest("PUT", uri, strings.NewReader(content))
	if err != nil {
		return nil, err
	}

	c.setHeaders(req.Header)
	req.Header.Set("Content-Type", contentType)
	dump, _ := httputil.DumpRequestOut(req, true)
	dump = noAuthHeader.ReplaceAll(dump, []byte("Authorization: Bearer <hidden>"))

	// perform the request
	resp, err := c.cl.Do(req)

	// recording (ignore errors here)
	if c.recorder != nil {
		respBody, _ := readBody(resp)
		fmt.Fprintf(c.recorder, "{ \"PUT\", \"%s\", %q, %q }\n", uri, dump, respBody)
	}

	// logging
	logRequest(c.debug, err, req, dump, resp)

	if err != nil {
		return nil, err
	}

	return processResponse(req, resp)
}

// Same as http.Client.Delete but just pass URI, like /api/instances
func (c *client) Delete(uri string, args map[string]interface{}) (*Response, error) {
	uri = c.makeURL(uri)
	if args != nil {
		uri += "?" + queryStringArgs(args)
	}
	req, err := http.NewRequest("DELETE", uri, nil)
	if err != nil {
		return nil, err
	}

	c.setHeaders(req.Header)
	dump, _ := httputil.DumpRequestOut(req, true)
	dump = noAuthHeader.ReplaceAll(dump, []byte("Authorization: Bearer <hidden>"))

	// perform the request
	resp, err := c.cl.Do(req)

	// recording (ignore errors here)
	if c.recorder != nil {
		respBody, _ := readBody(resp)
		fmt.Fprintf(c.recorder, "{ \"DELETE\", \"%s\", %q, %q }\n", uri, dump, respBody)
	}

	// logging
	logRequest(c.debug, err, req, dump, resp)

	if err != nil {
		return nil, err
	}

	return processResponse(req, resp)
}
