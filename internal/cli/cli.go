// Package cli implements the toxin command line tool on the venom engine.
// Venom generates the six etcd management subcommands (get, set, dump, list,
// clear and load) from annotated functions; this package wraps the built
// command tree with the contract's top-level behavior: the scanner as the
// root behavior when no subcommand is given, the help → version → dispatch
// precedence, and the usage and error shapes with their exit codes.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// Version is the string the --version option prints. It defaults to "dev";
// a release build sets it at link time with
// -ldflags "-X github.com/shakefu/toxin/internal/cli.Version=<version>".
var Version = "dev"

// The writers commands print through. Main swaps them for the caller's
// writers and restores the process defaults on return, so tests can capture
// output while library logging keeps the real stderr.
var (
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

// logLevels orders the toxin log levels from silly toward critical, the
// ladder a repeated -v walks down. It starts at critical, four steps above
// silly.
var logLevels = [...]string{"silly", "debug", "info", "warn", "critical"}

// globals carries the command-mode global options resolved for one run:
// -v/--verbose repeated and -n/--no-inherit.
type globals struct {
	verbose   int
	noInherit bool
}

// level derives the toxin log level from the -v repeat count: one step per
// repeat from critical toward silly, holding at silly.
func (g globals) level() string {
	idx := len(logLevels) - 1 // critical
	for i := 0; i < g.verbose && idx > 0; i++ {
		idx--
	}
	return logLevels[idx]
}

// runGlobals is the resolved global options for the dispatch in flight.
var runGlobals globals

// commandArgs holds the positional operands — the command name excluded —
// of the dispatch in flight, so commands can see positional presence the
// generated bindings cannot express (clear's optional second operand).
var commandArgs []string

// Main runs the toxin CLI with argv (the program name excluded), writing
// command output to out and diagnostics to errW, and returns the process
// exit code.
func Main(argv []string, out, errW io.Writer) int {
	stdout, stderr = out, errW
	defer func() { stdout, stderr = os.Stdout, os.Stderr }()
	if err := run(argv); err != nil {
		var ee exitError
		if errors.As(err, &ee) {
			return ee.code
		}
		var coder interface{ ErrorCode() int }
		if errors.As(err, &coder) {
			return coder.ErrorCode()
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// run is the dispatch loop: tokenize, resolve help and version, then either
// dispatch a subcommand or run the scanner as the root behavior.
func run(argv []string) error {
	toks := tokenize(argv)

	// --help/-h and --version are global in both modes and resolve before
	// anything else: help first, then version, then dispatch. They must
	// stand alone as tokens; a cluster like -hv is a parse error.
	rest := make([]string, 0, len(toks.flags))
	help, version := false, false
	for _, f := range toks.flags {
		switch f {
		case "-h", "--help":
			help = true
		case "--version":
			version = true
		default:
			rest = append(rest, f)
		}
	}

	if help {
		if len(toks.positionals) > 0 {
			if spec, ok := commandByName(toks.positionals[0]); ok {
				fmt.Fprint(stdout, commandUsage(spec))
				return nil
			}
		}
		fmt.Fprint(stdout, generalUsage())
		return nil
	}
	if version {
		fmt.Fprintln(stdout, Version)
		return nil
	}

	if len(toks.positionals) > 0 {
		return runCommand(toks.positionals[0], toks.positionals[1:], rest)
	}
	return runScannerRoot(rest)
}

// runCommand dispatches a named subcommand: the remaining flag tokens may
// only be the command-mode globals (-v/--verbose, -n/--no-inherit), the
// positionals must satisfy the command's requirements, and dispatch itself
// goes through the venom-built cobra tree.
func runCommand(name string, args []string, flags []string) error {
	spec, ok := commandByName(name)
	if !ok {
		return fail("toxin: no such command '%s'", name)
	}

	g := globals{}
	for _, f := range flags {
		switch {
		case f == "--verbose":
			g.verbose++
		case f == "--no-inherit":
			g.noInherit = true
		case strings.HasPrefix(f, "--"):
			return parseError("unknown flag: "+f, commandUsage(spec))
		case len(f) > 1 && f[0] == '-':
			// A shorthand cluster: every letter must be a global short.
			for _, c := range f[1:] {
				switch c {
				case 'v':
					g.verbose++
				case 'n':
					g.noInherit = true
				default:
					return parseError(fmt.Sprintf("unknown shorthand flag: %q in %s", string(c), f), commandUsage(spec))
				}
			}
		default:
			return parseError("unknown flag: "+f, commandUsage(spec))
		}
	}

	// A required positional may not be missing; extra operands beyond the
	// declared ones are ignored, as jetconfig's position-indexed options
	// did.
	for i, a := range spec.args {
		if a.required && i >= len(args) {
			return fail("%s argument is required\n%s", a.name, commandUsage(spec))
		}
	}

	runGlobals = g
	commandArgs = args

	root, err := buildTree()
	if err != nil {
		return err
	}
	root.SetArgs(append([]string{name}, args...))
	return root.ExecuteContext(context.Background())
}

// runScannerRoot runs the scanner as the root behavior: no subcommand was
// given, so the remaining flags belong to the scanner's own flag set and a
// target (-f or -m) is required.
func runScannerRoot(flags []string) error {
	opts, err := parseScanFlags(flags)
	if err != nil {
		return parseError(err.Error(), scannerUsage())
	}
	if opts.filename == "" && opts.module == "" {
		return fail("command argument is required\n%s", generalUsage())
	}

	root, err := buildTree()
	if err != nil {
		return err
	}
	root.RunE = func(*cobra.Command, []string) error { return runScan(opts) }
	root.SetArgs([]string{})
	return root.ExecuteContext(context.Background())
}

// parseError prints a parse failure with the relevant usage and returns
// exitError for exit code two.
func parseError(msg, usage string) error {
	fmt.Fprintf(stderr, "%s\n%s", msg, usage)
	return exitError{code: 2}
}

// tokens splits argv into flag tokens and positional operands. Everything
// from a bare "--" on is positional; a lone "-" is positional too. The
// target flags -f/--filename and -m/--module take a separate value, so the
// token after one is its value, not a positional — a path like
// "internal/cli/testdata/x.go" must not read as a command name.
type tokens struct {
	flags       []string
	positionals []string
}

// takesValue reports whether a flag token consumes the following token as
// its value.
func takesValue(flag string) bool {
	switch flag {
	case "-f", "--filename", "-m", "--module":
		return true
	}
	return false
}

func tokenize(argv []string) tokens {
	var t tokens
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "--":
			t.positionals = append(t.positionals, argv[i+1:]...)
			return t
		case strings.HasPrefix(a, "--") || (len(a) > 1 && a[0] == '-'):
			t.flags = append(t.flags, a)
			if takesValue(a) && i+1 < len(argv) && argv[i+1] != "--" {
				i++
				t.flags = append(t.flags, argv[i])
			}
		default:
			t.positionals = append(t.positionals, a)
		}
	}
	return t
}
