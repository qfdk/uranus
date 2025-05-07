package config

import (
	"log"
	"strings"
)

var (
	BuildName    = "buildName"
	BuildVersion = "开发版本"
	BuildTime    = "未知构建时间" // This will be set during build via ldflags
	CommitID     = "DEV888888"
	GoVersion    = "go version 开发环境"
)

func DisplayVersion() {
	log.Printf("Build name:\t%s\n", BuildName)
	log.Printf("Build ver:\t%s\n", BuildVersion)
	log.Printf("Build time:\t%s\n", BuildTime)
	log.Printf("Git commit:\t%s\n", CommitID)
	log.Printf("Go version:\t%s\n", strings.Split(GoVersion, "go version ")[1])
}
