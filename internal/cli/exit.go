package cli

import "fmt"

// exitError carries a process exit code out of a command. Errors that have
// already printed their message return it bare; the dispatcher only reads
// the code. It satisfies venom's ExitCoder so an app.Execute path would map
// it to the same code this dispatcher uses.
type exitError struct {
	code int
}

// Error makes exitError an error. The message is internal; the dispatcher
// never prints it.
func (e exitError) Error() string { return fmt.Sprintf("toxin: exit code %d", e.code) }

// ErrorCode implements venom.ExitCoder.
func (e exitError) ErrorCode() int { return e.code }

// fail prints msg to stderr and returns exitError for exit code one.
func fail(format string, args ...any) error {
	fmt.Fprintf(stderr, format+"\n", args...)
	return exitError{code: 1}
}

// silentExit returns exitError for a code with no message.
func silentExit(code int) error { return exitError{code: code} }
