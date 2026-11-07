package log

import (
	"bytes"
	"strings"
	"testing"
)

// 20-api@toxin.New::INPUT + root@EnvNaming: TOXIN_LOGLEVEL is one of
// silly, debug, info, warn, critical, and ParseLevel is the parsing the
// Config layer calls from New.
func TestParseLevelParsesEachNamedLevel(t *testing.T) {
	cases := []struct {
		name  string
		level Level
	}{
		{"silly", Silly},
		{"debug", Debug},
		{"info", Info},
		{"warn", Warn},
		{"critical", Critical},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseLevel(tc.name)
			if err != nil {
				t.Fatalf("ParseLevel(%q) failed: %v", tc.name, err)
			}
			if got != tc.level {
				t.Errorf("ParseLevel(%q) = %v, want %v", tc.name, got, tc.level)
			}
			if got.String() != tc.name {
				t.Errorf("Level.String() = %q, want %q", got.String(), tc.name)
			}
		})
	}
}

// 20-api@toxin.New::ERROR: a level that is not one of silly, debug,
// info, warn or critical is rejected with the error
// "Unknown log level '<level>'", byte for byte.
func TestParseLevelUnknownLevelErrorText(t *testing.T) {
	_, err := ParseLevel("loud")
	if err == nil {
		t.Fatal("ParseLevel(\"loud\") succeeded, want error")
	}
	want := "Unknown log level 'loud'"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// 20-api@toxin.New::INPUT: the log level defaults to critical.
func TestDefaultLevelIsCritical(t *testing.T) {
	if DefaultLevel != Critical {
		t.Errorf("DefaultLevel = %v, want Critical", DefaultLevel)
	}
}

// 30-cli@cli.Main::INPUT: each repeated verbosity step lowers the log
// level one step from Critical toward Silly and holds it at Silly.
func TestLowerStepsOneLevelFromCriticalTowardSillyAndHolds(t *testing.T) {
	cases := []struct {
		from, want Level
	}{
		{Critical, Warn},
		{Warn, Info},
		{Info, Debug},
		{Debug, Silly},
		{Silly, Silly},
	}
	for _, tc := range cases {
		if got := tc.from.Lower(); got != tc.want {
			t.Errorf("%v.Lower() = %v, want %v", tc.from, got, tc.want)
		}
	}
}

// Level filtering: a logger emits a message only when the message's
// level is at least the logger's level; at the default (critical) only
// critical messages pass.
func TestLoggerFiltersAtConfiguredLevel(t *testing.T) {
	cases := []struct {
		logger Level
	}{
		{Critical},
		{Warn},
		{Info},
		{Debug},
		{Silly},
	}
	all := []Level{Silly, Debug, Info, Warn, Critical}
	for _, tc := range cases {
		t.Run(tc.logger.String(), func(t *testing.T) {
			var buf bytes.Buffer
			logger := New(&buf, tc.logger)
			for _, msg := range all {
				logger.Logf(msg, "message at %s", msg.String())
			}
			for _, msg := range all {
				contains := strings.Contains(buf.String(), "message at "+msg.String())
				if want := msg >= tc.logger; contains != want {
					t.Errorf("logger at %s: message at %s emitted = %v, want %v",
						tc.logger.String(), msg.String(), contains, want)
				}
			}
		})
	}
}

// The default level filters at critical: only critical messages are
// emitted.
func TestLoggerAtDefaultLevelEmitsOnlyCritical(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, DefaultLevel)
	logger.Warnf("a warning")
	logger.Criticalf("a critical message")
	if strings.Contains(buf.String(), "a warning") {
		t.Errorf("warn message emitted at default level: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "a critical message") {
		t.Errorf("critical message not emitted at default level: %q", buf.String())
	}
}
