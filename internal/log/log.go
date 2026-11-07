// Package log provides toxin's internal leveled logging: the five
// TOXIN_LOGLEVEL levels in order, level parsing with the error text the
// configuration constructor reports, and level filtering that command
// line verbosity drives.
package log

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Level is a logging severity, ordered from most verbose (Silly) to
// least verbose (Critical).
type Level int

// The TOXIN_LOGLEVEL levels, most verbose first.
const (
	Silly Level = iota
	Debug
	Info
	Warn
	Critical
)

// DefaultLevel is the level loggers filter at when none is configured.
const DefaultLevel = Critical

var levelNames = [...]string{"silly", "debug", "info", "warn", "critical"}

// String returns the level's TOXIN_LOGLEVEL name.
func (l Level) String() string {
	if l < Silly || int(l) >= len(levelNames) {
		return fmt.Sprintf("Level(%d)", int(l))
	}
	return levelNames[l]
}

// ParseLevel parses a TOXIN_LOGLEVEL level name, one of silly, debug,
// info, warn or critical. Any other name fails with the error
// "Unknown log level '<level>'".
func ParseLevel(name string) (Level, error) {
	for i, n := range levelNames {
		if name == n {
			return Level(i), nil
		}
	}
	return 0, fmt.Errorf("Unknown log level '%s'", name)
}

// Lower returns the next more verbose level, holding at Silly, so a
// repeated verbosity flag can lower the level one step at a time from
// Critical toward Silly.
func (l Level) Lower() Level {
	if l > Silly {
		return l - 1
	}
	return Silly
}

// Logger writes level-filtered messages to an output writer.
type Logger struct {
	mu    sync.Mutex
	out   io.Writer
	level Level
}

// New returns a logger writing to out that emits messages at level or
// above.
func New(out io.Writer, level Level) *Logger {
	return &Logger{out: out, level: level}
}

// Default returns a logger writing to standard error at DefaultLevel.
func Default() *Logger {
	return New(os.Stderr, DefaultLevel)
}

// Level returns the level the logger filters at.
func (l *Logger) Level() Level {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.level
}

// SetLevel changes the level the logger filters at.
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// Enabled reports whether a message at level would be emitted.
func (l *Logger) Enabled(level Level) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return level >= l.level
}

// Logf writes a formatted message at level, prefixed with the level's
// uppercased name, when the level is enabled.
func (l *Logger) Logf(level Level, format string, args ...any) {
	if !l.Enabled(level) {
		return
	}
	msg := fmt.Sprintf(format, args...)
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.out, "%s %s\n", strings.ToUpper(level.String()), msg)
}

// Sillyf writes a formatted message at Silly.
func (l *Logger) Sillyf(format string, args ...any) {
	l.Logf(Silly, format, args...)
}

// Debugf writes a formatted message at Debug.
func (l *Logger) Debugf(format string, args ...any) {
	l.Logf(Debug, format, args...)
}

// Infof writes a formatted message at Info.
func (l *Logger) Infof(format string, args ...any) {
	l.Logf(Info, format, args...)
}

// Warnf writes a formatted message at Warn.
func (l *Logger) Warnf(format string, args ...any) {
	l.Logf(Warn, format, args...)
}

// Criticalf writes a formatted message at Critical.
func (l *Logger) Criticalf(format string, args ...any) {
	l.Logf(Critical, format, args...)
}
