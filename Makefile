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
	echo ${BUILD_TIME}>/tmp/release_time
	go build ${LDFLAGS} -v .
release:
	# build time
	echo ${BUILD_TIME}>/tmp/release_time
	# Clean
	go clean
	rm -rf dist
	mkdir dist

	# Build for linux amd64
	go clean
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ${LDFLAGS} -v .
	#tar czvf ${BUILD_NAME}-amd64-${BUILD_VERSION}.tar.gz ./${BUILD_NAME}
	cp ./${BUILD_NAME} ./dist/${BUILD_NAME}-amd64

	# Build for linux arm64
	# apt-get install -y gcc-aarch64-linux-gnu
	go clean
	CC=aarch64-linux-gnu-gcc CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build ${LDFLAGS} -v .
	#tar czvf ${BUILD_NAME}-arm64-${BUILD_VERSION}.tar.gz ./${BUILD_NAME}
	cp ./${BUILD_NAME} ./dist/${BUILD_NAME}-arm64

	go clean
clean:
	rm -rf uranus
	go clean -i .

help:
	@echo "make: compile packages and dependencies"
	@echo "make clean: remove object files and cached files"