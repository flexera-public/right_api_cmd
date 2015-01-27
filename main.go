// Copyright (c) 2015 RightScale, Inc. - see LICENSE

package main

import (
	"fmt"
	"os"
	"runtime"

	"gopkg.in/alecthomas/kingpin.v1"
)

var (
	app   = kingpin.New("rs_cmd", "RightScale/RightLink10 commands")
	debug = app.Flag("debug", "Enable verbose request and response logging").Bool()

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

// rightscale is the client handle to use to make API 1.5 requests, it's a function that will
// create an actual NewProxyClient the first time it's called
var rightscale = func() Client {
	if rsClientInternal != nil {
		return rsClientInternal
	}
	var err error
	rsClientInternal, err = NewProxyClient(rllSecretPath)
	if err != nil {
		kingpin.FatalIfError(err, "Error contacting RightLink proxy: ")
	}
	return rsClientInternal
}

func main() {

	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	case setTag.FullCommand():
		fmt.Printf("Tags: %#v", tagsToSet)
		selfHref := getSelfHref()
		setTagCmd(selfHref)
	default:
		app.Usage(os.Stderr)
		os.Exit(1)
	}
}

func getSelfHref() string {
	_, err := rightscale().Get("/rll/self_href", nil)
	kingpin.FatalIfError(err, "Error fetching self_href: ")
	return ""
}

func setTagCmd(selfHref string) {
	args := map[string]interface{}{
		"resource_hrefs[]": selfHref,
		"tags[]":           "rs_monitoring:state=active",
	}

	_, err := rightscale().Post("/api/tags/multi_add", args)
	kingpin.FatalIfError(err, "Error updating tags: ")

}
