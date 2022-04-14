package config

import (
	"log"
	"strings"
)

var (
	BuildName    = "buildName"
	BuildVersion = "开发版本"
	BuildTime    = "现在"
	CommitID     = "未 commit"
	GoVersion    = "latest"
)

func DisplayVersion() {
	log.Printf("Build name:\t%s\n", BuildName)
	log.Printf("Build ver:\t%s\n", BuildVersion)
	log.Printf("Build time:\t%s\n", BuildTime)
	log.Printf("Git commit:\t%s\n", CommitID)
	log.Printf("Go version:\t%s\n", strings.Split(GoVersion, "go version ")[1])
}
