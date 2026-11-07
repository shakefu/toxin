package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/shakefu/toxin"
	"github.com/shakefu/toxin/internal/testutil"
)

// TestMain starts the shared etcd container for the etcd-backed tests and
// terminates it once the whole binary has run.
func TestMain(m *testing.M) {
	code := m.Run()
	testutil.Cleanup()
	os.Exit(code)
}

// runCLI drives Main with argv, capturing stdout, stderr and the exit
// code.
func runCLI(args ...string) (string, string, int) {
	var out, errOut bytes.Buffer
	code := Main(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

// etcdEnv points the CLI's configurations at the shared etcd container.
func etcdEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))
}

// newTestConfig constructs a library Config on prefix against the shared
// etcd container, for arranging and asserting etcd state.
func newTestConfig(t *testing.T, prefix string, opts ...toxin.Option) *toxin.Config {
	t.Helper()
	etcdEnv(t)
	opts = append([]toxin.Option{toxin.WithPrefix(prefix)}, opts...)
	conf, err := toxin.New(opts...)
	if err != nil {
		t.Fatalf("New(%q): %v", prefix, err)
	}
	return conf
}

// cli.Main RETURN: --help or -h prints the general usage — the quoted
// usage line, each command name with a one-line help, and the global
// options — and exits zero.
func TestMainHelpPrintsGeneralUsage(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		out, errOut, code := runCLI(flag)
		if code != 0 {
			t.Fatalf("%s: exit code %d, stderr %q", flag, code, errOut)
		}
		if !strings.Contains(out, "Usage: toxin <command> [options]\n") {
			t.Errorf("%s: general usage missing the usage line:\n%s", flag, out)
		}
		for _, spec := range commandSpecs {
			if !strings.Contains(out, spec.name+" ") {
				t.Errorf("%s: general usage missing command %q:\n%s", flag, spec.name, out)
			}
			if !strings.Contains(out, spec.help) {
				t.Errorf("%s: general usage missing help for %q:\n%s", flag, spec.name, out)
			}
		}
		for _, label := range []string{"-n, --no-inherit", "-v, --verbose", "--version"} {
			if !strings.Contains(out, label) {
				t.Errorf("%s: general usage missing global option %q:\n%s", flag, label, out)
			}
		}
	}
}

// cli.Main RETURN: --help with a named command prints that command's
// usage and exits zero.
func TestMainHelpPrintsCommandUsage(t *testing.T) {
	out, _, code := runCLI("get", "--help")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if !strings.Contains(out, "Usage: toxin get <prefix> <key> [options]\n") {
		t.Errorf("get usage missing its usage line:\n%s", out)
	}
	if !strings.Contains(out, "Configuration key") {
		t.Errorf("get usage missing the key operand help:\n%s", out)
	}
	// The named command wins wherever it stands.
	out, _, _ = runCLI("--help", "get")
	if !strings.Contains(out, "Usage: toxin get <prefix> <key> [options]") {
		t.Errorf("--help get did not print the get usage:\n%s", out)
	}
}

// cli.Main RETURN: --version prints the version and exits zero.
func TestMainVersionPrintsVersion(t *testing.T) {
	old := Version
	Version = "vtest-version"
	defer func() { Version = old }()
	out, _, code := runCLI("--version")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if out != "vtest-version\n" {
		t.Errorf("stdout = %q", out)
	}
}

// cli.Main INVARIANT: --help or -h resolves before --version, and
// --version before subcommand dispatch.
func TestMainHelpVersionDispatchPrecedence(t *testing.T) {
	out, _, code := runCLI("--version", "--help")
	if code != 0 {
		t.Fatalf("--version --help: exit code %d", code)
	}
	if !strings.Contains(out, "Usage: toxin <command> [options]") {
		t.Errorf("--version --help did not resolve help first:\n%s", out)
	}

	out, _, code = runCLI("--version", "get", "cfg/", "key")
	if code != 0 {
		t.Fatalf("--version get: exit code %d", code)
	}
	if out != Version+"\n" {
		t.Errorf("--version get printed %q, wanted the version alone", out)
	}
}

// cli.Main ERROR: the first positional operand naming no command prints
// "toxin: no such command '<name>'" to stderr and exits one — including
// the members cobra would add to the tree but the contract does not.
func TestMainNoSuchCommand(t *testing.T) {
	for _, name := range []string{"bogus", "help", "completion"} {
		out, errOut, code := runCLI(name)
		if code != 1 {
			t.Fatalf("%s: exit code %d", name, code)
		}
		if out != "" {
			t.Errorf("%s: stdout = %q, wanted none", name, out)
		}
		if errOut != "toxin: no such command '"+name+"'\n" {
			t.Errorf("%s: stderr = %q", name, errOut)
		}
	}
}

// cli.Main ERROR: no command and no scanner target prints "command
// argument is required" with the usage to stderr and exits one.
func TestMainCommandArgumentRequired(t *testing.T) {
	for _, args := range [][]string{{}, {"-v"}, {"-c"}} {
		out, errOut, code := runCLI(args...)
		if code != 1 {
			t.Fatalf("%v: exit code %d", args, code)
		}
		if out != "" {
			t.Errorf("%v: stdout = %q, wanted none", args, out)
		}
		if !strings.HasPrefix(errOut, "command argument is required\n") {
			t.Errorf("%v: stderr = %q", args, errOut)
		}
		if !strings.Contains(errOut, "Usage: toxin <command> [options]") {
			t.Errorf("%v: stderr lacks the usage:\n%s", args, errOut)
		}
	}
}

// cli.Main ERROR: a command's required positional argument missing prints
// "<argument> argument is required" with the command usage to stderr and
// exits one.
func TestMainMissingPositionalArgument(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"get"}, "prefix argument is required"},
		{[]string{"get", "cfg/"}, "key argument is required"},
		{[]string{"set", "cfg/", "key"}, "value argument is required"},
		{[]string{"clear"}, "prefix argument is required"},
		{[]string{"load", "cfg/"}, "file argument is required"},
	}
	for _, tc := range cases {
		out, errOut, code := runCLI(tc.args...)
		if code != 1 {
			t.Fatalf("%v: exit code %d", tc.args, code)
		}
		if out != "" {
			t.Errorf("%v: stdout = %q, wanted none", tc.args, out)
		}
		if !strings.Contains(errOut, tc.want) {
			t.Errorf("%v: stderr = %q, wanted %q", tc.args, errOut, tc.want)
		}
		// The error comes with the command usage, not the general one.
		if !strings.Contains(errOut, "Usage: toxin "+tc.args[0]+" ") {
			t.Errorf("%v: stderr lacks the command usage:\n%s", tc.args, errOut)
		}
	}
}

// cli.Main ERROR: an unknown flag — including a scanner-only flag — in
// command mode prints the parse error and the usage to stderr and exits
// two.
func TestMainUnknownFlagInCommandModeExitsTwo(t *testing.T) {
	cases := [][]string{
		{"get", "-f", "x.go", "cfg/", "key"},
		{"-f", "x.go", "get", "cfg/", "key"},
		{"get", "cfg/", "key", "--nope"},
		{"--nope", "get", "cfg/", "key"},
	}
	for _, args := range cases {
		out, errOut, code := runCLI(args...)
		if code != 2 {
			t.Fatalf("%v: exit code %d, stderr %q", args, code, errOut)
		}
		if out != "" {
			t.Errorf("%v: stdout = %q, wanted none", args, out)
		}
		if !strings.Contains(errOut, "flag") || !strings.Contains(errOut, "unknown") {
			t.Errorf("%v: stderr lacks a parse error:\n%s", args, errOut)
		}
		if !strings.Contains(errOut, "Usage: toxin get <prefix> <key> [options]") {
			t.Errorf("%v: stderr lacks the command usage:\n%s", args, errOut)
		}
	}
}

// cli.Main INPUT: -v repeated in command mode lowers the log level one
// step per repeat from critical toward silly and holds it at silly. The
// missing-key warning is visible at warn and hidden at critical.
func TestMainVerboseRepeatsLowerLogLevel(t *testing.T) {
	etcdEnv(t)

	libErr := captureStderrLog(func() {
		out, _, code := runCLI("get", "tverbose/", "nokey")
		if code != 1 || out != "" {
			t.Fatalf("get unset: exit %d stdout %q", code, out)
		}
	})
	if strings.Contains(libErr, "Key not found") {
		t.Errorf("critical level logged a warning: %q", libErr)
	}

	libErr = captureStderrLog(func() {
		_, _, _ = runCLI("-v", "get", "tverbose/", "nokey")
	})
	if !strings.Contains(libErr, "Key not found") {
		t.Errorf("-v did not lower the level to warn; stderr %q", libErr)
	}

	// Held at silly: no amount of repeats errors or goes quieter.
	libErr = captureStderrLog(func() {
		_, _, code := runCLI("-vvvvvv", "get", "tverbose/", "nokey")
		if code != 1 {
			t.Fatalf("-vvvvvv get unset: exit %d", code)
		}
	})
	if !strings.Contains(libErr, "Key not found") {
		t.Errorf("-vvvvvv did not hold at silly; stderr %q", libErr)
	}
}

// cli.Main INPUT: -n in command mode constructs the configuration with
// inheritance disabled: an inherited key is found without it and unset
// with it.
func TestMainNoInheritDisablesInheritance(t *testing.T) {
	base := newTestConfig(t, "tinherit-base/")
	if err := base.Set("shared.key", "from-base"); err != nil {
		t.Fatalf("set base key: %v", err)
	}
	child := newTestConfig(t, "tinherit-child/")
	if err := child.Set("config.inherit", "tinherit-base/"); err != nil {
		t.Fatalf("set inherit key: %v", err)
	}

	out, _, code := runCLI("get", "tinherit-child/", "shared.key")
	if code != 0 || out != "\"from-base\"\n" {
		t.Fatalf("inherited get: exit %d stdout %q", code, out)
	}

	out, errOut, code := runCLI("-n", "get", "tinherit-child/", "shared.key")
	if code != 1 || out != "" || errOut != "" {
		t.Fatalf("-n get: exit %d stdout %q stderr %q", code, out, errOut)
	}

	out, _, code = runCLI("--no-inherit", "get", "tinherit-child/", "shared.key")
	if code != 1 || out != "" {
		t.Fatalf("--no-inherit get: exit %d stdout %q", code, out)
	}
}

// captureStderrLog runs fn with the process stderr swapped to a pipe, so
// library logging written to the real os.Stderr is observable, and
// returns what was logged.
func captureStderrLog(fn func()) string {
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		fn()
		return ""
	}
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}
