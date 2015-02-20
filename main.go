// Copyright (c) 2015 RightScale, Inc. - see LICENSE

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"runtime"

	"github.com/coddingtonbear/go-jsonselect"
	"github.com/jmoiron/jsonq"
	"gopkg.in/alecthomas/kingpin.v1"
)

var (
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

Non-zero exit codes indicate a problem: 1 -> HTTP 401, 2 -> HTTP 4XX, 3 -> HTTP 403,
4 -> HTTP 404, 5 -> HTTP 5XX
`)

	debugFlag  = app.Flag("debug", "Enable verbose request and response logging").Bool()
	host       = app.Flag("host", "host:port for API endpoint or RL10 proxy").String()
	rsKey      = app.Flag("key", "RightScale API key or RL10 proxy secret").String()
	prettyFlag = app.Flag("pretty", "pretty-print json output").Bool()
	//fetchFlag   = app.Flag("fetch", "auto-fetch resource returned in Location header").Bool()
	//noRedirFlag = app.Flag("noRedirect", "do not follow any redirects").Bool()
	rl10Flag = app.Flag("rl10", "use RightLink10 proxy and auto-detect port/secret "+
		"unless -host flag is provided").Bool()

	resourceHref = app.Arg("resource-href", "href of resource to operate on or shortcut, "+
		"ex: /api/instances/1234, servers, server_templates, self").Required().String()
	actionName = app.Arg("action", "name of action, ex: index, create, delete, launch, ...").
			Required().String()
	arguments = app.Arg("parameters", "arguments to the API call as described in API docs, "+
		"ex: 'server[instance][href]=/api/instances/123456'").Strings()

	x1 = app.Flag("x1", "extract single value from response using json:select, "+
		"print on one line").String()
	xm = app.Flag("xm", "extract multiple values from response using json:select, "+
		"print one value per line").String()
	xj = app.Flag("xj", "extract data from response using json:select, "+
		"print values as json array on one line").String()
	xh = app.Flag("xh", "extract value of named header and print on one line").String()

//	setTag    = app.Command("set_tag", "Set tags on the instance")
//	tagsToSet = setTag.Arg("tags", "Tags to add, e.g. rs_agent:monitoring=true").
//			Required().Strings()
)

func init() { kingpin.Version(VV) }

// file with info to access Rightlink10 proxy to RightApi, this could be more Go idiomatic, but
// let's wait whether more linux vs windows differences actually accumulate
var rllSecretPath = "/var/run/rll-secret"

func init() {
	if runtime.GOOS == "windows" {
		rllSecretPath = "C:\\some\\path\\rll-secret"
	}
}

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
		rsClientInternal, err = NewProxyClient(*host, *rsKey, *debugFlag)
		if err != nil {
			kingpin.FatalIfError(err, "")
		}
	} else {
		// we're gonna go direct to RightScale
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

	return rsClientInternal
}

var reResourceHref = regexp.MustCompile(`^([a-z0-9_]+)|(/(api|rll)(/[A-Za-z0-9_]+)+)$`)

func main() {
	_ = kingpin.MustParse(app.Parse(os.Args[1:]))

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

	if xFlags == 0 {
		// not extracting, let's print the json pretty or not
		if *prettyFlag {
			var buf bytes.Buffer
			json.Indent(&buf, js, "", "  ")
			js = buf.Bytes()
		}
		fmt.Println(string(js))
		return
	}

	if *xh != "" {
		// we're extracting a header
		fmt.Println(resp.header.Get(*xh))
		return
	}

	// let's extract something using json:select
	parser, err := jsonselect.CreateParserFromString(string(js))
	kingpin.FatalIfError(err, "")
	values, err := parser.GetValues(selectExpr)
	kingpin.FatalIfError(err, "")

	if selectOne {
		if len(values) == 0 {
			kingpin.Fatalf("No value selected, result was: <<%s>>", string(js))
		} else if len(values) > 1 {
			kingpin.Fatalf("Multiple values selected, result was: <<%s>>", string(js))
		}
	}
	if *xj != "" {
		// print array of json values
		js, err := json.Marshal(values)
		kingpin.FatalIfError(err, "Error printing selected value")
		fmt.Println(string(js))
	} else {
		// print one value per line
		for _, v := range values {
			js, err := json.Marshal(v)
			kingpin.FatalIfError(err, "Error printing selected value")
			fmt.Println(string(js))
		}
	}

}

var reArgument = regexp.MustCompile(`^([a-zA-Z0-9_\[\]]+)=(.*)`)

var crudActions = map[string]string{
	"index": "GET", "show": "GET", "list": "GET",
	"update": "POST", "create": "POST", "delete": "DELETE",
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

	// perform the request
	method := crudActions[actionName]
	if method == "" {
		method = "POST" // custom actions all use POST
		resourceHref += "/" + actionName
	}

	resp, err := rightscale().Do(method, resourceHref, arguments, "", "")
	kingpin.FatalIfError(err, "")

	// produce JSON
	js, err := json.Marshal(resp.data)
	kingpin.FatalIfError(err, "")

	return resp, js
}

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
