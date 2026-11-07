package cli

import (
	"fmt"
	"strings"
)

// The usage renderers. The layouts reproduce the nomnom layout jetconfig
// printed, toxin-ified; the spec pins only the quoted "Usage: toxin
// <command> [options]" line, the command names with a one-line help, and
// the presence of the global options.

// globalOption is one global option of the usage: a label like
// "-n, --no-inherit" and its one-line help.
type globalOption struct {
	label string
	help  string
}

// globalOptions lists the global options of the command mode.
var globalOptions = []globalOption{
	{"-n, --no-inherit", "Prevent configuration inheritance"},
	{"-v, --verbose", "Increase verbosity, can be used multiple times"},
	{"--version", "Print version and exit"},
}

// pad pads s with spaces to width bytes.
func pad(s string, width int) string {
	return fmt.Sprintf("%-*s", width, s)
}

// optionsUsage appends the global options block to lines.
func optionsUsage(lines []string) []string {
	width := 0
	for _, opt := range globalOptions {
		if len(opt.label) > width {
			width = len(opt.label)
		}
	}
	width += 3
	lines = append(lines, "Options:")
	for _, opt := range globalOptions {
		lines = append(lines, "   "+pad(opt.label, width)+opt.help)
	}
	return lines
}

// generalUsage renders the general usage: the usage line, every command
// with a one-line help, and the global options.
func generalUsage() string {
	width := 0
	for _, spec := range commandSpecs {
		if len(spec.name) > width {
			width = len(spec.name)
		}
	}
	width += 5

	lines := []string{""}
	lines = append(lines, "Usage: toxin <command> [options]")
	lines = append(lines, "")
	lines = append(lines, pad("command", width+2))
	for _, spec := range commandSpecs {
		lines = append(lines, "  "+pad(spec.name, width)+spec.help)
	}
	lines = append(lines, "")
	lines = optionsUsage(lines)
	lines = append(lines, "")
	return strings.Join(lines, "\n") + "\n"
}

// commandUsage renders the usage of one command: the usage line with its
// positional operands, each operand with its help, the global options, and
// the command's one-line help.
func commandUsage(spec commandSpec) string {
	width := 0
	operands := make([]string, 0, len(spec.args))
	for _, a := range spec.args {
		if len(a.name) > width {
			width = len(a.name)
		}
		if a.required {
			operands = append(operands, "<"+a.name+">")
		} else {
			operands = append(operands, "["+a.name+"]")
		}
	}
	width += 5

	lines := []string{""}
	lines = append(lines, "Usage: toxin "+spec.name+" "+strings.Join(operands, " ")+" [options]")
	lines = append(lines, "")
	for _, a := range spec.args {
		lines = append(lines, pad(a.name, width)+a.help)
	}
	lines = append(lines, "")
	lines = optionsUsage(lines)
	lines = append(lines, "")
	lines = append(lines, spec.help)
	return strings.Join(lines, "\n") + "\n"
}

// scanFlag is one scanner flag: the label with its value placeholder, and
// its one-line help (pyconfig's, adapted).
type scanFlag struct {
	label string
	help  string
}

// scanFlags lists the scanner flags of the scanner usage.
var scanFlags = []scanFlag{
	{"-f, --filename F", "parse an individual file or directory"},
	{"-m, --module M", "parse a package or module, recursively looking inside it"},
	{"-v, --view-call", "show the actual toxin call made (default: show namespace)"},
	{"-l, --load-configs", "query the currently set value for each key found"},
	{"-a, --all", "show keys which don't have defaults set"},
	{"-k, --only-keys", "show a list of discovered keys without values"},
	{"-n, --natural-sort", "sort by filename and line (default: alphabetical by key)"},
	{"-s, --source", "show source annotations (implies --natural-sort)"},
	{"-c, --color", "toggle output colors (default: on)"},
}

// scannerUsage renders the usage of the scanner, the root behavior with no
// subcommand: the target and display flags.
func scannerUsage() string {
	width := 0
	for _, f := range scanFlags {
		if len(f.label) > width {
			width = len(f.label)
		}
	}
	width += 3

	lines := []string{""}
	lines = append(lines, "Usage: toxin [options]")
	lines = append(lines, "")
	for _, f := range scanFlags {
		lines = append(lines, "  "+pad(f.label, width)+f.help)
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n") + "\n"
}
