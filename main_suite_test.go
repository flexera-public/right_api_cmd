// Copyright (c) 2015 RightScale, Inc. - see LICENSE

package main

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

func TestRLCmd(t *testing.T) {

	// load the testcases
	//err := services.Load("./aws-metadata", "./rest-metadata", serviceFiles)
	//if err != nil {
	//	log.Fatal(err)
	//}

	format.UseStringerRepresentation = true
	RegisterFailHandler(Fail)
	RunSpecs(t, "RL_CMD")
}
