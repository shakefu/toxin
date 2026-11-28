package log

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Zap returns a *zap.Logger that writes through l, so libraries that log
// with zap (the etcd client) obey TOXIN_LOGLEVEL like every other toxin
// message. The logger is named name when name is non-empty.
//
// zap levels map onto toxin levels as Debug to Debug, Info to Info, Warn
// and Error to Warn, and DPanic, Panic and Fatal to Critical. Error maps
// to Warn because the etcd client logs failed retries at Error while the
// failure itself reaches the caller as a Go error, so the default critical
// level stays silent about it. The level check runs on every entry, so
// SetLevel on l takes effect immediately. Sync is a no-op.
func (l *Logger) Zap(name string) *zap.Logger {
	z := zap.New(&zapCore{l: l})
	if name != "" {
		z = z.Named(name)
	}
	return z
}

// zapLevel maps a zap level onto a toxin level.
func zapLevel(lvl zapcore.Level) Level {
	switch {
	case lvl < zapcore.InfoLevel:
		return Debug
	case lvl == zapcore.InfoLevel:
		return Info
	case lvl <= zapcore.ErrorLevel:
		return Warn
	default:
		return Critical
	}
}

// zapCore is a zapcore.Core that formats each entry onto one line and
// hands it to a *Logger.
type zapCore struct {
	l      *Logger
	fields []zapcore.Field
}

func (c *zapCore) Enabled(lvl zapcore.Level) bool {
	return c.l.Enabled(zapLevel(lvl))
}

func (c *zapCore) With(fields []zapcore.Field) zapcore.Core {
	merged := make([]zapcore.Field, 0, len(c.fields)+len(fields))
	merged = append(merged, c.fields...)
	merged = append(merged, fields...)
	return &zapCore{l: c.l, fields: merged}
}

func (c *zapCore) Check(e zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(e.Level) {
		return ce.AddCore(e, c)
	}
	return ce
}

func (c *zapCore) Write(e zapcore.Entry, fields []zapcore.Field) error {
	var b strings.Builder
	if e.LoggerName != "" {
		b.WriteString(e.LoggerName)
		b.WriteString(": ")
	}
	b.WriteString(e.Message)
	all := make([]zapcore.Field, 0, len(c.fields)+len(fields))
	all = append(all, c.fields...)
	all = append(all, fields...)
	writeFields(&b, all)
	c.l.Logf(zapLevel(e.Level), "%s", b.String())
	return nil
}

func (c *zapCore) Sync() error { return nil }

// writeFields appends each field as " key=value" in the order given. As in
// zapcore's encoders, a namespace field scopes every field after it in the
// same entry (the logger's With fields, then the call's), and a nested
// namespace joins its parent's with "."; the scope ends with the entry,
// so sibling loggers and later entries never inherit it.
func writeFields(b *strings.Builder, fields []zapcore.Field) {
	ns := ""
	for _, f := range fields {
		if f.Type == zapcore.NamespaceType {
			ns += f.Key + "."
			continue
		}
		enc := zapcore.NewMapObjectEncoder()
		f.AddTo(enc)
		keys := make([]string, 0, len(enc.Fields))
		for k := range enc.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(b, " %s%s=%s", ns, k, formatValue(enc.Fields[k]))
		}
	}
}

// formatValue renders a field value, quoting strings that are empty or
// hold whitespace, quotes or '=' so the line stays unambiguous.
func formatValue(v any) string {
	s := fmt.Sprintf("%v", v)
	if s == "" || strings.ContainsAny(s, " \t\n\r\"=") {
		return strconv.Quote(s)
	}
	return s
}
