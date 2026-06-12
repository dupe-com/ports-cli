// Command ports is an interactive port manager: see what's listening, who
// owns it, and reclaim ports in one keypress — plus managed kubectl
// port-forwards and Cloudflare tunnel visibility.
package main

import (
	"fmt"
	"os"

	"github.com/dupe-com/ports-cli/internal/cli"
)

// Injected by goreleaser / Makefile via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := cli.Execute(version, commit, date); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
