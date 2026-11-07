package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/shakefu/toxin"
)

// The six etcd management subcommands. Venom turns each annotated function
// into a cobra command — the name from the function name, the one-line help
// from @cmd, the positional operands from @arg — and this package dispatches
// into the built tree. The commandSpec table mirrors the same contract for
// usage rendering and positional validation.

// positionalSpec describes one positional operand of a command.
type positionalSpec struct {
	name     string
	help     string
	required bool
}

// commandSpec describes one subcommand: its name, one-line help and
// positional operands.
type commandSpec struct {
	name string
	help string
	args []positionalSpec
}

// commandSpecs lists every subcommand, alphabetically, exactly as the usage
// shows them. The help strings are jetconfig's, carried over.
var commandSpecs = []commandSpec{
	{name: "clear", help: "Clear the current etcd configuration", args: []positionalSpec{
		{name: "prefix", help: "Etcd namespace", required: true},
		{name: "extra", help: "Don't forget to include a / between env and key", required: false},
	}},
	{name: "dump", help: "Dump the current etcd configuration", args: []positionalSpec{
		{name: "prefix", help: "Etcd namespace (default: config/)", required: false},
	}},
	{name: "get", help: "Get a configuration key", args: []positionalSpec{
		{name: "prefix", help: "Etcd namespace", required: true},
		{name: "key", help: "Configuration key", required: true},
	}},
	{name: "list", help: "List all available etcd configurations", args: []positionalSpec{
		{name: "prefix", help: "Etcd namespace (default: /)", required: false},
	}},
	{name: "load", help: "Load a configuration from a file", args: []positionalSpec{
		{name: "prefix", help: "Etcd namespace", required: true},
		{name: "file", help: "Filename to load", required: true},
	}},
	{name: "set", help: "Set a configuration key to a value", args: []positionalSpec{
		{name: "prefix", help: "Etcd namespace", required: true},
		{name: "key", help: "Configuration key", required: true},
		{name: "value", help: "Configuration value", required: true},
	}},
}

// commandByName finds a commandSpec by name.
func commandByName(name string) (commandSpec, bool) {
	for _, spec := range commandSpecs {
		if spec.name == name {
			return spec, true
		}
	}
	return commandSpec{}, false
}

// newConfig constructs a Config on prefix under the resolved global
// options: inheritance off with -n, the log level stepped down per -v
// repeat. clear additionally enables clearing.
func newConfig(prefix string, allowClear bool) (*toxin.Config, error) {
	return toxin.New(
		toxin.WithPrefix(prefix),
		toxin.WithInherit(!runGlobals.noInherit),
		toxin.WithLogLevel(runGlobals.level()),
		toxin.WithAllowClear(allowClear),
	)
}

// printJSON prints v as two-space indented JSON, the shape every subcommand
// reports through. HTML escaping is off so values read back the way
// JSON.stringify wrote them in jetconfig.
func printJSON(v any) error {
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// get prints the value returned for the key. An unset key is a value
// outcome, not an error: it exits one without printing anything. GetStrict
// answers a stored JSON null as a found value — nil — so it prints as
// null rather than reading as the unset case; its 'No setting "<key>"'
// error is the unset marker, and every other failure reaches the caller.
//
// @cmd Get a configuration key
func get(ctx context.Context,
	prefix string, // @arg @required @desc "Etcd namespace"
	key string, // @arg @required @desc "Configuration key"
) error {
	conf, err := newConfig(prefix, false)
	if err != nil {
		return err
	}
	v, err := conf.GetStrict(key)
	if err != nil {
		if strings.HasPrefix(err.Error(), `No setting "`) {
			return silentExit(1)
		}
		return err
	}
	return printJSON(v)
}

// set stores the value under the key, printing nothing on success. The
// value arrives as a string from the command line and Config.Set stores
// strings verbatim.
//
// @cmd Set a configuration key to a value
func set(ctx context.Context,
	prefix string, // @arg @required @desc "Etcd namespace"
	key string, // @arg @required @desc "Configuration key"
	value string, // @arg @required @desc "Configuration value"
) error {
	conf, err := newConfig(prefix, false)
	if err != nil {
		return err
	}
	return conf.Set(key, value)
}

// dump prints the configuration dump for the prefix as two-space indented
// JSON. A namespace with nothing stored fails the dump with the not-found
// error, which prints to stderr and exits one.
//
// @cmd Dump the current etcd configuration
func dump(ctx context.Context,
	prefix string, // @arg @default config/ @desc "Etcd namespace (default: config/)"
) error {
	conf, err := newConfig(prefix, false)
	if err != nil {
		return err
	}
	m, err := conf.Dump()
	if err != nil {
		return fail("%v", err)
	}
	return printJSON(m)
}

// list prints the immediate children of the prefix — leaves and
// directories alike, dirOnly false — as two-space indented JSON.
//
// @cmd List all available etcd configurations
func list(ctx context.Context,
	prefix string, // @arg @default / @desc "Etcd namespace (default: /)"
) error {
	conf, err := newConfig(prefix, false)
	if err != nil {
		return err
	}
	children, err := conf.List("", toxin.DirOnly(false))
	if err != nil {
		return err
	}
	return printJSON(children)
}

// clear clears the prefix namespace. A second positional operand means the
// caller put a key where the prefix belongs; that reports the jetconfig
// joke as JSON and clears nothing.
//
// @cmd Clear the current etcd configuration
func clear(ctx context.Context,
	prefix string, // @arg @required @desc "Etcd namespace"
	extra string, // @arg @desc "Don't forget to include a / between env and key"
) error {
	conf, err := newConfig(prefix, true)
	if err != nil {
		return err
	}
	if len(commandArgs) >= 2 {
		return printJSON(map[string]any{"error": "You forgot a slash didn't you?"})
	}
	return conf.Clear()
}

// load reads the file as JSON and loads it into the prefix namespace,
// writing it through to etcd.
//
// @cmd Load a configuration from a file
func load(ctx context.Context,
	prefix string, // @arg @required @desc "Etcd namespace"
	file string, // @arg @required @desc "Filename to load"
) error {
	data, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fail("No such file: %s", file)
		}
		return err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fail("%v", err)
	}
	conf, err := newConfig(prefix, false)
	if err != nil {
		return err
	}
	_, err = conf.Load(toxin.WithConfig(cfg), toxin.CacheOnly(false))
	return err
}
