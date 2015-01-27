// Copyright (c) 2014 RightScale, Inc. - see LICENSE

// Miscellaneous: good place to put stuff that has no home and move it later...

package main

import (
	"io"
	"io/ioutil"
)

// like ioutil.ReadAll but with an explicit limit for safety
func ReadLimited(r io.Reader, limit int64) ([]byte, error) {
	return ioutil.ReadAll(&io.LimitedReader{R: r, N: limit})
}
