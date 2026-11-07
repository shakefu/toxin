package cli

import (
	"github.com/shakefu/venom"
	"github.com/spf13/cobra"
)

// The generator needs the package's absolute path: venom's findModulePath
// derives the runtime-qualified function names from the module root, and a
// relative directory defeats the rel-path step for non-main packages.
//go:generate venom generate $PWD

// newApp builds the venom application. The version comes from this
// package's Version; the config file search points at a directory that
// never exists, because the toxin CLI takes no flags the file could set
// and a stray .toxin.yaml on a user's disk must not break dispatch.
func newApp() *venom.App {
	return venom.New(
		venom.WithName("toxin"),
		venom.WithVersion(Version),
		venom.WithConfigPaths("/nonexistent/toxin-cli"),
	)
}

// buildTree builds the venom command tree for the six subcommands and
// shapes the root for this contract: the scanner is the root behavior, and
// cobra's own usage, error and argument handling is silenced so the
// dispatcher's texts and exit codes are the only ones a user sees.
// Positional validation already happened in the dispatcher, so every
// command accepts any operand count and ignores extras, as jetconfig's
// position-indexed options did.
func buildTree() (*cobra.Command, error) {
	app := newApp()
	root, _, err := app.Build(get, set, dump, list, clear, load)
	if err != nil {
		return nil, err
	}
	root.SilenceUsage = true
	root.SilenceErrors = true
	root.Args = cobra.ArbitraryArgs
	root.CompletionOptions.DisableDefaultCmd = true
	// A hidden no-op stands in for cobra's automatic help command: this
	// dispatcher owns help, so "toxin help" is no such command.
	root.SetHelpCommand(&cobra.Command{Use: "no-help", Hidden: true})
	for _, cmd := range root.Commands() {
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		cmd.Args = cobra.ArbitraryArgs
	}
	return root, nil
}
