package toxin

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/shakefu/toxin/internal/testutil"
)

// 20-api@Config.Load::INPUT: a nested map flattens into dot-notation keys
// by recursing through nested maps only; a slice stays a single value, a
// top-level key that already contains a dot is kept as-is, and a nested
// empty map is stored as an empty map value.
func TestLoadFlattensNestedMapIntoDotNotationKeys(t *testing.T) {
	c := newTestConfig(t, WithPrefix("load-flat/"))

	nested := map[string]any{
		"app": map[string]any{
			"db":    map[string]any{"host": "db.example.com", "port": 5432},
			"flag":  true,
			"hosts": []any{"a", "b"},
			"empty": map[string]any{},
		},
		"already.dotted": map[string]any{"kept": "whole"},
		"top":            "level",
	}
	cache, err := c.Load(WithConfig(nested))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := map[string]any{
		"app.db.host":    "db.example.com",
		"app.db.port":    5432,
		"app.flag":       true,
		"app.hosts":      []any{"a", "b"},
		"app.empty":      map[string]any{},
		"already.dotted": map[string]any{"kept": "whole"},
		"top":            "level",
	}
	for key, wantVal := range want {
		got, ok := cache[key]
		if !ok {
			t.Fatalf("cache lacks flattened key %q: %v", key, cache)
			continue
		}
		if !jsonEqual(t, got, wantVal) {
			t.Errorf("cache[%q] = %#v, want %#v", key, got, wantVal)
		}
	}
	if len(cache) != len(want) {
		t.Errorf("cache has %d keys, want %d: %v", len(cache), len(want), cache)
	}
}

// 20-api@Config.Load::RETURN: with no config given and the namespace
// holding nothing, Load yields an empty cache without error.
func TestLoadEmptyNamespaceYieldsEmptyCacheWithoutError(t *testing.T) {
	c := newTestConfig(t, WithPrefix("load-empty/"))

	cache, err := c.Load()
	if err != nil {
		t.Fatalf("Load of an empty namespace errored: %v", err)
	}
	if len(cache) != 0 {
		t.Errorf("Load of an empty namespace = %v, want an empty cache", cache)
	}
}

// 20-api@Config.Load::SIDE-EFFECT: with no config given and no cache file
// read, the etcd configuration under the prefix is loaded into the cache;
// the default cacheOnly leaves etcd untouched.
func TestLoadWithoutConfigLoadsEtcdIntoCache(t *testing.T) {
	raw := newRawClient(t)
	rawPut(t, raw, "/load-etcd/app.key", `"stored"`)

	c := newTestConfig(t, WithPrefix("load-etcd/"))
	cache, err := c.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cache["app.key"] != "stored" {
		t.Errorf("cache[app.key] = %v, want %q", cache["app.key"], "stored")
	}

	// The cached value answers without contacting etcd after the raw
	// delete.
	if _, _, err := raw.Delete(t.Context(), "/load-etcd/app.key", false); err != nil {
		t.Fatalf("raw delete: %v", err)
	}
	got, err := c.Get("app.key", nil, CacheOnly(true))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "stored" {
		t.Errorf("cacheOnly Get after Load = %v, want the loaded %q", got, "stored")
	}
}

// 20-api@Config.Load::SIDE-EFFECT: with no config given and a file cache
// configured, the file cache is read in place of etcd, populating the
// cache without contacting etcd, clearing it or saving the file.
func TestLoadReadsFileCacheInPlaceOfEtcd(t *testing.T) {
	dir := t.TempDir()
	clearToxinEnv(t)
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))
	t.Setenv("TOXIN_CACHE", dir)

	seed := NewFileCache("load-fc/", dir)
	if err := seed.Save(map[string]any{"file.key": "from-file"}); err != nil {
		t.Fatalf("seeding the cache file: %v", err)
	}

	c, err := New(WithPrefix("load-fc/"))
	if err != nil {
		t.Fatalf("toxin.New: %v", err)
	}
	cache, err := c.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cache["file.key"] != "from-file" {
		t.Fatalf("cache[file.key] = %v, want %q from the cache file", cache["file.key"], "from-file")
	}

	// The value answers without contacting etcd.
	got, err := c.Get("file.key", nil, CacheOnly(true))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "from-file" {
		t.Errorf("cacheOnly Get after file-cache Load = %v, want %q", got, "from-file")
	}

	// The cache file was not rewritten by the load.
	data, err := os.ReadFile(seed.Filename())
	if err != nil {
		t.Fatalf("reading the cache file: %v", err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("cache file is not JSON: %v", err)
	}
	if len(onDisk) != 1 || onDisk["file.key"] != "from-file" {
		t.Errorf("cache file changed by a file-cache load: %v", onDisk)
	}
}

// 20-api@Config.Load::SIDE-EFFECT: fileCacheOnly never contacts etcd.
func TestLoadFileCacheOnlyNeverContactsEtcd(t *testing.T) {
	raw := newRawClient(t)
	rawPut(t, raw, "/load-fconly/app.key", `"stored"`)

	dir := t.TempDir()
	clearToxinEnv(t)
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))
	t.Setenv("TOXIN_CACHE", dir)
	c, err := New(WithPrefix("load-fconly/"))
	if err != nil {
		t.Fatalf("toxin.New: %v", err)
	}

	cache, err := c.Load(FileCacheOnly(true))
	if err != nil {
		t.Fatalf("Load(fileCacheOnly): %v", err)
	}
	if len(cache) != 0 {
		t.Errorf("Load(fileCacheOnly) = %v, want the untouched cache", cache)
	}
	got, err := c.Get("app.key", nil, CacheOnly(true))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("cacheOnly Get after fileCacheOnly Load = %v, want no etcd data", got)
	}
	if _, ok := rawGet(t, raw, "/load-fconly/app.key"); !ok {
		t.Error("the etcd namespace changed under a fileCacheOnly load")
	}
}

// 20-api@Config.Load::SIDE-EFFECT: with merge false the cache is cleared
// before loading; with merge true (the default) loaded keys merge over
// the cache.
func TestLoadMergeFalseClearsCacheFirst(t *testing.T) {
	c := newTestConfig(t, WithPrefix("load-merge/"))

	if _, err := c.Get("old.default", "stale"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	cache, err := c.Load(WithConfig(map[string]any{"new": "key"}), Merge(false))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := cache["old.default"]; ok {
		t.Errorf("merge=false kept the old cache entry: %v", cache)
	}
	if cache["new"] != "key" {
		t.Errorf("cache[new] = %v, want %q", cache["new"], "key")
	}
}

// 20-api@Config.Load::SIDE-EFFECT: with cacheOnly false every loaded key
// is written to etcd, merged over what exists rather than replacing it.
func TestLoadCacheOnlyFalseWritesThroughToEtcdMerged(t *testing.T) {
	raw := newRawClient(t)
	rawPut(t, raw, "/load-write/kept.key", `"kept"`)

	c := newTestConfig(t, WithPrefix("load-write/"))
	_, err := c.Load(
		WithConfig(map[string]any{"added.key": "added"}),
		CacheOnly(false),
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got, ok := rawGet(t, raw, "/load-write/added.key"); !ok || got != "added" {
		t.Errorf("raw /load-write/added.key = %q (%v), want the written %q", got, ok, "added")
	}
	if got, ok := rawGet(t, raw, "/load-write/kept.key"); !ok || got != `"kept"` {
		t.Errorf("raw /load-write/kept.key = %q (%v), want the pre-existing entry left in place", got, ok)
	}
}

// 20-api@Config.Load::SIDE-EFFECT: with no cache file read and a file
// cache configured, the cache file is saved after loading.
func TestLoadSavesCacheFileAfterLoading(t *testing.T) {
	dir := t.TempDir()
	clearToxinEnv(t)
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))
	t.Setenv("TOXIN_CACHE", dir)
	c, err := New(WithPrefix("load-save/"))
	if err != nil {
		t.Fatalf("toxin.New: %v", err)
	}
	raw := newRawClient(t)
	rawPut(t, raw, "/load-save/app.key", `"stored"`)

	if _, err := c.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	data, err := os.ReadFile(NewFileCache("load-save/", dir).Filename())
	if err != nil {
		t.Fatalf("no cache file saved after loading: %v", err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("saved cache file is not JSON: %v", err)
	}
	if onDisk["app.key"] != "stored" {
		t.Errorf("saved cache = %v, want the loaded key", onDisk)
	}
}

// 20-api@Config.Load::SIDE-EFFECT: a supplied config skips the file cache
// read — the file's entries do not leak into the load.
func TestLoadGivenConfigSkipsFileCacheRead(t *testing.T) {
	dir := t.TempDir()
	if err := NewFileCache("load-skip/", dir).Save(map[string]any{"file.key": "from-file"}); err != nil {
		t.Fatalf("seeding the cache file: %v", err)
	}
	clearToxinEnv(t)
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))
	t.Setenv("TOXIN_CACHE", dir)
	c, err := New(WithPrefix("load-skip/"))
	if err != nil {
		t.Fatalf("toxin.New: %v", err)
	}

	cache, err := c.Load(WithConfig(map[string]any{"given": "value"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := cache["file.key"]; ok {
		t.Errorf("a supplied config still read the file cache: %v", cache)
	}
	if cache["given"] != "value" {
		t.Errorf("cache[given] = %v, want %q", cache["given"], "value")
	}
}

// 20-api@toxin.ReloadHook::INPUT: hooks register for every subsequent
// load, run bare in registration order, and there is no way to
// unregister one — later loads still run an early hook.
func TestLoadRunsReloadHooksInRegistrationOrder(t *testing.T) {
	c := newTestConfig(t, WithPrefix("load-hooks/"))

	var order []string
	c.AddReloadHook(func() { order = append(order, "first") })
	c.AddReloadHook(func() { order = append(order, "second") })

	if _, err := c.Load(WithConfig(map[string]any{"a": 1})); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("hook order after first load = %v, want [first second]", order)
	}

	order = nil
	if _, err := c.Load(WithConfig(map[string]any{"b": 2})); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(order) != 2 {
		t.Errorf("hooks did not stay registered for a later load: %v", order)
	}
}

// 20-api@toxin.Reload::SIDE-EFFECT: reload loads the configuration anew;
// with the clear flag set the cached settings are wiped first.
func TestReloadLoadsAnewAndClearWipesFirst(t *testing.T) {
	raw := newRawClient(t)
	rawPut(t, raw, "/reload-ns/app.key", `"fresh"`)

	c := newTestConfig(t, WithPrefix("reload-ns/"))
	if _, err := c.Get("stale.default", "stale"); err != nil {
		t.Fatalf("Get: %v", err)
	}

	if err := c.Reload(true); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	got, err := c.Get("app.key", nil, CacheOnly(true))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "fresh" {
		t.Errorf("cacheOnly Get after Reload = %v, want the reloaded %q", got, "fresh")
	}
	stale, err := c.Get("stale.default", nil, CacheOnly(true))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stale != nil {
		t.Errorf("clear flag left the stale cache entry %v", stale)
	}
}
