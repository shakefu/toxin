package cli

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shakefu/toxin"
	"github.com/spf13/pflag"
)

// scanOptions carries the scanner's parsed flags: the target (-f or -m)
// and the display flags.
type scanOptions struct {
	filename    string
	module      string
	viewCall    bool
	loadConfigs bool
	all         bool
	onlyKeys    bool
	naturalSort bool
	source      bool
	color       bool
}

// parseScanFlags parses the scanner flag set — the root behavior's own
// flags, where -v is --view-call and -n is --natural-sort. -c is a toggle:
// output is colorized by default and the flag's presence turns that off,
// the store_const toggle pyconfig used.
func parseScanFlags(tokens []string) (scanOptions, error) {
	fs := pflag.NewFlagSet("toxin", pflag.ContinueOnError)
	// Errors render through this package's parse-error path with the
	// scanner usage, not pflag's own output.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	filename := fs.StringP("filename", "f", "", "parse an individual file or directory")
	module := fs.StringP("module", "m", "", "parse a package or module, recursively looking inside it")
	viewCall := fs.BoolP("view-call", "v", false, "show the actual toxin call made (default: show namespace)")
	loadConfigs := fs.BoolP("load-configs", "l", false, "query the currently set value for each key found")
	all := fs.BoolP("all", "a", false, "show keys which don't have defaults set")
	onlyKeys := fs.BoolP("only-keys", "k", false, "show a list of discovered keys without values")
	naturalSort := fs.BoolP("natural-sort", "n", false, "sort by filename and line (default: alphabetical by key)")
	source := fs.BoolP("source", "s", false, "show source annotations (implies --natural-sort)")
	fs.BoolP("color", "c", true, "toggle output colors (default: on)")
	if err := fs.Parse(tokens); err != nil {
		return scanOptions{}, err
	}

	opts := scanOptions{
		filename:    *filename,
		module:      *module,
		viewCall:    *viewCall,
		loadConfigs: *loadConfigs,
		all:         *all,
		onlyKeys:    *onlyKeys,
		naturalSort: *naturalSort,
		source:      *source,
		color:       !fs.Changed("color"),
	}
	if opts.filename != "" && opts.module != "" {
		return opts, fmt.Errorf("-f/--filename and -m/--module are mutually exclusive")
	}
	if opts.all && opts.onlyKeys {
		return opts, fmt.Errorf("-a/--all and -k/--only-keys are mutually exclusive")
	}
	return opts, nil
}

// moduleError distinguishes the two module resolution failures.
type moduleError struct {
	source bool // true: resolved, but to no readable source
	module string
}

func (e *moduleError) Error() string {
	if e.source {
		return fmt.Sprintf("Could not determine module source: %s", e.module)
	}
	return fmt.Sprintf("Could not load module or package: %s", e.module)
}

// resolveModule resolves a Go package import path to its source directory
// by asking the go tool. `go list -e` reports resolution failures in the
// {{.Error}} field rather than the exit code; a package that resolves but
// holds no Go files — the namespace-package shape, with a directory and no
// source to parse — is the "no readable source" case.
func resolveModule(module string) (string, error) {
	cmd := exec.Command("go", "list", "-e", "-f", "{{.Dir}}\n{{.Error}}", "--", module)
	out, err := cmd.Output()
	if err != nil {
		// The go tool itself failed: no module context, no go binary.
		return "", &moduleError{module: module}
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	dir := strings.TrimSpace(lines[0])
	errText := ""
	if len(lines) > 1 {
		errText = strings.TrimSpace(lines[1])
	}
	// {{.Error}} renders a nil error as the literal "<nil>".
	if errText == "<nil>" {
		errText = ""
	}
	switch {
	case errText != "" && strings.Contains(errText, "no Go files in"):
		return "", &moduleError{source: true, module: module}
	case errText != "":
		return "", &moduleError{module: module}
	case dir == "":
		return "", &moduleError{source: true, module: module}
	}
	return dir, nil
}

// runScan is the scanner: resolve the target, parse the Go source, record
// the toxin calls, and emit them per the display flags.
func runScan(opts scanOptions) error {
	target := opts.filename
	if opts.module != "" {
		dir, err := resolveModule(opts.module)
		if err != nil {
			return fail("%v", err)
		}
		target = dir
	}

	info, err := os.Stat(target)
	if err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
		return fail("Could not determine file type: %s", target)
	}

	var calls []call
	if info.IsDir() {
		// A directory target shows filenames relative to the target
		// directory's parent.
		calls = parseDir(target)
	} else {
		// A file target shows filenames relative to the target's
		// containing directory — the basename.
		calls = parseSource(target, filepath.Base(target))
	}
	if len(calls) == 0 {
		return fail("No toxin calls.")
	}

	if opts.loadConfigs {
		var err error
		calls, err = loadLive(calls)
		if err != nil {
			return err
		}
	}

	emitScan(stdout, calls, opts)
	return nil
}

// parseDir walks dir recursively for files ending in ".go" and parses each
// one, showing filenames relative to dir's parent.
func parseDir(dir string) []call {
	parent := filepath.Dir(dir)
	var calls []call
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable entry is skipped, like a file that fails
			// to parse.
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		display, relErr := filepath.Rel(parent, path)
		if relErr != nil {
			display = path
		}
		calls = append(calls, parseSource(path, filepath.ToSlash(display))...)
		return nil
	})
	return calls
}

// loadLive loads the process-wide default configuration and marks every
// scanned call with its key's current value — a falsy one falls back to
// the call's rendering — then appends the keys that exist only in the
// configuration, with no source annotation of their own.
func loadLive(calls []call) ([]call, error) {
	conf, err := toxin.Default()
	if err != nil {
		return nil, fail("%v", err)
	}
	settings, err := conf.Load()
	if err != nil {
		return nil, fail("%v", err)
	}

	scanned := make(map[string]bool, len(calls))
	for i := range calls {
		c := &calls[i]
		scanned[c.key] = true
		v, err := conf.Get(c.key, nil)
		if err != nil {
			return nil, fail("%v", err)
		}
		if truthy(v) {
			c.live, c.liveSet = renderValue(v), true
		}
	}

	for _, key := range sortedKeys(settings) {
		if scanned[key] {
			continue
		}
		rendered := renderValue(settings[key])
		calls = append(calls, call{
			method:     "set",
			key:        key,
			defaults:   rendered,
			hasDefault: true,
			live:       rendered,
			liveSet:    true,
		})
	}
	return calls, nil
}

// emitScan sorts the calls, applies the only-keys and no-default dedup
// rules, and writes the output: one line per remaining call,
// newline-terminated, each optionally under its source annotation, then
// colorized as Go syntax unless -c turned that off.
func emitScan(w io.Writer, calls []call, opts scanOptions) {
	if opts.naturalSort || opts.source {
		// --source implies the natural sort, so annotations read in
		// source order.
		sort.SliceStable(calls, func(i, j int) bool {
			if calls[i].file != calls[j].file {
				return calls[i].file < calls[j].file
			}
			return calls[i].line < calls[j].line
		})
	} else {
		sort.SliceStable(calls, func(i, j int) bool {
			return calls[i].key < calls[j].key
		})
	}

	var b strings.Builder
	writeLine := func(s string) {
		b.WriteString(s)
		b.WriteByte('\n')
	}

	if opts.onlyKeys {
		seen := make(map[string]bool, len(calls))
		for _, c := range calls {
			if seen[c.key] {
				continue
			}
			seen[c.key] = true
			if opts.source {
				writeLine(c.annotation())
			}
			writeLine(c.key)
		}
	} else {
		// Without --all, a recorded call that carries no default is
		// omitted when another recorded call for the same key carries
		// one.
		withDefault := make(map[string]bool, len(calls))
		for _, c := range calls {
			if c.hasDefault {
				withDefault[c.key] = true
			}
		}
		for _, c := range calls {
			if !opts.all && !c.hasDefault && withDefault[c.key] {
				continue
			}
			if opts.source {
				writeLine(c.annotation())
			}
			writeLine(c.valueLine(opts))
		}
	}

	out := b.String()
	if opts.color {
		out = colorizeGo(out)
	}
	fmt.Fprint(w, out)
}

// sortedKeys returns the map's keys sorted.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
