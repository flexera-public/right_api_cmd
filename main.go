// Copyright (c) 2015 RightScale, Inc. - see LICENSE

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"runtime"

	"github.com/jmoiron/jsonq"
	"github.com/rightscale/go-jsonselect"
	"gopkg.in/alecthomas/kingpin.v1"
)

// Initializing the command line args and flags is a bit convoluted because we need to reinit
// everything each time we run a recorded test

var app *kingpin.Application
var host, rsKey, x1, xm, xj, xh, recordFile, actionName, resourceHref *string
var debugFlag, prettyFlag, rl10Flag *bool
var arguments *[]string

func initKingpin() {
	app = kingpin.New("rs-api", `RightScale/RightLink10 API 1.5/1.6 Command Line Client

rs-api issues API requests to the RightScale platform or to RightLink10 either directly or via
the proxy built into RightLink10. The command line mateches the REST API documentation at
http://reference.rightscale.com/api1.5 very closely. Each request is anchored at a 
resource-href such as /api/servers or /api/cloud/1/instances/123445. Each request performs
an action on that resource or resource collection, such as index, show, delete, launch,
terminate, etc. The action names are as specified in the API docs. The arguments are the
unescaped parameters, such as server[instance][href]=/api/cloud/instances/123456.

Some shortcuts are accepted instead of full resource-hrefs: self denotes the instance's
self-href (/api/cloud/X/instances/Y), single words are replaced by /api/<word> and can be
used for global collections.

By default the requests are issued to API 1.5 using the local RL10 proxy. Use the command
line flags to alter this.

By default the JSON response is printed but instead it is possible to extract values from the
response and print those instead using a JSON:select syntax. See http://jsonselect.org/ for
details.

Non-zero exit codes indicate a problem
`)

	debugFlag = app.Flag("debug", "Enable verbose request and response logging").Bool()
	host = app.Flag("host", "host:port for API endpoint or RL10 proxy").String()
	rsKey = app.Flag("key", "RightScale API key or RL10 proxy secret").String()
	prettyFlag = app.Flag("pretty", "pretty-print json output").Bool()
	//fetchFlag   = app.Flag("fetch", "auto-fetch resource returned in Location header").Bool()
	//noRedirFlag = app.Flag("noRedirect", "do not follow any redirects").Bool()
	rl10Flag = app.Flag("rl10", "use RightLink10 proxy and auto-detect port/secret "+
		"unless -host flag is provided").Bool()

	actionName = app.Arg("action", "name of action, ex: index, create, delete, launch, ...").
		Required().String()
	resourceHref = app.Arg("resource-href", "href of resource to operate on or shortcut, "+
		"ex: /api/instances/1234, servers, server_templates, self").Required().String()
	arguments = app.Arg("parameters", "arguments to the API call as described in API docs, "+
		"ex: 'server[instance][href]=/api/instances/123456'").Strings()

	x1 = app.Flag("x1", "extract single value from response using json:select, "+
		"print on one line").String()
	xm = app.Flag("xm", "extract multiple values from response using json:select, "+
		"print one value per line").String()
	xj = app.Flag("xj", "extract data from response using json:select, "+
		"print values as json array on one line").String()
	xh = app.Flag("xh", "extract value of named header and print on one line").String()
	recordFile = app.Flag("record", "for test generation purposes, specifies a file to record "+
		"all requests").String()
}

func init() { kingpin.Version(VV) }

// file with info to access Rightlink10 proxy to RightApi, this could be more Go idiomatic, but
// let's wait whether more linux vs windows differences actually accumulate
var rllSecretPath = "/var/run/rll-secret"

func init() {
	if runtime.GOOS == "windows" {
		rllSecretPath = "C:\\some\\path\\rll-secret"
	}
}

//===== Overrides for testing

var osExit = os.Exit
var osStdout = io.Writer(os.Stdout)

//===== RightScale client handle

var rsClientInternal Client // internal, do not use directly!
//notused var rsProxyLocation string  // how to contact the proxy, either a file or host:port/secret

// rightscale is the client handle to use to make API 1.5 requests, it's a function that will
// create an actual NewProxyClient the first time it's called
var rightscale = func() Client {
	if rsClientInternal != nil {
		return rsClientInternal
	}

	var err error
	if *rl10Flag {
		// we're gonna use the RL10 proxy
		if *debugFlag {
			fmt.Fprintf(os.Stderr, "Using RightLink10 proxy\n")
		}
		rsClientInternal, err = NewProxyClient(*host, *rsKey, *debugFlag)
		if err != nil {
			kingpin.FatalIfError(err, "")
		}
	} else {
		// we're gonna go direct to RightScale
		if *debugFlag {
			fmt.Fprintf(os.Stderr, "Going direct to RightScale\n")
		}
		h := *host
		if h == "" {
			h = os.Getenv("RS_api_hostname")
		}
		k := *rsKey
		if k == "" {
			k = os.Getenv("RS_api_key")
		}
		rsClientInternal, err = NewDirectClient(h, k, *debugFlag)
		if err != nil {
			kingpin.FatalIfError(err, "")
		}
	}

	if *recordFile != "" {
		rsClientInternal.RecordHttp(recorder)
	}

	return rsClientInternal
}

//===== Request Recording

type MyRecording struct {
	CmdArgs  []string         // command line arguments
	ExitCode int              // Exit code
	Stdout   string           // Exit print
	RR       RequestRecording // back-end request/response
}

var ReqResp MyRecording // global var, we only have one request in flight

//===== Main

var reResourceHref = regexp.MustCompile(`^([a-z0-9_]+)|(/(api|rll)(/[A-Za-z0-9_]+)+)$`)

// record the command line args but skip stuff that we shouldn't record
func captureCmdArgs(args []string) []string {
	rec := []string{}
	skipArg := false // skip the next argument
	for _, a := range os.Args[1:] {
		if skipArg {
			skipArg = false
			continue
		}
		if a == "--record" { // don't record the record flag & filename
			skipArg = true
			continue
		}
		if a == "--host" { // don't record the host flag & hostname
			skipArg = true
			continue
		}
		rec = append(rec, a)
		if a == "--key" { // record a fake key, not the real one
			rec = append(rec, "test-key")
			skipArg = true
		}
	}
	return rec
}

func main() {
	//for i, a := range os.Args {
	//	fmt.Fprintf(os.Stderr, "arg[%d]=%s\n", i, a)
	//}

	// record the command line before we mess it up
	ReqResp.CmdArgs = captureCmdArgs(os.Args[1:])

	// hand the command line to kingpin for real parsing
	initKingpin()
	_ = kingpin.MustParse(app.Parse(os.Args[1:]))

	// validate resource href
	if *resourceHref == "self" {
		rh := getSelfHref()
		resourceHref = &rh
	} else {
		m := reResourceHref.FindStringSubmatch(*resourceHref)
		if m == nil {
			kingpin.Fatalf("resourceHref '%s' is not valid", *resourceHref)
		}
		if len(m) != 5 {
			kingpin.Fatalf("OOPS: %d -- %#v", len(m), m)
		}
		if m[1] != "" {
			href := "/api/" + m[1]
			resourceHref = &href
		}
	}

	if *actionName == "list" {
		i := "index"
		actionName = &i
	}

	// ensure only one extract flag is given -- is there a better way to code this??
	xFlags := 0
	selectExpr := *x1
	selectOne := false
	if *x1 != "" {
		xFlags += 1
		selectOne = true
	}
	if *xm != "" {
		xFlags += 1
		selectExpr = *xm
	}
	if *xj != "" {
		xFlags += 1
		selectExpr = *xj
	}
	if *xh != "" {
		xFlags += 1
	}
	if xFlags > 1 {
		kingpin.Fatalf("cannot specify --x1 and --xm at the same time")
	}

	resp, js := doRequest(*resourceHref, *actionName, *arguments)

	stdout, stderr, exit := doOutput(xFlags, selectOne, selectExpr, resp, js)

	if *recordFile != "" {
		ReqResp.Stdout = stdout
		ReqResp.ExitCode = exit
		//fmt.Fprintf(os.Stderr, "REC:\n%+v\n\n", ReqResp)
		recordToFile(*recordFile, ReqResp)
	}

	fmt.Fprint(os.Stderr, stderr)
	fmt.Fprint(osStdout, stdout)
	osExit(exit)
}

func doOutput(xFlags int, selectOne bool, selectExpr string, resp *Response, js []byte) (string, string, int) {

	if xFlags == 0 {
		// not extracting, let's print the json pretty or not
		if *prettyFlag {
			var buf bytes.Buffer
			json.Indent(&buf, js, "", "  ")
			js = buf.Bytes()
		}

		return string(js), "", 0
	}

	if *xh != "" {
		// we're extracting a header
		return resp.header.Get(*xh), "", 0
	}

	// let's extract something using json:select
	parser, err := jsonselect.CreateParserFromString(string(js))
	if err != nil {
		return "", err.Error(), 1
	}
	values, err := parser.GetValues(selectExpr)
	if err != nil {
		return "", err.Error(), 1
	}

	if selectOne { // --x1 flag, really
		if len(values) == 0 {
			return "", fmt.Sprintf("No value could be selected"), 1
			//return "", fmt.Sprintf("No value could be selected, result was: <<%s>>", js), 1
		} else if len(values) > 1 {
			return "", fmt.Sprintf("Multiple values selected"), 1
			//return "", fmt.Sprintf("Multiple values selected, result was: <<%s>>", js), 1
		}
		switch v := values[0].(type) {
		case nil:
			return "", "", 0
		case bool, float64, string:
			return fmt.Sprint(v), "", 0
		default:
			js, err := json.Marshal(v)
			if err != nil {
				return "", fmt.Sprintf("Error printing selected value: %s",
					err.Error()), 1
				return string(js), "", 0
			}
			return string(js), "", 0
		}
	} else if *xj != "" { // --xj flag
		// print array of json values
		js, err := json.Marshal(values)
		if err != nil {
			return "", fmt.Sprintf("Error printing selected value: %s",
				err.Error()), 1
		}
		return string(js), "", 0
	} else { // --xm flag
		// print one value per line
		stdout := ""
		for _, v := range values {
			js, err := json.Marshal(v)
			if err != nil {
				return "", fmt.Sprintf("Error printing selected value: %s",
					err.Error()), 1
			}
			stdout += string(js) + "\n"
		}
		return stdout, "", 0
	}
}

//===== Perform a request

var reArgument = regexp.MustCompile(`^([a-zA-Z0-9_\[\]]+)=(.*)`)

// crud actions and their http verb
var crudActions = map[string]string{
	"index": "GET", "show": "GET", "list": "GET",
	"update": "POST", "create": "POST", "destroy": "DELETE",
}

// exceptions for custom actions with the URI suffic and http verb
var exceptionActions = map[string][2]string{
	"accounts":          [2]string{"/accounts", "GET"},
	"current_instances": [2]string{"/current_instances", "GET"},
	"data":              [2]string{"/data", "GET"},
	"detail":            [2]string{"/detail", "GET"},
	"multi_update":      [2]string{"/multi_update", "PUT"},
	"servers":           [2]string{"/servers", "GET"},
	"show_source":       [2]string{"/source", "GET"},
	"update_source":     [2]string{"/source", "PUT"},
}

// performs the request and returns a *Response and the parsed json, bombs on error
func doRequest(resourceHref, actionName string, arguments []string) (*Response, []byte) {

	// query-string encode the arguments
	// we don't use url.Values because we allow multiple arguments with the same
	// key, filter[]=... is an example
	// we don't encode the key part because it's not required by our servers
	for i := range arguments {
		s := reArgument.FindStringSubmatch(arguments[i])
		if len(s) != 3 {
			kingpin.Fatalf("argument '%s' is not valid", arguments[i])
		}
		arguments[i] = s[1] + "=" + url.QueryEscape(s[2])
	}

	// figure out the HTTP verb and exact URI, we have a table of CRUD actions, the rest
	// mostly are POST with the action appended to the href, but there are a few exceptions...
	method := crudActions[actionName]
	if method == "" {
		if e, ok := exceptionActions[actionName]; ok {
			// one of the exceptions
			resourceHref += e[0]
			method = e[1]
		} else {
			// default is to use POST
			method = "POST"
			resourceHref += "/" + actionName
		}
	}

	// perform the request
	resp, err := rightscale().Do(method, resourceHref, arguments, "", "")
	if resp == nil {
		kingpin.FatalIfError(err, "")
	} else {
		kingpin.FatalIfError(err, resp.errorMessage)
	}

	// produce JSON
	js := []byte("")
	if resp.data != nil {
		js, err = json.Marshal(resp.data)
		kingpin.FatalIfError(err, "")
	}

	return resp, js
}

//===== Find the self-href

// findRel finds a relationship in a json links collections and returns the href, i.e. given
// { links: [ { rel: "self", href: "/a/b/123" }, { rel: "parent", href: "/b/c/567" }
// findRel("self") returns "/a/b/123"
func findRel(rel string, data map[string]interface{}) string {
	jq := jsonq.NewQuery(data)
	links, err := jq.ArrayOfObjects("links")
	if err != nil || len(links) == 0 {
		return ""
	}

	for _, link := range links {
		if r, ok := link["rel"].(string); ok && r == rel {
			if href, ok := link["href"].(string); ok {
				return href
			}
		}

	}
	return ""
}

// retrieve the instance's self href (e.g. /api/instances/123) either from RLL or from the
// platform
func getSelfHref() string {
	if !*rl10Flag {
		kingpin.Fatalf("Cannot retrieve self-href when not using RightLink proxy")
	}

	// first query RLL to see whether it has the self href as a global variable
	resp, err := rightscale().Do("GET", "/rll/env", nil, "", "")
	kingpin.FatalIfError(err, "fetching self_href")
	jq := jsonq.NewQuery(resp.data)
	href, err := jq.String("RS_SELF_HREF")
	if err == nil && href != "" {
		// found it, return it
		if *debugFlag {
			fmt.Fprintf(os.Stderr, "Self href: %s\n", href)
		}
		return href
	}

	// RLL doesn't have it, fetch it from the platform
	resp, err = rightscale().Do("GET", "/api/session/instance", nil, "", "")
	kingpin.FatalIfError(err, "fetching instance from RS")
	if data, ok := resp.data.(map[string]interface{}); ok {
		href = findRel("self", data)
	}

	if href == "" {
		kingpin.Fatalf("extracting self-href from %+v <<%s>>", resp.data, resp.raw)
	}

	// set the self-href as global in RLL
	_, err = rightscale().Do("PUT", "/rll/env/RS_SELF_HREF", nil, "text/plain", href)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot set RS_SELF_HREF in RLL: %s\n", err.Error())
	}

	if *debugFlag {
		fmt.Fprintf(os.Stderr, "Self href: %s\n", href)
	}
	return href
}

//===== Recording helpers

func recorder(rr RequestRecording) {
	// remove some headers for security purposes and others to reduce recording bulk
	rr.ReqHeader.Del("Authorization")
	rr.ReqHeader.Del("User-Agent")
	rr.RespHeader.Del("Cache-Control")
	rr.RespHeader.Del("Connection")
	rr.RespHeader.Del("Set-Cookie")
	rr.RespHeader.Del("Strict-Transport-Security")
	rr.RespHeader.Del("X-Request-Uuid")
	ReqResp.RR = rr
}

func recordToFile(filename string, r MyRecording) {
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	kingpin.FatalIfError(err, "")
	defer f.Close()
	json, _ := json.MarshalIndent(r, "", "  ")
	fmt.Fprintf(f, "\n%s\n", json)
}

/*
func setTagCmd(selfHref string) {
	args := map[string]interface{}{
		"resource_hrefs[]": selfHref,
		"tags[]":           *tagsToSet,
	}

	_, err := rightscale().Post("/api/tags/multi_add", args)
	kingpin.FatalIfError(err, "Error updating tags: ")

}
*/
