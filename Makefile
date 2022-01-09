BUILD_VERSION   := v1.0.0
#BUILD_TIME      := $(shell date "+%F %T")
#BUILD_NAME      := proxy-manager_$(shell date "+%Y%m%d%H" )
#COMMIT_SHA1     := $(shell git rev-parse HEAD )
#GO_VERSION      := $(shell go version)

.PHONY: build clean help

all: build

build:
	go build -v .

clean:
	rm -rf proxy-manager
	go clean -i .

help:
	@echo "make: compile packages and dependencies"
	@echo "make clean: remove object files and cached files"