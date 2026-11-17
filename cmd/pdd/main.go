package main

import (
	"os"
	"runtime/debug"

	"github.com/cruskell/pdd-kit-go/internal/cli"
)

// Set by release builds. go install @version obtains the module version below.
var version string

func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "0.2.1-dev"
}

func main() { os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, buildVersion())) }
