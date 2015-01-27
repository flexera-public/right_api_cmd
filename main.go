// Copyright (c) 2015 RightScale, Inc. - see LICENSE

package main

import (
	"fmt"
	"os"

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

func main() {

	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	case setTag.FullCommand():
		fmt.Printf("Tags: %#v", tagsToSet)
		setTagCmd()
	default:
		app.Usage(os.Stderr)
		os.Exit(1)
	}
}

func setTagCmd() {
	cl := NewProxyClient()

}
