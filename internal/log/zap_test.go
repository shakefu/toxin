package log

import (
	"bytes"
	"errors"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// 10-etcd3@etcd.Client::RETURN + root@EnvNaming: zap levels map onto the
// TOXIN_LOGLEVEL levels, with Error demoted to warn so the default critical
// level stays silent about failures the caller also receives as errors.
func TestZapLevelMapping(t *testing.T) {
	cases := []struct {
		zap  zapcore.Level
		want Level
	}{
		{zapcore.DebugLevel, Debug},
		{zapcore.InfoLevel, Info},
		{zapcore.WarnLevel, Warn},
		{zapcore.ErrorLevel, Warn},
		{zapcore.DPanicLevel, Critical},
		{zapcore.PanicLevel, Critical},
		{zapcore.FatalLevel, Critical},
	}
	for _, tc := range cases {
		if got := zapLevel(tc.zap); got != tc.want {
			t.Errorf("zapLevel(%v) = %v, want %v", tc.zap, got, tc.want)
		}
	}

	var buf bytes.Buffer
	z := New(&buf, Debug).Zap("")
	z.Debug("d")
	z.Info("i")
	z.Warn("w")
	z.Error("e")
	z.DPanic("p") // development mode is off, so DPanic does not panic
	want := "DEBUG d\nINFO i\nWARN w\nWARN e\nCRITICAL p\n"
	if buf.String() != want {
		t.Errorf("output = %q, want %q", buf.String(), want)
	}
}

// At the default critical level, the etcd client's error-level retry
// diagnostics are silent.
func TestZapCriticalSilencesErrors(t *testing.T) {
	var buf bytes.Buffer
	z := New(&buf, Critical).Zap("etcd-client")
	z.Debug("d")
	z.Info("i")
	z.Warn("w")
	z.Error("retrying of unary invoker failed", zap.Error(errors.New("boom")))
	if buf.Len() != 0 {
		t.Errorf("critical logger wrote %q, want nothing", buf.String())
	}
	if z.Core().Enabled(zapcore.ErrorLevel) {
		t.Error("core enabled for error at critical, want disabled")
	}
}

// SetLevel on the underlying logger takes effect on the zap logger at once.
func TestZapFollowsSetLevel(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, Critical)
	z := l.Zap("n")
	z.Warn("hidden")
	l.SetLevel(Warn)
	z.Warn("shown")
	l.SetLevel(Critical)
	z.Warn("hidden again")
	if got, want := buf.String(), "WARN n: shown\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// Fields format on the same line as the message, in order, with values that
// would be ambiguous quoted; With carries fields onto every later entry and
// Named joins names.
func TestZapFormatsFieldsWithAndNamed(t *testing.T) {
	var buf bytes.Buffer
	z := New(&buf, Debug).Zap("etcd-client")
	z.Warn("retrying",
		zap.String("target", "127.0.0.1:1"),
		zap.Uint("attempt", 2),
		zap.Error(errors.New("connection refused")),
		zap.String("empty", ""),
		zap.Strings("endpoints", []string{"a", "b"}),
	)
	want := `WARN etcd-client: retrying target=127.0.0.1:1 attempt=2 error="connection refused" empty="" endpoints="[a b]"` + "\n"
	if buf.String() != want {
		t.Errorf("output =\n%q\nwant\n%q", buf.String(), want)
	}

	buf.Reset()
	child := z.With(zap.String("member", "m1")).Named("balancer")
	child.Info("picked", zap.Int("n", 1))
	z.Info("parent")
	want = "INFO etcd-client.balancer: picked member=m1 n=1\nINFO etcd-client: parent\n"
	if buf.String() != want {
		t.Errorf("output =\n%q\nwant\n%q", buf.String(), want)
	}

}

// Namespaces scope the fields after them within one entry, nest with ".",
// and match zapcore's own ObjectEncoder nesting; they never leak into a
// sibling logger or a later entry.
func TestZapNamespaces(t *testing.T) {
	var buf bytes.Buffer
	z := New(&buf, Debug).Zap("n")
	scoped := z.With(zap.String("top", "t"), zap.Namespace("a"), zap.String("x", "1"))
	entry := []zap.Field{zap.Namespace("b"), zap.String("y", "2"), zap.Namespace("c"), zap.Int("z", 3)}

	scoped.Info("m", entry...)
	want := "INFO n: m top=t a.x=1 a.b.y=2 a.b.c.z=3\n"
	if buf.String() != want {
		t.Errorf("namespaced output = %q, want %q", buf.String(), want)
	}

	// zapcore's MapObjectEncoder nests the same fields the same way.
	enc := zapcore.NewMapObjectEncoder()
	for _, f := range append([]zap.Field{zap.String("top", "t"), zap.Namespace("a"), zap.String("x", "1")}, entry...) {
		f.AddTo(enc)
	}
	flat := map[string]any{}
	flatten("", enc.Fields, flat)
	for key, v := range map[string]any{"top": "t", "a.x": "1", "a.b.y": "2", "a.b.c.z": int64(3)} {
		if flat[key] != v {
			t.Errorf("zapcore nests %q as %v, want %v (flattened %v)", key, flat[key], v, flat)
		}
	}
	if len(flat) != 4 {
		t.Errorf("zapcore flattened to %v, want exactly the four keys above", flat)
	}

	// The scope ends with the entry: the With namespace stays on its own
	// logger, and neither the entry's namespaces nor the With namespace
	// reach a sibling or a later entry.
	buf.Reset()
	scoped.Info("again", zap.String("w", "4"))
	z.Info("sibling", zap.String("v", "5"))
	z.With(zap.Namespace("other")).Info("other", zap.String("u", "6"))
	want = "INFO n: again top=t a.x=1 a.w=4\nINFO n: sibling v=5\nINFO n: other other.u=6\n"
	if buf.String() != want {
		t.Errorf("output = %q, want %q", buf.String(), want)
	}
}

// flatten joins nested encoder maps into dotted keys.
func flatten(prefix string, m map[string]any, out map[string]any) {
	for k, v := range m {
		if nested, ok := v.(map[string]any); ok {
			flatten(prefix+k+".", nested, out)
			continue
		}
		out[prefix+k] = v
	}
}

// Sync is a no-op that never fails.
func TestZapSyncIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	z := New(&buf, Debug).Zap("n")
	if err := z.Sync(); err != nil {
		t.Errorf("Sync = %v, want nil", err)
	}
	if buf.Len() != 0 {
		t.Errorf("Sync wrote %q, want nothing", buf.String())
	}
}
