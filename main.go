package main

import (
	"runtime/debug"

	"github.com/magicdrive/kjak/cmd"
)

var version string

func main() {
	cmd.Execute(Version())
}

func Version() string {
	if version != "" {
		return version
	}

	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		return buildInfo.Main.Version
	}
	return "version unknown"
}
