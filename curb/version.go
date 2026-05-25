package main

import (
	"fmt"
	"io"
	"runtime/debug"
)

// printVersion writes module version, VCS commit (when available), and the
// Go toolchain that built the binary. All info comes from build metadata
// embedded by the Go toolchain — no external state needed.
func printVersion(out io.Writer) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		fmt.Fprintln(out, "curb (build info unavailable)")
		return
	}
	version := info.Main.Version
	if version == "" {
		version = "(devel)"
	}
	var rev string
	modified := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	fmt.Fprintf(out, "curb %s\n", version)
	if rev != "" {
		short := rev
		if len(short) > 12 {
			short = short[:12]
		}
		suffix := ""
		if modified {
			suffix = " (modified)"
		}
		fmt.Fprintf(out, "commit %s%s\n", short, suffix)
	}
	fmt.Fprintf(out, "built with %s\n", info.GoVersion)
}
