#! /usr/bin/make
#
# Makefile for rest-aws
#
# Top-level targets:
# default: compile rest-aws, you can thus use make && ./rest-aws -options ...
# build: builds binaries for linux and darwin
# test: runs unit tests recursively and produces code coverage stats and shows them
# travis-test: just runs unit tests recursively
# clean: removes build stuff
#
# HACKS - a couple of things here are unconventional in order to keep travis-ci fast
# - use 'godep save' on your laptop if you add dependencies, but we don't use godep in the
#   makefile, instead, we simply add the godep workspace to the GOPATH
# - the godep workspace has the source for `go cover`, `ginkgo`, and `go3fr` so we don't have
#   to go get them each time, if you blow away ./Godeps then use `make depend` to put them back
# - we have ginkgo in the Godep workspace, the build binary gets put there, so we need the
#   workspace bin directory in the path
# - we have go cover in the Godep workspace, the build binary needs to go into the go tools dir

NAME=rightlink_cmd
BUCKET=rightlinklite
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

# the standard build produces a linux tgz
build: build/$(NAME) # .tgz

# TODO: convert from RLL...
build/$(NAME).tgz: build/$(NAME)
	rm -rf build/$(NAME)
	mkdir -p build/$(NAME)
	#cp script/rightlinklite* build/rll
	cp build/$(NAME) build/$(NAME)
	sed -i -e "s/BRANCH/$(TRAVIS_BRANCH)/" build/*.sh
	cd build; tar zcf $(NAME).tgz ./$(NAME)
	rm -r build/$(NAME)

build/$(NAME): *.go version depend
	@mkdir -p build
	GOOS=linux GOARCH=amd64 go build -o build/$(NAME) .

# #TODO: convert from RLL
upload:
	@echo "Upload needs to be implemented in the Makefile"
# this assumes you have AWS_ACCESS_KEY_ID and AWS_SECRET_KEY env variables set,
# which happens in the .travis.yml for CI
#upload: depend
#	@which gof3r >/dev/null || (echo 'Please "go get github.com/rlmcpherson/s3gof3r/gof3r"'; false)
#	(cd build; set -ex; for f in *; do \
#	 gof3r put -b ${BUCKET} -k rll/$(TRAVIS_BRANCH)/$$f -m x-amz-acl:public-read <$$f; \
#	done)
#	gof3r put -b ${BUCKET} -k rll/$(TRAVIS_BRANCH)/upgrades -m x-amz-acl:public-read <upgrades
#@which s3cmd >/dev/null || (echo Please install s3cmd from http://s3tools.org; false)
#s3cmd -P -c ./.s3cfg --force put build/* s3://${BUCKET}/$(TRAVIS_BRANCH)/

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
	go get golang.org/x/tools/cmd/cover
	go get github.com/onsi/ginkgo/ginkgo
	go get github.com/rlmcpherson/s3gof3r/gof3r

# The targets below were an attempt to preinstall this stuff in the repository
#depend: Godeps/_workspace/bin/ginkgo Godeps/_workspace/bin/gof3r
#depend: Godeps/_workspace/bin/cover Godeps/_workspace/bin/ginkgo Godeps/_workspace/bin/gof3r
# go install will not produce ./Godeps/_workspace/bin/cover if you have 'cover' installed
# in $GOROOT (which is quite OK), hence we ignore if the cp fails
#Godeps/_workspace/bin/cover:
#	go get golang.org/x/tools/cmd/cover
#	#go install golang.org/x/tools/cmd/cover # Go1.3 version
#	#@cp ./Godeps/_workspace/bin/cover `go env GOTOOLDIR` 2>/dev/null || \
#	#  echo "Using already installed go cover"
#Godeps/_workspace/bin/ginkgo:
#	go get github.com/onsi/ginkgo/ginkgo
#Godeps/_workspace/bin/gof3r:
#	go get github.com/rlmcpherson/s3gof3r/gof3r

clean:
	rm -rf build _aws-sdk
	@echo "package main; const VV = \"$(NAME) unversioned - $(DATE)\"" >version.go

travis-test:
	ginkgo -r -cover

test:
	ginkgo -r
	ginkgo -r -cover
	go tool cover -func=$(NAME).coverprofile

# Pull fresh metadata for AWS services by cloning the AWS Ruby SDK and copying out the
# relevant directory. This is pretty slow...
meta-pull:
	git clone https://github.com/aws/aws-sdk-core-ruby _aws-sdk
	mv _aws-sdk/aws-sdk-core/apis aws-metadata
	rm -rf _aws-sdk

# Update the RightScale-massaged metadata by passing the AWS metadata through the
# rest-analyzer tool
meta-analyze: ./aws-metadata ./analyze
	mkdir -p ./rest-metadata
	rm -f ./rest-metadata/*
	for s in ./aws-metadata/*.resources.json; do \
	  svc=`basename -s .resources.json $$s`; \
	  ./analyze --service $$svc --path ./aws-metadata --resource-only --force \
		  >./rest-metadata/$$svc.yaml; \
	done
aws-metadata:
	@echo "AWS metadata is missing from ./aws-metadata, it should be checked into the"
	@echo "repo, but you can pull it using make meta-pull if you have to"
	@exit 1
analyze:
	@echo "You need a local symlink that points to the (ruby) rest-analyzer program"
	@echo "like: ln -s ~/src/rest-analyzer/bin/analyzer.rb analyze"
	@exit 1
