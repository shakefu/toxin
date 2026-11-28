package toxin

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakefu/toxin/internal/log"
	"github.com/shakefu/toxin/internal/testutil"
)

// setHostileToxinEnv sets every TOXIN_* runtime variable to a value that
// would change, slow down or break construction if it were read: an
// unreachable member, credentials, a missing CA, an uncreatable cache
// directory, and invalid log level and inherit depth values.
func setHostileToxinEnv(t *testing.T) {
	t.Helper()
	clearToxinEnv(t)
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatalf("writing cache blocker: %v", err)
	}
	for k, v := range map[string]string{
		"TOXIN_ETCD":          "192.0.2.1:2379",
		"TOXIN_AUTH":          "u:p",
		"TOXIN_SSL_CA":        filepath.Join(dir, "missing-ca.pem"),
		"TOXIN_SSL_CERT":      filepath.Join(dir, "missing.crt"),
		"TOXIN_SSL_KEY":       filepath.Join(dir, "missing.key"),
		"TOXIN_CACHE":         filepath.Join(blocker, "cache"),
		"TOXIN_LOGLEVEL":      "not-a-level",
		"TOXIN_INHERIT_DEPTH": "not-a-depth",
		"TOXIN_PREFIX":        "env-prefix/",
		"TOXIN_WATCH":         "1",
	} {
		t.Setenv(k, v)
	}
}

// root@toxin INPUT and 20-api@toxin.New::INPUT: with environment reading
// disabled, New reads no TOXIN_* variable and builds an offline Config from
// options and defaults alone, quickly and silently; a constructor parent
// and a lazily resolved parent stay offline too.
func TestWithEnvFalseIgnoresEveryTOXINVariable(t *testing.T) {
	setHostileToxinEnv(t)

	var c, lazy *Config
	var err, lazyErr error
	var elapsed time.Duration
	var got, lazyGot any
	stderr := captureNoEtcdStderr(t, func() {
		start := time.Now()
		c, err = New(WithEnv(false), WithInheritPrefix("parent/"))
		elapsed = time.Since(start)
		if err != nil {
			return
		}
		got, err = c.Get("app.missing", "fallback")

		lazy, lazyErr = New(WithEnv(false))
		if lazyErr != nil {
			return
		}
		if lazyErr = lazy.Set("config.inherit", "lazy-parent/"); lazyErr != nil {
			return
		}
		lazyGot, lazyErr = lazy.Get("app.missing", "lazy-fallback")
	})
	if err != nil {
		t.Fatalf("New(WithEnv(false)) with hostile TOXIN_* environment: %v", err)
	}
	if lazyErr != nil {
		t.Fatalf("lazy inheritance with WithEnv(false): %v", lazyErr)
	}
	// A generous bound: the nil client below is what proves no dial; this
	// only catches a construction that waits on the unreachable member.
	if elapsed > 500*time.Millisecond {
		t.Errorf("construction took %s, want well under the dial timeout with no network", elapsed)
	}
	if stderr != "" {
		t.Errorf("WithEnv(false) wrote stderr %q, want silence", stderr)
	}

	if c.client != nil {
		t.Error("WithEnv(false) created an etcd client from TOXIN_ETCD")
	}
	if eps := c.Endpoints(); eps == nil || len(eps) != 0 {
		t.Errorf("endpoints = %v, want a non-nil empty list", eps)
	}
	if c.prefix != "config/" {
		t.Errorf("prefix = %q, want the default, not TOXIN_PREFIX", c.prefix)
	}
	if c.watcher != nil {
		t.Error("WithEnv(false) started a watcher from TOXIN_WATCH")
	}
	if c.fileCache != nil {
		t.Error("WithEnv(false) configured a file cache from TOXIN_CACHE")
	}
	if c.log.Level() != log.Critical {
		t.Errorf("log level = %v, want the critical default, not TOXIN_LOGLEVEL", c.log.Level())
	}
	if c.inheritDepth != 2 {
		t.Errorf("inherit depth = %d, want the default 2, not TOXIN_INHERIT_DEPTH", c.inheritDepth)
	}
	if got != "fallback" {
		t.Errorf("Get through the offline parent = %v, want the default", got)
	}

	for name, cfg := range map[string]*Config{"constructor": c, "lazy": lazy} {
		p := cfg.parentConfig()
		if p == nil {
			t.Errorf("%s parent not resolved", name)
			continue
		}
		if p.client != nil || p.fileCache != nil || p.watcher != nil {
			t.Errorf("%s parent has client %p, file cache %p, watcher %p; want an offline parent", name, p.client, p.fileCache, p.watcher)
		}
		if p.log != cfg.log {
			t.Errorf("%s parent does not share the child's logger", name)
		}
	}
	if lazyGot != "lazy-fallback" {
		t.Errorf("Get through the lazy offline parent = %v, want the default", lazyGot)
	}
}

// 20-api@toxin.New::INPUT: with environment reading disabled, WithHosts is
// the member list; TOXIN_ETCD, TOXIN_AUTH and TOXIN_SSL_* are ignored.
func TestWithEnvFalseUsesProgrammaticHosts(t *testing.T) {
	setHostileToxinEnv(t)
	t.Setenv("TOXIN_AUTH", "justauser") // would warn on stderr if read
	endpoint := testutil.Etcd(t)

	var c *Config
	var err error
	stderr := captureNoEtcdStderr(t, func() {
		c, err = New(WithEnv(false), WithHosts(endpoint), WithPrefix("env-optout/"))
	})
	if err != nil {
		t.Fatalf("New(WithEnv(false), WithHosts): %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if stderr != "" {
		t.Errorf("construction wrote stderr %q, want no TOXIN_AUTH warning", stderr)
	}
	if eps := c.Endpoints(); len(eps) != 1 || eps[0] != endpoint {
		t.Errorf("endpoints = %v, want [%s] from WithHosts, not TOXIN_ETCD", eps, endpoint)
	}
	if err := c.Set("app.key", "value"); err != nil {
		t.Fatalf("Set against the programmatic host: %v", err)
	}
	got, err := c.Get("app.key", nil, Cached(false))
	if err != nil || got != "value" {
		t.Errorf("Get against the programmatic host = (%v, %v), want (value, nil)", got, err)
	}
}

// 20-api@toxin.New::INPUT: the last environment option supplied wins, and
// environment reading is on by default.
func TestWithEnvLastValueWins(t *testing.T) {
	clearToxinEnv(t)
	t.Setenv("TOXIN_PREFIX", "from-env/")
	t.Setenv("TOXIN_LOGLEVEL", "debug")

	for _, tc := range []struct {
		name    string
		opts    []Option
		envRead bool
	}{
		{"default", nil, true},
		{"false then true", []Option{WithEnv(false), WithEnv(true)}, true},
		{"true then false", []Option{WithEnv(true), WithEnv(false)}, false},
	} {
		c, err := New(tc.opts...)
		if err != nil {
			t.Fatalf("%s: New: %v", tc.name, err)
		}
		wantPrefix, wantLevel := "config/", log.Critical
		if tc.envRead {
			wantPrefix, wantLevel = "from-env/", log.Debug
		}
		if c.prefix != wantPrefix || c.log.Level() != wantLevel {
			t.Errorf("%s: prefix %q level %v, want %q %v", tc.name, c.prefix, c.log.Level(), wantPrefix, wantLevel)
		}
	}
}
