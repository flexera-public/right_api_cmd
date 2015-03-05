// Copyright (c) 2015 RightScale, Inc. - see LICENSE

package main

// Omega: Alt+937

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Recorded request", func() {

	f, err := os.Open("recording.json")
	if err != nil {
		fmt.Fprintf(os.Stdout, "Cannot open recording: %s\n", err.Error())
		os.Exit(1)
	}

	for {
		var testCase MyRecording
		err := json.NewDecoder(f).Decode(&testCase)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Json decode: %s\n", err.Error())
			break
		}

		handler := func(w http.ResponseWriter, req *http.Request) {
			reqBody, _ := ioutil.ReadAll(req.Body)
			Ω(req.Method).Should(Equal(testCase.RR.Verb))
			Ω(req.URL).Should(Equal(testCase.RR.Uri))
			Ω(reqBody).Should(Equal(testCase.RR.ReqBody))
			for k, _ := range testCase.RR.ReqHeader {
				Ω(req.Header.Get(k)).Should(Equal(testCase.RR.ReqHeader.Get(k)))
			}

			w.WriteHeader(testCase.RR.Status)
			w.Write([]byte(testCase.RR.RespBody))
		}

		It(strings.Join(testCase.CmdArgs, " "), func() {
			server := httptest.NewTLSServer(http.HandlerFunc(handler))
			defer server.Close()

			os.Args = append([]string{
				"rs-api",
				//"--debug",
				"--host", strings.TrimPrefix(server.URL, "http://")},
				testCase.CmdArgs...)
			fmt.Fprintf(os.Stderr, "testing \"%s\"\n", strings.Join(os.Args, `" "`))

			stdoutBuf := bytes.Buffer{}
			osStdout = &stdoutBuf
			exitCode := 99
			osExit = func(code int) { exitCode = code }
			rsClientInternal = nil
			rightscale().SetInsecure()

			main()

			Ω(exitCode).Should(Equal(testCase.ExitCode))
			Ω(stdoutBuf.String()).Should(Equal(testCase.Stdout))
		})
	}

})
