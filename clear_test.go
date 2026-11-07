package toxin

import (
	"testing"

	"github.com/shakefu/toxin/internal/etcd"
)

// 20-api@Config.Clear::SIDE-EFFECT: without cacheOnly the prefix
// namespace is recursively deleted — and nothing outside it.
func TestClearRecursivelyDeletesThePrefixNamespaceOnly(t *testing.T) {
	raw := newRawClient(t)
	rawPut(t, raw, "/clear-ns/app.key", "1")
	rawPut(t, raw, "/clear-ns/sub/nested", "2")
	rawPut(t, raw, "/clear-other/survivor", "3")

	c := newTestConfig(t, WithPrefix("clear-ns/"), WithAllowClear(true))
	if err := c.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	entries, err := raw.Read(t.Context(), "/clear-ns", true)
	if err != nil && !etcd.IsNotFound(err) {
		t.Fatalf("raw read after Clear: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("keys survived under the cleared namespace: %v", entries)
	}
	if _, ok := rawGet(t, raw, "/clear-other/survivor"); !ok {
		t.Error("Clear deleted a key outside the namespace prefix")
	}
}

// 20-api@Config.Clear::SIDE-EFFECT: a namespace that is already empty
// succeeds without error and leaves the cache as it is.
func TestClearEmptyNamespaceSucceedsAndLeavesCache(t *testing.T) {
	c := newTestConfig(t, WithPrefix("clear-empty/"), WithAllowClear(true))

	if _, err := c.Get("cached.default", "kept"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := c.Clear(); err != nil {
		t.Fatalf("Clear of an empty namespace errored: %v", err)
	}
	got, err := c.Get("cached.default", nil, CacheOnly(true))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "kept" {
		t.Errorf("cache after clearing an empty namespace = %v, want it left as it was", got)
	}
}

// 20-api@Config.Clear::SIDE-EFFECT: when the namespace delete removed
// entries, the cache is emptied.
func TestClearEmptiesCacheWhenEntriesWereDeleted(t *testing.T) {
	raw := newRawClient(t)
	rawPut(t, raw, "/clear-cache/app.key", `"stored"`)

	c := newTestConfig(t, WithPrefix("clear-cache/"), WithAllowClear(true))
	if _, err := c.Get("app.key", nil); err != nil {
		t.Fatalf("Get: %v", err)
	}

	if err := c.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	got, err := c.Get("app.key", nil, CacheOnly(true))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("cache after Clear = %v, want it emptied", got)
	}
}

// 20-api@Config.Clear::SIDE-EFFECT: with cacheOnly the cache alone is
// emptied and etcd is untouched.
func TestClearCacheOnlyEmptiesCacheWithoutEtcd(t *testing.T) {
	raw := newRawClient(t)
	rawPut(t, raw, "/clear-only/app.key", `"stored"`)

	c := newTestConfig(t, WithPrefix("clear-only/"), WithAllowClear(true))
	if _, err := c.Get("app.key", nil); err != nil {
		t.Fatalf("Get: %v", err)
	}

	if err := c.Clear(CacheOnly(true)); err != nil {
		t.Fatalf("Clear(cacheOnly): %v", err)
	}
	got, err := c.Get("app.key", nil, CacheOnly(true))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("cache after Clear(cacheOnly) = %v, want it emptied", got)
	}
	if _, ok := rawGet(t, raw, "/clear-only/app.key"); !ok {
		t.Error("Clear(cacheOnly) deleted from etcd, want the cache only")
	}
}
