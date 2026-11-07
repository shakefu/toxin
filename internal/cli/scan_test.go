package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scanFile targets the calls fixture; every byte-pinned expectation below
// was derived from this file.
const scanFile = "testdata/scan/calls.go"

// cli.Scan RETURN: the default namespace view — one line per remaining
// recorded call, newline-terminated, values in Go literal syntax, keys
// sorted alphabetically.
func TestScanNamespaceView(t *testing.T) {
	out, errOut, code := runCLI("-f", scanFile, "-c")
	if code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	want := `<"sample." + suffix> = <not set>
<config.Keys.sample> = <not set>
keyVar = <not set>
sample.composite = []string{"a", "b"}
sample.dedup = "fallback"
sample.map = map[string]int{"a": 1}
sample.multi = 1, "two", false
sample.neg = -1
sample.nil = nil
sample.num = 10
sample.plain = <not set>
sample.strict = true
sample.unparsed = <unparsed>
sample.withdefault = "default value"
`
	if out != want {
		t.Errorf("stdout =\n%q\nwant\n%q", out, want)
	}
}

// cli.Scan RETURN: without --all a recorded call that carries no default
// is omitted when another recorded call for the same key carries one;
// with --all both calls are emitted.
func TestScanOmitsAndRestoresDefaultlessDuplicate(t *testing.T) {
	out, _, _ := runCLI("-f", scanFile, "-c")
	if got := strings.Count(out, "sample.dedup"); got != 1 {
		t.Errorf("default view shows %d sample.dedup lines:\n%s", got, out)
	}
	if !strings.Contains(out, "sample.dedup = \"fallback\"\n") {
		t.Errorf("default view lacks the carrying call:\n%s", out)
	}

	out, _, _ = runCLI("-f", scanFile, "-c", "-a")
	if got := strings.Count(out, "sample.dedup"); got != 2 {
		t.Errorf("--all view shows %d sample.dedup lines:\n%s", got, out)
	}
	if !strings.Contains(out, "sample.dedup = <not set>\n") {
		t.Errorf("--all view lacks the defaultless call:\n%s", out)
	}
}

// cli.Scan RETURN: --view-call emits the call as written.
func TestScanViewCall(t *testing.T) {
	out, _, code := runCLI("-f", scanFile, "-c", "--view-call")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	want := `toxin.Get("<\"sample.\" + suffix>")
toxin.Get("<config.Keys.sample>")
toxin.Get("keyVar")
toxin.Get("sample.composite", []string{"a", "b"})
toxin.Get("sample.dedup", "fallback")
toxin.Get("sample.map", map[string]int{"a": 1})
toxin.Get("sample.multi", 1, "two", false)
toxin.Get("sample.neg", -1)
toxin.Get("sample.nil", nil)
toxin.Set("sample.num", 10)
toxin.Get("sample.plain")
toxin.GetStrict("sample.strict", true)
toxin.Get("sample.unparsed", <unparsed>)
toxin.Get("sample.withdefault", "default value")
`
	if out != want {
		t.Errorf("stdout =\n%q\nwant\n%q", out, want)
	}

	// The -v short flag is the scanner's --view-call.
	outV, _, _ := runCLI("-v", "-f", scanFile, "-c")
	if outV != want {
		t.Errorf("-v stdout =\n%q", outV)
	}
}

// cli.Scan RETURN: --only-keys emits each distinct key once with no
// value.
func TestScanOnlyKeys(t *testing.T) {
	out, _, code := runCLI("-f", scanFile, "-c", "-k")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	want := `<"sample." + suffix>
<config.Keys.sample>
keyVar
sample.composite
sample.dedup
sample.map
sample.multi
sample.neg
sample.nil
sample.num
sample.plain
sample.strict
sample.unparsed
sample.withdefault
`
	if out != want {
		t.Errorf("stdout =\n%q\nwant\n%q", out, want)
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.Contains(line, "=") {
			t.Errorf("only-keys line carries a value: %q", line)
		}
	}
}

// cli.Scan RETURN: an absent default renders as "<not set>" and an
// unreadable default as "<unparsed>".
func TestScanDefaultRenderings(t *testing.T) {
	out, _, _ := runCLI("-f", scanFile, "-c")
	if !strings.Contains(out, "sample.plain = <not set>\n") {
		t.Errorf("absent default not rendered as <not set>:\n%s", out)
	}
	if !strings.Contains(out, "sample.unparsed = <unparsed>\n") {
		t.Errorf("unreadable default not rendered as <unparsed>:\n%s", out)
	}
}

// cli.Scan INPUT: a bare identifier key displays as the identifier; any
// other non-literal key expression displays as its source text wrapped in
// angle brackets.
func TestScanNonLiteralKeys(t *testing.T) {
	out, _, _ := runCLI("-f", scanFile, "-c")
	if !strings.Contains(out, "keyVar = <not set>\n") {
		t.Errorf("identifier key not displayed as the identifier:\n%s", out)
	}
	if !strings.Contains(out, `<"sample." + suffix> = <not set>`+"\n") {
		t.Errorf("expression key not wrapped in angle brackets:\n%s", out)
	}
	if !strings.Contains(out, "<config.Keys.sample> = <not set>\n") {
		t.Errorf("selector key not wrapped in angle brackets:\n%s", out)
	}
}

// cli.Scan INPUT: every package-qualified call to toxin.Get, toxin.Set or
// toxin.GetStrict is recorded with its method — and nothing else: calls on
// other receivers, other methods and calls without a key argument are not.
func TestScanRecordsOnlyToxinCalls(t *testing.T) {
	out, _, _ := runCLI("-f", scanFile, "-c", "-v")
	for _, want := range []string{
		`toxin.Set("sample.num", 10)`,
		`toxin.GetStrict("sample.strict", true)`,
		`toxin.Get("sample.plain")`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"other.Get", "toxin.Other"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("recorded %q in:\n%s", unwanted, out)
		}
	}
}

// cli.Scan RETURN: output sorts alphabetically by key, or by filename
// then line when -n or -s is given; -v before the target is the scanner's
// short flag, so the file's first call leads the natural order.
func TestScanSortOrders(t *testing.T) {
	out, _, _ := runCLI("-f", scanFile, "-c", "-n")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 || lines[0] != "sample.plain = <not set>" {
		t.Errorf("-n did not sort by file order; first line %q:\n%s", lines[0], out)
	}
	// Alphabetical: "<" sorts ahead of letters.
	out, _, _ = runCLI("-f", scanFile, "-c")
	lines = strings.Split(strings.TrimRight(out, "\n"), "\n")
	if lines[0] != `<"sample." + suffix> = <not set>` {
		t.Errorf("alphabetical sort first line = %q", lines[0])
	}
}

// cli.Scan RETURN: --source prepends "# <file>, line <n>" above each
// call, and implies the natural sort.
func TestScanSourceAnnotations(t *testing.T) {
	out, _, code := runCLI("-f", scanFile, "-c", "-s")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if !strings.HasPrefix(out, "# calls.go, line 5\nsample.plain = <not set>\n") {
		t.Errorf("source annotation wrong at the head:\n%s", out[:min(len(out), 120)])
	}
	if !strings.Contains(out, "# calls.go, line 8\nsample.withdefault = \"default value\"\n") {
		t.Errorf("source annotation missing mid-file:\n%s", out)
	}
}

// cli.Scan INPUT: a directory target is walked recursively for files
// ending in ".go"; RETURN: its filenames show relative to the target
// directory's parent, and a file that fails to parse is skipped silently.
func TestScanDirectoryWalk(t *testing.T) {
	out, errOut, code := runCLI("-f", "testdata/scan", "-c", "-s")
	if code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	if errOut != "" {
		t.Errorf("stderr = %q, wanted silence for the unparseable file", errOut)
	}
	if !strings.Contains(out, "# scan/calls.go, line 5\n") {
		t.Errorf("directory filenames not relative to the parent:\n%s", out[:min(len(out), 200)])
	}
	if !strings.Contains(out, "# scan/sub/inner.go, line 5\ninner.key = \"inner default\"\n") {
		t.Errorf("recursive walk missed the nested file:\n%s", out)
	}
	if strings.Contains(out, "broken") {
		t.Errorf("unparseable file surfaced:\n%s", out)
	}
}

// cli.Scan RETURN: a file target shows filenames relative to the target's
// containing directory — the basename.
func TestScanFileTargetRelativeName(t *testing.T) {
	out, _, _ := runCLI("-f", scanFile, "-c", "-s")
	if !strings.HasPrefix(out, "# calls.go, line 5\n") {
		t.Errorf("file target annotation not the basename:\n%s", out[:min(len(out), 80)])
	}
}

// cli.Scan INPUT: -m resolves a Go package import path to its source;
// RETURN: filenames show relative to the module directory's parent.
func TestScanModuleTarget(t *testing.T) {
	t.Chdir("testdata/scannermod")
	out, errOut, code := runCLI("-m", "example.com/scannermod/lib", "-c", "-s")
	if code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	if out != "# lib/lib.go, line 5\nmod.key = \"mod default\"\n" {
		t.Errorf("stdout = %q", out)
	}
}

// cli.Scan ERROR: a module that cannot be resolved to source prints
// "Could not load module or package: <module>" to stderr and exits one.
func TestScanModuleNotResolved(t *testing.T) {
	out, errOut, code := runCLI("-m", "example.com/nope/nope")
	if code != 1 {
		t.Fatalf("exit code %d", code)
	}
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
	if errOut != "Could not load module or package: example.com/nope/nope\n" {
		t.Errorf("stderr = %q", errOut)
	}
}

// cli.Scan ERROR: a module that resolves to no readable source prints
// "Could not determine module source: <module>" to stderr and exits one.
func TestScanModuleNoReadableSource(t *testing.T) {
	t.Chdir("testdata/scannermod")
	out, errOut, code := runCLI("-m", "./empty", "-c")
	if code != 1 {
		t.Fatalf("exit code %d", code)
	}
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
	if errOut != "Could not determine module source: ./empty\n" {
		t.Errorf("stderr = %q", errOut)
	}
}

// cli.Scan ERROR: a target that is neither a file nor a directory prints
// "Could not determine file type: <path>" to stderr and exits one.
func TestScanUnknownFileType(t *testing.T) {
	out, errOut, code := runCLI("-f", "/nonexistent/target.go", "-c")
	if code != 1 {
		t.Fatalf("exit code %d", code)
	}
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
	if errOut != "Could not determine file type: /nonexistent/target.go\n" {
		t.Errorf("stderr = %q", errOut)
	}
}

// cli.Scan ERROR: no calls found in any parsed file prints "No toxin
// calls." to stderr and exits one.
func TestScanNoCalls(t *testing.T) {
	empty, err := os.CreateTemp(t.TempDir(), "nocalls-*.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := empty.WriteString("package nocalls\n\nfunc f() {}\n"); err != nil {
		t.Fatal(err)
	}
	empty.Close()

	out, errOut, code := runCLI("-f", empty.Name(), "-c")
	if code != 1 {
		t.Fatalf("exit code %d", code)
	}
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
	if errOut != "No toxin calls.\n" {
		t.Errorf("stderr = %q", errOut)
	}
}

// cli.Scan ERROR: an unknown flag or a combination of the exclusive
// flags prints the parse error and the usage to stderr and exits two.
func TestScanParseErrors(t *testing.T) {
	cases := [][]string{
		{"--bogus", "-f", "x.go"},
		{"-z"},
		{"-f", "a.go", "-m", "mod"},
		{"-a", "-k", "-f", "a.go"},
	}
	for _, args := range cases {
		out, errOut, code := runCLI(args...)
		if code != 2 {
			t.Fatalf("%v: exit code %d, stderr %q", args, code, errOut)
		}
		if out != "" {
			t.Errorf("%v: stdout = %q", args, out)
		}
		if !strings.Contains(errOut, "Usage: toxin [options]") {
			t.Errorf("%v: stderr lacks the scanner usage:\n%s", args, errOut)
		}
	}
	// The mutually exclusive target flags are named in the error.
	_, errOut, _ := runCLI("-f", "a.go", "-m", "mod")
	if !strings.Contains(errOut, "mutually exclusive") {
		t.Errorf("target exclusivity error = %q", errOut)
	}
}

// cli.Scan RETURN: output is colorized as Go syntax by default, and -c
// toggles the setting off.
func TestScanColorizedByDefaultToggledByC(t *testing.T) {
	out, _, code := runCLI("-f", scanFile)
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if !strings.Contains(out, "\x1b[32m\"fallback\"\x1b[0m") {
		t.Errorf("default output not colorized as Go syntax:\n%q", out[:min(len(out), 120)])
	}

	plain, _, _ := runCLI("-f", scanFile, "-c")
	if strings.Contains(plain, "\x1b[") {
		t.Errorf("-c output still carries escapes:\n%q", plain[:min(len(plain), 120)])
	}
}

// cli.Scan RETURN: --load-configs loads the process-wide default
// configuration, prints each found key's current value in its place — a
// falsy current value prints the call's default, or "<not set>" when the
// call carries no default — and appends keys that exist only in the
// configuration.
func TestScanLoadConfigs(t *testing.T) {
	etcdEnv(t)
	conf := newTestConfig(t, "config/")
	for key, value := range map[string]any{
		"live.set":            "live-value",
		"live.falsy":          "",
		"live.falsynodefault": 0,
		"live.extra":          "extra-value",
	} {
		if err := conf.Set(key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}

	out, errOut, code := runCLI("-f", "testdata/live.go", "-c", "-l")
	if code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	want := `live.absent = <not set>
live.extra = "extra-value"
live.falsy = "fallback"
live.falsynodefault = <not set>
live.set = "live-value"
`
	if out != want {
		t.Errorf("stdout =\n%q\nwant\n%q", out, want)
	}
}

// cli.Scan RETURN: with --source and --load-configs, "# Loaded config" is
// prepended above the keys appended from the configuration.
func TestScanLoadConfigsLoadedConfigAnnotation(t *testing.T) {
	etcdEnv(t)
	conf := newTestConfig(t, "config/")
	if err := conf.Set("live.set", "live-value"); err != nil {
		t.Fatal(err)
	}
	if err := conf.Set("live.extra", "extra-value"); err != nil {
		t.Fatal(err)
	}

	out, _, code := runCLI("-f", "testdata/live.go", "-c", "-l", "-s")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	want := `# Loaded config
live.extra = "extra-value"
# live.go, line 5
live.set = "live-value"
# live.go, line 6
live.falsy = "fallback"
# live.go, line 7
live.falsynodefault = <not set>
# live.go, line 8
live.absent = <not set>
`
	if out != want {
		t.Errorf("stdout =\n%q\nwant\n%q", out, want)
	}
}

// cli.Main INPUT: -f or -m without a subcommand runs the scanner — a
// module target works without any file flags and vice versa; the scanner
// usage names both targets. Smoked above; this pins the scanner-flags
// help text presence once, through a parse error.
func TestScanUsageListsTargets(t *testing.T) {
	_, errOut, _ := runCLI("--bogus")
	for _, want := range []string{"-f, --filename F", "-m, --module M", "-v, --view-call", "-n, --natural-sort"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("scanner usage lacks %q:\n%s", want, errOut)
		}
	}
}

// filepath.Join sanity for fixtures: the walk target exists.
func TestScanFixturesExist(t *testing.T) {
	for _, p := range []string{"testdata/scan/calls.go", "testdata/scan/sub/inner.go", "testdata/scan/broken.go", "testdata/live.go", filepath.Join("testdata", "scannermod", "lib", "lib.go")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("fixture %s: %v", p, err)
		}
	}
}
