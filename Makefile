BUILD_VERSION:=v1.0.0
BUILD_TIME:=$(shell date "+%F %T")
BUILD_NAME:=nginx-proxy-manager
COMMIT_SHA1:=$(shell git rev-parse HEAD )
CONFIG_PATH=github.com/qfdk/nginx-proxy-manager/app/config

LDFLAGS=-ldflags "-X ${CONFIG_PATH}.BuildName=${BUILD_NAME} \
-X ${CONFIG_PATH}.CommitID=${COMMIT_SHA1} \
-X '${CONFIG_PATH}.BuildTime=${BUILD_TIME}' \
-X ${CONFIG_PATH}.BuildVersion=${BUILD_VERSION}"

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