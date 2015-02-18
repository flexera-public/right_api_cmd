#! /usr/bin/make
#
# Makefile for Golang projects
#
# Top-level targets:
# default: compile the program, you can thus use make && ./NAME -options ...
# build: builds binaries for linux and darwin
# test: runs unit tests recursively and produces code coverage stats and shows them
# travis-test: just runs unit tests recursively
# clean: removes build stuff
#
# HACKS - a couple of things here are unconventional in order to keep travis-ci fast:
# - use 'godep save' on your laptop if you add dependencies, but we don't use godep in the
#   makefile, instead, we simply add the godep workspace to the GOPATH

#NAME=$(shell basename $$PWD)
NAME=rs-api
BUCKET=rightscale-binaries
ACL=public-read
DEPEND=golang.org/x/tools/cmd/cover github.com/onsi/ginkgo/ginkgo \
			 github.com/rlmcpherson/s3gof3r/gof3r github.com/coddingtonbear/go-jsonselect

#=== below this line ideally remains unchanged ===

TRAVIS_BRANCH?=dev
DATE=$(shell date '+%F %T')
TRAVIS_COMMIT?=$(shell git symbolic-ref HEAD | cut -d"/" -f 3)
# by manually adding the godep workspace to the path we don't need to run godep itself
GOPATH:=$(PWD)/Godeps/_workspace:$(GOPATH)
# because of the Godep path we build ginkgo into the godep workspace
PATH:=$(PWD)/Godeps/_workspace/bin:$(PATH)

# the default target builds a binary in the top-level dir for whatever the local OS is
default: $(NAME)
$(NAME): *.go version depend
	go build -o $(NAME) .

# the standard build produces a "local" executable and a linux tgz
build: $(NAME) build/$(NAME)-linux-amd64.tgz

# create a tgz with the binary and any artifacts that are necessary
build/$(NAME)-linux-amd64.tgz: *.go version depend
	rm -rf build/$(NAME)
	mkdir -p build/$(NAME)
	GOOS=linux GOARCH=amd64 go build -o build/$(NAME)/$(NAME) .
	for d in script init; do if [ -d $$d ]; then cp -r $$d build/$(NAME); fi; done
	sed -i -e "s/BRANCH/$(TRAVIS_BRANCH)/" build/*/*.sh || true
	cd build; tar zcf $(NAME)-linux-amd64.tgz ./$(NAME)
	rm -r build/$(NAME)

# upload assumes you have AWS_ACCESS_KEY_ID and AWS_SECRET_KEY env variables set,
# which happens in the .travis.yml for CI
upload: depend
	@which gof3r >/dev/null || (echo 'Please "go get github.com/rlmcpherson/s3gof3r/gof3r"'; false)
	(cd build; set -ex; \
	  for f in *.tgz; do \
	    gof3r put --no-md5 --acl=$(ACL) -b ${BUCKET} -k rsbin/$(NAME)/$(TRAVIS_COMMIT)/$$f <$$f; \
	    if [ "$(TRAVIS_PULL_REQUEST)" = "false" ]; then \
	      gof3r put --no-md5 --acl=$(ACL) -b ${BUCKET} -k rsbin/$(NAME)/$(TRAVIS_BRANCH)/$$f <$$f; \
	    fi; \
	  done)

# produce a version string that is embedded into the binary that captures the branch, the date
# and the commit we're building
version:
	@echo "package main; const VV = \"$(NAME) $(TRAVIS_BRANCH) - $(DATE) - $(TRAVIS_COMMIT)\"" \
	  >version.go
	@echo "version.go: `cat version.go`"

# Installing build dependencies is a bit of a mess. Don't want to spend lots of time in
# Travis doing this. The folllowing just relies on go get no reinstalling when it's already
# there, like your laptop.
depend:
	go get $(DEPEND)

clean:
	rm -rf build _aws-sdk
	@echo "package main; const VV = \"$(NAME) unversioned - $(DATE)\"" >version.go

travis-test:
	ginkgo -r -cover

test:
	ginkgo -r
	ginkgo -r -cover
	go tool cover -func=`basename $$PWD`.coverprofile
