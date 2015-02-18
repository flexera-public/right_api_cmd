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
	Do(method, uri string, args []string, contentType, content string) (*Response, error)
	SetInsecure()           // makes the client accept broken ssl certs, used in tests
	SetDebug(debug bool)    // causes each request and response to be logged
	RecordHttp(w io.Writer) // starts recording requests/resp to put into tests
}

type Response struct {
	statusCode   int
	errorMessage string
	data         interface{}
	raw          []byte
	header       http.Header
}

//===== Client data structure and helper functions

// An rsclient.client is a handle to perform HTTP requests to the RightScale platform.
// if proxySecret is set, we use the RL proxy at httpServer, else we use a direct connection
// to httpServer with apiKey and authToken
type client struct {
	cl          http.Client // underlying std http client
	apiVersion  string      // "1.5" or "1.6"
	debug       bool        // whether to print request/response bodies
	httpServer  string      // "http[s]://hostname:port" for RightScale HTTP API endpoint
	account     string      // RightScale account ID (needed to get cluster redirect)
	authToken   string      // OAuth authentication token used in every direct request
	apiKey      string      // API key for direct connections
	proxySecret string      // proxy secret for RL10 proxied connections
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
	if c.proxySecret != "" {
		h.Set("X-RLL-Secret", c.proxySecret)
	} else if c.authToken != "" {
		h.Set("Authorization", "Bearer "+c.authToken)
	}

	if c.account != "" {
		h.Set("X-Account", c.account)
	}
	h.Set("X-API-VERSION", c.apiVersion)
	h.Set("User-Agent", "right_api_cmd")
}

//===== RightLink10 proxy secret

var reRllPort = regexp.MustCompile(`RS_RLL_PORT=(\d+)`)
var reRllSecret = regexp.MustCompile(`RS_RLL_SECRET=([A-Za-z0-9]+)`)
var reWhere = regexp.MustCompile(`([-A-Za-z0-9.]+):([0-9]+)`) // host:port

func NewProxyClient(proxyHost, secret string, debug bool) (Client, error) {
	var rllHost, rllPort, rllSecret string

	if proxyHost == "" || secret == "" {
		// read file content to get the info
		secrets, err := ioutil.ReadFile(rllSecretPath)
		if err != nil {
			return nil, fmt.Errorf("reading proxy secret file: %s", err.Error())
		}

		// parse file using regexp
		p := reRllPort.FindSubmatch(secrets)
		if len(p) != 1 {
			return nil, fmt.Errorf("Cannot find or parse RS_RLL_PORT in %s",
				rllSecretPath)
		}
		rllPort = string(p[0])
		s := reRllSecret.FindSubmatch(secrets)
		if len(s) != 1 {
			return nil, fmt.Errorf("Cannot find or parse RS_RLL_SECRET in %s",
				rllSecretPath)
		}
		rllSecret = string(s[0])
		rllHost = "localhost"
	}

	if m := reWhere.FindStringSubmatch(proxyHost); proxyHost != "" && len(m) == 3 {
		// explicit info about what to contact
		rllHost = m[1]
		rllPort = m[2]
	}

	if secret != "" {
		rllSecret = secret
	}

	// concoct client
	c := &client{
		httpServer:  "http://" + rllHost + ":" + rllPort,
		proxySecret: rllSecret,
		apiVersion:  "1.5",
		debug:       debug,
	}
	c.cl.Timeout = requestTimeout
	return c, nil
}

//===== Auth stuff =====

func NewDirectClient(httpServer, apiKey string, debug bool) (Client, error) {
	if !strings.HasPrefix(httpServer, "https:") {
		httpServer = "https://" + httpServer
	}
	c := &client{httpServer: httpServer, apiKey: apiKey, apiVersion: "1.5", debug: debug}
	c.cl.Timeout = requestTimeout
	err := c.authenticate()
	if err != nil {
		return nil, err
	}

	return c, nil
}

/* this is not used but kept here for reference
type authResponse struct {
	Mode        string `json:"mode"`
	ShardId     int    `json:"shard_id"`
	TokenType   string `json:"token_type"`
	ApiUrl      string `json:"api_url"`
	AccessToken string `json:"access_token"`
	RouterUrl   string `json:"router_url"`
	ExpiresIn   int    `json:"expires_in"`
}
*/

// Perform an authentication request and save the oauth token in the client
func (c *client) authenticate() error {
	resp, err := c.Do("POST", "/api/oauth2", []string{
		"grant_type=refresh_token", "refresh_token=" + c.apiKey}, "", "")
	if err != nil {
		msg := err.Error()
		if resp != nil && resp.data != nil {
			if d, ok := resp.data.(map[string]interface{}); ok {
				if descr, ok := d["error_description"].(string); ok {
					msg += " \"" + descr + "\""
				}
			}
		}
		return fmt.Errorf("OAuth failed: %s", msg)
	}

	data, ok := resp.data.(map[string]interface{})
	if !ok || len(data) == 0 {
		return fmt.Errorf("Invalid oauth response: <<%s>>", resp.raw)
	}

	if c.authToken, ok = data["access_token"].(string); !ok {
		return fmt.Errorf("Oauth response doesn't have access token: %+v", resp.data)
	}

	return nil
}

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

// logRequest logs a request and dumps request & response if there was an error
func logRequest(err error, req *http.Request, reqDump []byte, resp *http.Response) {
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
			logf("===== REQUEST =====\n%s\n", reqDump)
			dump, _ := httputil.DumpResponse(resp, true)
			logf("===== RESPONSE =====\n%s\n", dump)
		} else { // 2XX - all ok
			logf("HTTP %s %s -> %s\n", req.Method, req.URL.Path, resp.Status)
		}
	}
}

var noAuthHeader = regexp.MustCompile(`(?im)^Authorization:.*$`)

// construct a queryString from the args, which is a map to strings or arrays of strings, for
// example: { "view": "expanded", "filter[]": [ "name==my_name", "cloud_href==/api/clouds/1" ] }
// both the key and the value of the map will be URL-encoded
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

func parseResponseBody(body io.Reader) (interface{}, error) {
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
	//if _, ok := data.(map[string]interface{}); !ok {
	//	return nil, fmt.Errorf("response is not a hash: %#v", data)
	//}
	return data, nil
}

func processResponse(req *http.Request, resp *http.Response) (*Response, error) {
	r := Response{statusCode: resp.StatusCode, header: resp.Header}
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
		return &r, nil
	} else if resp.Body != nil {
		var err error
		r.raw, err = readBody(resp)
		if err != nil {
			return nil, fmt.Errorf("HTTP %s %s error reading response body: %s",
				req.Method, req.URL.Path, err.Error())
		}
		r.data, _ = parseResponseBody(resp.Body) // in case there's a json error body
		r.errorMessage = string(r.raw)
		return &r, fmt.Errorf("HTTP %s %s: %s", req.Method, req.URL.Path, resp.Status)
	} else {
		r.errorMessage = resp.Status
		return &r, fmt.Errorf("HTTP %s %s: %s", req.Method, req.URL.Path, resp.Status)
	}
}

// Same as http.Client.Post but just pass URI, like /api/instances
func (c *client) Do(method string, uri string, args []string, contentType, content string) (
	*Response, error) {

	uri = c.makeURL(uri)
	if args != nil {
		uri += "?" + strings.Join(args, "&")
	}
	req, err := http.NewRequest(method, uri, strings.NewReader(content))
	if err != nil {
		return nil, err
	}

	c.setHeaders(req.Header)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	dump, _ := httputil.DumpRequestOut(req, true)
	dump = noAuthHeader.ReplaceAll(dump, []byte("Authorization: Bearer <hidden>"))

	try := 1
	for {
		// perform the request
		var res *http.Response
		res, err = c.cl.Do(req)

		// log every iteration
		if c.debug {
			logRequest(err, req, dump, res)
		}

		// if the request didn't happen, retry
		// TODO: need to be careful with timeouts!
		if err != nil {
			continue
		}

		// process the response, which extracts json
		resp, err := processResponse(req, res)

		if resp.statusCode < 500 || try >= 3 {
			// success or our error, return what we got after recording
			if c.recorder != nil {
				respBody, _ := readBody(res)
				fmt.Fprintf(c.recorder, "{ \"%s\", \"%s\", %q, %q }\n",
					method, uri, dump, respBody)
			}

			return resp, err
		}

		try += 1
	}
}
