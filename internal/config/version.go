package config

import (
	"log"
	"strings"
	"time"
)

var (
	BuildName    = "buildName"
	BuildVersion = "开发版本"
	BuildTime    = time.Now().Format("2006-01-02 15:04:05")
	CommitID     = "尚未提交"
	GoVersion    = "go version 开发环境"
)

func DisplayVersion() {
	log.Printf("Build name:\t%s\n", BuildName)
	log.Printf("Build ver:\t%s\n", BuildVersion)
	log.Printf("Build time:\t%s\n", BuildTime)
	log.Printf("Git commit:\t%s\n", CommitID)
	log.Printf("Go version:\t%s\n", strings.Split(GoVersion, "go version ")[1])
}
