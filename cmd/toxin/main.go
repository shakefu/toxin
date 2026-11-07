// Command toxin is the toxin command line tool: with a subcommand it
// manages the etcd configuration (get, set, dump, list, clear, load), and
// without one it is the pyconfig-style scanner over Go source.
package main

import (
	"os"

	"github.com/shakefu/toxin/internal/cli"
)

// version is set at link time:
// -ldflags "-X main.version=$(git describe --tags)"
var version = "dev"

func main() {
	cli.Version = version
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}
