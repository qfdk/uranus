package config

import (
	"log"
	"strings"
)

var (
	BuildVersion string
	BuildTime    string
	BuildName    string
	CommitID     string
	GoVersion    string
)

func DisplayVersion() {
	log.Printf("Build name:\t%s\n", BuildName)
	log.Printf("Build ver:\t%s\n", BuildVersion)
	log.Printf("Build time:\t%s\n", BuildTime)
	log.Printf("Git commit:\t%s\n", CommitID)
	log.Printf("Go version:\t%s\n", strings.Split(GoVersion, "go version ")[1])
}
