package main

import (
	"fmt"
)

var (
	BuildVersion string
	BuildTime    string
	BuildName    string
	CommitID     string
	GoVersion    string
)

func displayVersion() {
	fmt.Printf("Build name:\t%s\n", BuildName)
	fmt.Printf("Build ver:\t%s\n", BuildVersion)
	fmt.Printf("Build time:\t%s\n", BuildTime)
	fmt.Printf("Git commit:\t%s\n", CommitID)
	fmt.Printf("Go version:\t%s\n", GoVersion)
}
