BUILD_VERSION:=v1.0.0
BUILD_TIME:=$(shell date "+%F %T")
BUILD_NAME:=uranus
COMMIT_SHA1:=$(shell git rev-parse HEAD )
GoVersion:=$(shell go version)
CONFIG_PATH=uranus/internal/config

LDFLAGS=-ldflags "-X ${CONFIG_PATH}.BuildName=${BUILD_NAME} \
-X ${CONFIG_PATH}.CommitID=${COMMIT_SHA1} \
-X '${CONFIG_PATH}.BuildTime=${BUILD_TIME}' \
-X '${CONFIG_PATH}.GoVersion=${GoVersion}' \
-X ${CONFIG_PATH}.BuildVersion=${BUILD_VERSION}"

.PHONY: build clean release help

all: clean build

build:
	go build ${LDFLAGS} -v .
release:
	# build time
	echo ${BUILD_TIME}>/tmp/release_time
	# Clean
	go clean
	rm -rf dist
	mkdir dist
	rm -rf *.gz
	# Build for mac
#	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build ${LDFLAGS} -v .
#	tar czvf ${BUILD_NAME}-darwin-amd64-${BUILD_VERSION}.tar.gz ./${BUILD_NAME}
	# Build for mac arm64
#	go clean
#	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build ${LDFLAGS} -v .
#	tar czvf ${BUILD_NAME}-darwin-arm64-${BUILD_VERSION}.tar.gz ./${BUILD_NAME}
	# Build for linux
	go clean
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ${LDFLAGS} -v .
	tar czvf ${BUILD_NAME}-linux64-${BUILD_VERSION}.tar.gz ./${BUILD_NAME}
	go clean
	mv *.gz dist

clean:
	rm -rf uranus
	go clean -i .

help:
	@echo "make: compile packages and dependencies"
	@echo "make clean: remove object files and cached files"