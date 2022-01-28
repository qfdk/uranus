BUILD_VERSION   := v1.0.0
BUILD_TIME      := $(shell date "+%F %T")
BUILD_NAME      := nginx-proxy-manager_$(shell date "+%Y%m%d%H" )
COMMIT_SHA1     := $(shell git rev-parse HEAD )
GO_VERSION      := $(shell go version)

LDFLAGS=-ldflags \
"-X 'main.BuildName=${BUILD_NAME}' \
-X 'main.BuildVersion=${BUILD_VERSION}' \
-X 'main.BuildTime=${BUILD_TIME}' \
-X 'main.CommitID=${COMMIT_SHA1}' \
-X 'main.GoVersion=${GO_VERSION}'"

.PHONY: build clean help

all: build

build:
	go build ${LDFLAGS} -v .

clean:
	rm -rf github.com/qfdk/nginx-proxy-manager
	go clean -i .

help:
	@echo "make: compile packages and dependencies"
	@echo "make clean: remove object files and cached files"