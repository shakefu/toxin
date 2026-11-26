package toxin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/shakefu/toxin/internal/log"
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

	var logs bytes.Buffer
	c.log = log.New(&logs, log.Silly)
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
// etcd-only TLS handling and watching; cache-disabled Set writes nothing.
func TestNoEtcdCacheDisabledAndWatchAreSilent(t *testing.T) {
	clearToxinEnv(t)
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
		t.Errorf("no-etcd TLS/watch handling wrote stderr %q, want silence", stderr)
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

// 20-api@Config.Get: a bypassing read does not see or overwrite the local
// setting; a later ordinary read still returns the value written by Set.
func TestNoEtcdCachedFalseBypassPreservesLocalSetting(t *testing.T) {
	clearToxinEnv(t)
	c, err := New()
	if err != nil {
		t.Fatalf("New without etcd: %v", err)
	}
	if err := c.Set("http.port", 9090); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := c.Get("http.port", 8080, Cached(false))
	if err != nil || got != 8080 {
		t.Errorf("Cached(false) Get = (%v, %v), want the bypass default 8080", got, err)
	}
	got, err = c.GetStrict("http.port")
	if err != nil || got != 9090 {
		t.Errorf("bypass destroyed the local setting: (%v, %v), want 9090", got, err)
	}
	if _, err := c.GetStrict("http.port", Cached(false)); !errors.Is(err, ErrNoSetting) {
		t.Errorf("Cached(false) GetStrict error = %v, want ErrNoSetting", err)
	}
	got, err = c.GetStrict("http.port")
	if err != nil || got != 9090 {
		t.Errorf("strict bypass destroyed the local setting: (%v, %v), want 9090", got, err)
	}
}

// 20-api@Config.Set::ERROR: local writes reject values that cannot cross a
// JSON storage boundary instead of failing on a later save.
func TestNoEtcdSetRejectsNonJSONValue(t *testing.T) {
	clearToxinEnv(t)
	c, err := New()
	if err != nil {
		t.Fatalf("New without etcd: %v", err)
	}
	if err := c.Set("bad.value", make(chan int)); err == nil {
		t.Fatal("Set accepted a channel, want a JSON encoding error")
	}
	if _, err := c.GetStrict("bad.value"); !errors.Is(err, ErrNoSetting) {
		t.Errorf("rejected Set left a value behind: %v", err)
	}
}

// 20-api@toxin.FileCache: a no-etcd save and clearing reload preserves every
// JSON integer digit instead of round-tripping through float64.
func TestNoEtcdFileCacheReloadPreservesLargeInteger(t *testing.T) {
	clearToxinEnv(t)
	c, err := New(WithFileCache(t.TempDir()))
	if err != nil {
		t.Fatalf("New with file cache: %v", err)
	}
	const exact = int64(9007199254740993)
	if err := c.Set("large.integer", exact); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := c.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := c.Reload(true); err != nil {
		t.Fatalf("Reload(true): %v", err)
	}
	got, err := c.GetStrict("large.integer")
	if err != nil {
		t.Fatalf("GetStrict after reload: %v", err)
	}
	if want := json.Number("9007199254740993"); got != want {
		t.Errorf("reloaded integer = %v (%T), want %s", got, got, want)
	}
}

// 20-api@Config.Get and Inheritance: public APIs seed and read parent file
// caches for both constructor-based and key-based inheritance.
func TestNoEtcdInheritanceLoadsParentFileCache(t *testing.T) {
	clearToxinEnv(t)
	dir := t.TempDir()
	parent, err := New(WithPrefix("parent/"), WithFileCache(dir))
	if err != nil {
		t.Fatalf("New parent: %v", err)
	}
	if err := parent.Set("app.key", "parent"); err != nil {
		t.Fatalf("Set parent app.key: %v", err)
	}
	if err := parent.Set("from.parent", true); err != nil {
		t.Fatalf("Set parent flag: %v", err)
	}
	if _, err := parent.Load(); err != nil {
		t.Fatalf("Save parent file cache: %v", err)
	}

	child, err := New(WithPrefix("child/"), WithFileCache(dir), WithInheritPrefix("parent/"))
	if err != nil {
		t.Fatalf("New child: %v", err)
	}
	if _, err := child.Load(); err != nil {
		t.Fatalf("Load child: %v", err)
	}
	got, err := child.Get("app.key", 8080)
	if err != nil || got != "parent" {
		t.Errorf("constructor-based inherited Get = (%v, %v), want parent", got, err)
	}
	if err := child.Set("app.key", "child"); err != nil {
		t.Fatalf("Set child value: %v", err)
	}
	got, err = child.Get("app.key", nil)
	if err != nil || got != "child" {
		t.Errorf("child value did not win: (%v, %v)", got, err)
	}

	keyBased, err := New(WithPrefix("key-child/"), WithFileCache(dir))
	if err != nil {
		t.Fatalf("New key-based child: %v", err)
	}
	if err := keyBased.Set("config.inherit", "parent/"); err != nil {
		t.Fatalf("Set inherit key: %v", err)
	}
	got, err = keyBased.Get("from.parent", nil)
	if err != nil || got != true {
		t.Errorf("key-based inherited Get = (%v, %v), want true", got, err)
	}

	missing, err := New(WithPrefix("missing-child/"), WithFileCache(dir), WithInheritPrefix("missing/"))
	if err != nil {
		t.Fatalf("New child with missing parent file: %v", err)
	}
	got, err = missing.Get("absent", "fallback")
	if err != nil || got != "fallback" {
		t.Errorf("missing parent file Get = (%v, %v), want fallback", got, err)
	}
}

// 20-api@Config.Get::ERROR: an unreadable parent file is returned without a
// log message rather than being silently treated as an empty namespace.
func TestNoEtcdInheritanceSurfacesParentFileCacheError(t *testing.T) {
	clearToxinEnv(t)
	dir := t.TempDir()
	fc := NewFileCache("parent/", dir)
	if err := os.WriteFile(fc.Filename(), []byte("not json"), 0o644); err != nil {
		t.Fatalf("writing invalid parent cache: %v", err)
	}
	child, err := New(WithPrefix("child/"), WithFileCache(dir), WithInheritPrefix("parent/"))
	if err != nil {
		t.Fatalf("New child: %v", err)
	}
	var logs bytes.Buffer
	child.log = log.New(&logs, log.Silly)
	if _, err := child.Get("app.key", "fallback"); err == nil {
		t.Fatal("Get with corrupt parent file succeeded, want the file-cache error")
	}
	if logs.Len() != 0 {
		t.Errorf("parent file-cache failure logged %q, want silence", logs.String())
	}
}

// 20-api@Config.Dump: the empty external view does not resolve inheritance.
func TestNoEtcdDumpDoesNotResolveInheritance(t *testing.T) {
	clearToxinEnv(t)
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Set("config.inherit", "parent/"); err != nil {
		t.Fatalf("Set inherit key: %v", err)
	}
	if _, err := c.Dump(); err != nil {
		t.Fatalf("Dump without etcd: %v", err)
	}
	if c.parent != nil || !c.inherit {
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

// captureNoEtcdStderr captures process stderr around construction, draining
// concurrently so a noisy regression cannot fill the pipe and deadlock.
func captureNoEtcdStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating stderr capture pipe: %v", err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()

	var data []byte
	var readErr error
	done := make(chan struct{})
	go func() {
		data, readErr = io.ReadAll(r)
		close(done)
	}()
	fn()
	os.Stderr = old
	if err := w.Close(); err != nil {
		t.Fatalf("closing stderr capture writer: %v", err)
	}
	<-done
	if readErr != nil {
		t.Fatalf("reading captured stderr: %v", readErr)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("closing stderr capture reader: %v", err)
	}
	return string(data)
}
