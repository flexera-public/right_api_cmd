// Copyright (c) 2015 RightScale, Inc. - see LICENSE

package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/jmoiron/jsonq"
	"gopkg.in/alecthomas/kingpin.v1"
)

var (
	app   = kingpin.New("rs_cmd", "RightScale/RightLink10 commands")
	debug = app.Flag("debug", "Enable verbose request and response logging").Bool()
	proxy = app.Flag("proxy", "host:port/secret for API proxy (optional)").String()

	setTag    = app.Command("set_tag", "Set tags on the instance")
	tagsToSet = setTag.Arg("tags", "Tags to add, e.g. rs_agent:monitoring=true").
			Required().Strings()
)

func init() { kingpin.Version("0.0.1") }

// file with info to access Rightlink10 proxy to RightApi, this could be more Go idiomatic, but
// let's wait whether more linux vs windows differences actually accumulate
var rllSecretPath = "/var/run/rll-secret"

func init() {
	if runtime.GOOS == "windows" {
		rllSecretPath = "C:\\some\\path\\rll-secret"
	}
}

var rsClientInternal Client // internal, do not use directly!
var rsProxyLocation string  // how to contact the proxy, either a file or host:port/secret

// rightscale is the client handle to use to make API 1.5 requests, it's a function that will
// create an actual NewProxyClient the first time it's called
var rightscale = func() Client {
	if rsClientInternal != nil {
		return rsClientInternal
	}

	// either get the proxy location from the file or from the --proxy command line flag
	where := rllSecretPath
	if *proxy != "" {
		where = *proxy
	}

	var err error
	rsClientInternal, err = NewProxyClient(where)
	if err != nil {
		kingpin.FatalIfError(err, "Error contacting RightLink proxy: ")
	}

	return rsClientInternal
}

func main() {
	dispatch := kingpin.MustParse(app.Parse(os.Args[1:]))
	if *debug {
		rightscale().SetDebug(true)
	}
	switch dispatch {
	case setTag.FullCommand():
		fmt.Printf("Tags: %#v\n", tagsToSet)
		selfHref := getSelfHref()
		setTagCmd(selfHref)
	default:
		app.Usage(os.Stderr)
		os.Exit(1)
	}
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
	// first query RLL to see whether it has the self href as a global variable
	resp, err := rightscale().Get("/rll/env", nil)
	kingpin.FatalIfError(err, "Error fetching self_href: ")
	jq := jsonq.NewQuery(resp.data)
	href, err := jq.String("RS_SELF_HREF")
	if err == nil && href != "" {
		// found it, return it
		if *debug {
			fmt.Fprintf(os.Stderr, "Self href: %s\n", href)
		}
		return href
	}

	// RLL doesn't have it, fetch it from the platform
	resp, err = rightscale().Get("/api/session/instance", nil)
	kingpin.FatalIfError(err, "Error fetching instance from RS")
	href = findRel("self", resp.data)

	if href == "" {
		kingpin.Fatalf("Error extracting self-href")
	}

	// set the self-href as global in RLL
	_, err = rightscale().Put("/rll/env/RS_SELF_HREF", nil, "text/plain", href)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot set RS_SELF_HREF in RLL: %s\n", err.Error())
	}

	if *debug {
		fmt.Fprintf(os.Stderr, "Self href: %s\n", href)
	}
	return href
}

func setTagCmd(selfHref string) {
	args := map[string]interface{}{
		"resource_hrefs[]": selfHref,
		"tags[]":           *tagsToSet,
	}

	_, err := rightscale().Post("/api/tags/multi_add", args)
	kingpin.FatalIfError(err, "Error updating tags: ")

}
