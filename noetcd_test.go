package toxin

import (
	"errors"
	"io"
	"os"
	"testing"
)

// 20-api@toxin.New, Config.Get, Config.Set, Config.Dump, Config.List,
// Config.Clear, Config.Watch and toxin.Reload: with no member list the
// public API operates in-process without a client, network activity or logs.
func TestNoEtcdPublicAPIUsesInProcessSettings(t *testing.T) {
	clearToxinEnv(t)
	var c *Config
	var err error
	stderr := captureNoEtcdStderr(t, func() {
		c, err = New(WithWatch(true), WithAllowClear(true), WithLogLevel("silly"))
	})
	if err != nil {
		t.Fatalf("New without etcd: %v", err)
	}
	if stderr != "" {
		t.Errorf("construction without etcd wrote stderr %q, want silence", stderr)
	}
	if c.client != nil {
		t.Fatal("New without etcd created a client")
	}
	if got := c.Endpoints(); got == nil || len(got) != 0 {
		t.Errorf("Endpoints without etcd = %v, want a non-nil empty list", got)
	}
	if c.watcher != nil {
		t.Error("New without etcd started a watcher")
	}

	logs := captureLog(c)
	if err := c.Set("App.Timeout", 30, WithTTL(1)); err != nil {
		t.Fatalf("Set without etcd: %v", err)
	}
	got, err := c.GetStrict("app.timeout")
	if err != nil || got != 30 {
		t.Errorf("GetStrict after local Set = (%v, %v), want (30, nil)", got, err)
	}
	got, err = c.Get("APP.TIMEOUT", nil)
	if err != nil || got != 30 {
		t.Errorf("case-insensitive Get after local Set = (%v, %v), want (30, nil)", got, err)
	}
	got, err = c.Get("app.missing", "fallback")
	if err != nil || got != "fallback" {
		t.Errorf("Get missing key = (%v, %v), want (fallback, nil)", got, err)
	}
	if _, err := c.GetStrict("app.absent"); !errors.Is(err, ErrNoSetting) {
		t.Errorf("GetStrict missing key error = %v, want ErrNoSetting", err)
	}

	dump, err := c.Dump()
	if err != nil || dump == nil || len(dump) != 0 {
		t.Errorf("Dump without etcd = (%v, %v), want a non-nil empty map", dump, err)
	}
	list, err := c.List("", DirOnly(false))
	if err != nil || list == nil || len(list) != 0 {
		t.Errorf("List without etcd = (%v, %v), want a non-nil empty list", list, err)
	}

	hooks := 0
	c.AddReloadHook(func() { hooks++ })
	if err := c.Reload(false); err != nil {
		t.Fatalf("Reload(false) without etcd: %v", err)
	}
	got, err = c.Get("app.timeout", nil, CacheOnly(true))
	if err != nil || got != 30 {
		t.Errorf("Reload(false) discarded the local value: (%v, %v)", got, err)
	}
	if err := c.Reload(true); err != nil {
		t.Fatalf("Reload(true) without etcd: %v", err)
	}
	got, err = c.Get("app.timeout", "cleared", CacheOnly(true))
	if err != nil || got != "cleared" {
		t.Errorf("Reload(true) left %v, want a cleared cache", got)
	}
	if hooks != 2 {
		t.Errorf("reload hooks ran %d times, want 2", hooks)
	}

	if err := c.Set("app.key", "value"); err != nil {
		t.Fatalf("Set before Load(merge=false): %v", err)
	}
	if _, err := c.Load(Merge(false)); err != nil {
		t.Fatalf("Load(merge=false) without etcd: %v", err)
	}
	got, err = c.Get("app.key", nil, CacheOnly(true))
	if err != nil || got != nil {
		t.Errorf("Load(merge=false) left %v in the cache, want no value", got)
	}

	if err := c.Set("app.key", "value"); err != nil {
		t.Fatalf("Set before Clear: %v", err)
	}
	if err := c.Clear(); err != nil {
		t.Fatalf("Clear without etcd: %v", err)
	}
	got, err = c.Get("app.key", nil, CacheOnly(true))
	if err != nil || got != nil {
		t.Errorf("Clear without etcd left %v in the cache", got)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close without etcd: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("second Close without etcd: %v", err)
	}
	if logs.Len() != 0 {
		t.Errorf("operations without etcd logged %q, want silence", logs.String())
	}
}

// 20-api@toxin.New, Config.Set and Config.Watch: no-etcd construction skips
// etcd-only TLS/auth handling, watching and cache-disabled Set writes nothing.
func TestNoEtcdCacheDisabledAndWatchAreSilent(t *testing.T) {
	clearToxinEnv(t)
	t.Setenv("TOXIN_AUTH", "incomplete")
	t.Setenv("TOXIN_SSL_CA", "/does/not/exist")
	var c *Config
	var err error
	stderr := captureNoEtcdStderr(t, func() {
		c, err = New(WithCache(false), WithWatch(true), WithLogLevel("silly"))
	})
	if err != nil {
		t.Fatalf("New without etcd and with disabled cache: %v", err)
	}
	if stderr != "" {
		t.Errorf("no-etcd TLS/auth/watch handling wrote stderr %q, want silence", stderr)
	}
	if c.client != nil || c.watcher != nil {
		t.Errorf("no-etcd config has client %p and watcher %p, want both nil", c.client, c.watcher)
	}
	if err := c.Set("app.key", "discarded"); err != nil {
		t.Fatalf("Set with cache disabled: %v", err)
	}
	got, err := c.Get("app.key", "fallback")
	if err != nil || got != "fallback" {
		t.Errorf("Get with cache disabled = (%v, %v), want (fallback, nil)", got, err)
	}
	loaded, err := c.Load(WithConfig(map[string]any{
		"app": map[string]any{"retries": 3},
	}), CacheOnly(false))
	if err != nil {
		t.Fatalf("Load(config, cacheOnly=false) without etcd: %v", err)
	}
	if loaded["app.retries"] != 3 {
		t.Errorf("loaded config = %v, want flattened app.retries=3", loaded)
	}
	c.Watch()
	if c.watcher != nil {
		t.Error("direct Watch without etcd created a watcher")
	}
}

// 20-api@Config.Load and toxin.Reload: the existing file-cache source and
// reload-hook behavior remains intact when no etcd client exists.
func TestNoEtcdLoadReloadAndFileCache(t *testing.T) {
	clearToxinEnv(t)
	c, err := New(WithFileCache(t.TempDir()))
	if err != nil {
		t.Fatalf("New with file cache and no etcd: %v", err)
	}
	hooks := 0
	c.AddReloadHook(func() { hooks++ })
	if err := c.Set("app.key", "saved"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := c.Load(); err != nil {
		t.Fatalf("Load to file cache without etcd: %v", err)
	}
	if _, err := os.Stat(c.fileCache.Filename()); err != nil {
		t.Fatalf("no-etcd Load did not save the file cache: %v", err)
	}

	if err := c.Set("app.key", "newer"); err != nil {
		t.Fatalf("Set newer value: %v", err)
	}
	if err := c.Reload(false); err != nil {
		t.Fatalf("Reload(false): %v", err)
	}
	got, err := c.Get("app.key", nil, CacheOnly(true))
	if err != nil || got != "newer" {
		t.Errorf("file-cache reload replaced a fresh value with %v, want newer", got)
	}
	if err := c.Reload(true); err != nil {
		t.Fatalf("Reload(true): %v", err)
	}
	got, err = c.Get("app.key", nil, CacheOnly(true))
	if err != nil || got != "saved" {
		t.Errorf("clearing reload restored %v, want saved file value", got)
	}
	if hooks != 3 {
		t.Errorf("hooks ran %d times, want 3", hooks)
	}

	loaded, err := c.Load(WithConfig(map[string]any{
		"app": map[string]any{"retries": 3},
	}), CacheOnly(false))
	if err != nil {
		t.Fatalf("Load(config, cacheOnly=false): %v", err)
	}
	if loaded["app.retries"] != 3 {
		t.Errorf("loaded config = %v, want app.retries=3", loaded["app.retries"])
	}
	got, err = c.Get("app.retries", nil, CacheOnly(true))
	if err != nil || got != 3 {
		t.Errorf("Get after in-process write-through load = (%v, %v), want (3, nil)", got, err)
	}
}

// 20-api@Config.Get and Config.Dump: no-etcd inheritance uses parent caches,
// while Dump remains an external-namespace view and does not resolve a parent.
func TestNoEtcdInheritanceUsesParentCachesOnly(t *testing.T) {
	clearToxinEnv(t)
	c, err := New(WithInheritPrefix("parent/"))
	if err != nil {
		t.Fatalf("New with constructor inheritance: %v", err)
	}
	if err := c.parent.Set("app.key", "parent"); err != nil {
		t.Fatalf("Set on parent: %v", err)
	}
	got, err := c.Get("app.key", nil)
	if err != nil || got != "parent" {
		t.Errorf("inherited Get = (%v, %v), want (parent, nil)", got, err)
	}
	if err := c.Set("app.key", "child"); err != nil {
		t.Fatalf("Set on child: %v", err)
	}
	got, err = c.Get("app.key", nil)
	if err != nil || got != "child" {
		t.Errorf("own value did not win over parent: (%v, %v)", got, err)
	}

	keyBased, err := New()
	if err != nil {
		t.Fatalf("New with key-based inheritance: %v", err)
	}
	if err := keyBased.Set("config.inherit", "parent/"); err != nil {
		t.Fatalf("Set inherit key: %v", err)
	}
	parent := keyBased.parentConfig()
	if parent == nil {
		t.Fatal("cache-held inherit key did not resolve a no-etcd parent")
	}
	if err := parent.Set("from.parent", true); err != nil {
		t.Fatalf("Set on key-based parent: %v", err)
	}
	got, err = keyBased.Get("from.parent", nil)
	if err != nil || got != true {
		t.Errorf("key-based inherited Get = (%v, %v), want (true, nil)", got, err)
	}

	lazy, err := New()
	if err != nil {
		t.Fatalf("New for Dump inheritance check: %v", err)
	}
	if err := lazy.Set("config.inherit", "parent/"); err != nil {
		t.Fatalf("Set lazy inherit key: %v", err)
	}
	if _, err := lazy.Dump(); err != nil {
		t.Fatalf("Dump without etcd: %v", err)
	}
	if lazy.parent != nil || !lazy.inherit {
		t.Error("Dump without etcd resolved or disabled inheritance")
	}
}

// The issue #14 cache-only clear guard is intentionally unchanged in this slice.
func TestNoEtcdClearRetainsAllowClearGuard(t *testing.T) {
	clearToxinEnv(t)
	c, err := New()
	if err != nil {
		t.Fatalf("New without etcd: %v", err)
	}
	if err := c.Set("app.key", "kept"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := c.Clear(CacheOnly(true)); err == nil || err.Error() != "clear() is not allowed on this instance" {
		t.Errorf("Clear(cacheOnly) guard error = %v, want the existing allowClear error", err)
	}
	got, err := c.Get("app.key", nil, CacheOnly(true))
	if err != nil || got != "kept" {
		t.Errorf("guarded Clear changed the cache to %v, want kept", got)
	}
}

// captureNoEtcdStderr captures process stderr around construction, where the
// config logger is not yet available to replace with a buffer.
func captureNoEtcdStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating stderr capture pipe: %v", err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()

	fn()
	os.Stderr = old
	if err := w.Close(); err != nil {
		t.Fatalf("closing stderr capture writer: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading captured stderr: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("closing stderr capture reader: %v", err)
	}
	return string(data)
}
