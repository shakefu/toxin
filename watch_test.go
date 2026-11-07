package toxin

import (
	"testing"
	"time"

	"github.com/shakefu/toxin/internal/etcd"
)

// ensureWatching blocks until the config's watcher demonstrably delivers
// events: a probe key is written repeatedly until the watcher caches one,
// because a write that lands before the watch stream registers would
// otherwise be missed by every later assertion.
func ensureWatching(t *testing.T, raw *etcd.Client, c *Config, ns string) {
	t.Helper()
	const probe = "zzprobe"
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		rawPut(t, raw, "/"+ns+probe, `"live"`)
		if got, _ := c.Get(probe, nil, CacheOnly(true)); got == "live" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("watcher on %s never delivered a probe event", ns)
}

// 20-api@Config.Watch::SIDE-EFFECT: with caching disabled, watching is
// skipped with a warning — both when requested at construction and when
// Watch is called directly.
func TestWatchSkippedWithWarningWhenCachingDisabled(t *testing.T) {
	atConstruction := newTestConfig(t, WithPrefix("watch-nocache/"), WithCache(false), WithWatch(true))
	if atConstruction.watcher != nil {
		t.Error("watching started at construction with caching disabled")
	}

	direct := newTestConfig(t, WithPrefix("watch-nocache2/"), WithCache(false))
	buf := captureLog(direct)
	direct.Watch()
	if direct.watcher != nil {
		t.Error("Watch started a watcher with caching disabled")
	}
	if buf.Len() == 0 {
		t.Error("Watch with caching disabled logged no warning")
	}
}

// 20-api@Config.Watch::SIDE-EFFECT: at most one watcher runs per config.
func TestWatchRunsAtMostOneWatcherPerConfig(t *testing.T) {
	c := newTestConfig(t, WithPrefix("watch-once/"))
	t.Cleanup(func() { _ = c.Close() })

	c.Watch()
	first := c.watcher
	c.Watch()
	if c.watcher != first || first == nil {
		t.Error("a second Watch replaced or added a watcher, want at most one")
	}
}

// 20-api@Config.Watch::SIDE-EFFECT: a set event decodes the value and
// stores it under the prefix-stripped key.
func TestWatchSetEventStoresDecodedValueUnderStrippedKey(t *testing.T) {
	raw := newRawClient(t)
	c := newTestConfig(t, WithPrefix("watch-set/"))
	t.Cleanup(func() { _ = c.Close() })
	c.Watch()
	ensureWatching(t, raw, c, "watch-set/")

	rawPut(t, raw, "/watch-set/app.num", "42")
	waitFor(t, 10*time.Second, "the set event to reach the cache", func() bool {
		got, err := c.Get("app.num", nil, CacheOnly(true))
		return err == nil && got != nil
	})
	got, err := c.Get("app.num", nil, CacheOnly(true))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !jsonEqual(t, got, 42) {
		t.Errorf("watched app.num = %v (%T), want the decoded 42", got, got)
	}
}

// 20-api@Config.Watch::SIDE-EFFECT: a single-key delete event drops that
// cache key.
func TestWatchSingleKeyDeleteDropsCacheKey(t *testing.T) {
	raw := newRawClient(t)
	c := newTestConfig(t, WithPrefix("watch-del/"))
	t.Cleanup(func() { _ = c.Close() })
	c.Watch()

	if err := c.Set("app.key", "value"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := c.Get("app.key", nil); err != nil {
		t.Fatalf("Get: %v", err)
	}
	ensureWatching(t, raw, c, "watch-del/")

	if _, _, err := raw.Delete(t.Context(), "/watch-del/app.key", false); err != nil {
		t.Fatalf("raw delete: %v", err)
	}
	waitFor(t, 10*time.Second, "the delete event to drop the cache key", func() bool {
		got, _ := c.Get("app.key", nil, CacheOnly(true))
		return got == nil
	})
}

// 20-api@Config.Watch::SIDE-EFFECT: a directory delete covering the whole
// namespace empties the entire cache when clearing is allowed.
func TestWatchDirectoryDeleteEmptiesCacheWhenClearingAllowed(t *testing.T) {
	raw := newRawClient(t)
	c := newTestConfig(t, WithPrefix("watch-dirallow/"), WithAllowClear(true))
	t.Cleanup(func() { _ = c.Close() })
	c.Watch()
	ensureWatching(t, raw, c, "watch-dirallow/")

	rawPut(t, raw, "/watch-dirallow/one", "1")
	rawPut(t, raw, "/watch-dirallow/two", "2")
	waitFor(t, 10*time.Second, "both watched keys to reach the cache", func() bool {
		a, _ := c.Get("one", nil, CacheOnly(true))
		b, _ := c.Get("two", nil, CacheOnly(true))
		return a != nil && b != nil
	})

	// A recursive delete lands on one revision, coalescing into a
	// directory delete event.
	if _, _, err := raw.Delete(t.Context(), "/watch-dirallow", true); err != nil {
		t.Fatalf("raw recursive delete: %v", err)
	}
	waitFor(t, 10*time.Second, "the directory delete to empty the cache", func() bool {
		return len(c.cacheSnapshot()) == 0
	})
}

// 20-api@Config.Watch::SIDE-EFFECT: a directory delete covering the whole
// namespace leaves the cache unchanged when clearing is not allowed.
func TestWatchDirectoryDeleteLeavesCacheWhenClearingNotAllowed(t *testing.T) {
	raw := newRawClient(t)
	c := newTestConfig(t, WithPrefix("watch-dirdeny/"))
	t.Cleanup(func() { _ = c.Close() })
	c.Watch()
	ensureWatching(t, raw, c, "watch-dirdeny/")

	rawPut(t, raw, "/watch-dirdeny/one", "1")
	rawPut(t, raw, "/watch-dirdeny/two", "2")
	waitFor(t, 10*time.Second, "both watched keys to reach the cache", func() bool {
		a, _ := c.Get("one", nil, CacheOnly(true))
		b, _ := c.Get("two", nil, CacheOnly(true))
		return a != nil && b != nil
	})

	if _, _, err := raw.Delete(t.Context(), "/watch-dirdeny", true); err != nil {
		t.Fatalf("raw recursive delete: %v", err)
	}
	// Wait until the delete is visible in etcd, then give the watcher a
	// grace window in which it could wrongly react.
	waitFor(t, 10*time.Second, "the etcd namespace to be gone", func() bool {
		_, ok := rawGet(t, raw, "/watch-dirdeny/one")
		return !ok
	})
	time.Sleep(500 * time.Millisecond)

	snapshot := c.cacheSnapshot()
	if !jsonEqual(t, snapshot["one"], 1) || !jsonEqual(t, snapshot["two"], 2) {
		t.Errorf("cache after a denied directory delete = %v, want it unchanged with both keys", snapshot)
	}
}

// 20-api@Config.Watch::SIDE-EFFECT (MUST-NOT): no cache entry is stored
// for a key outside the namespace.
func TestWatchStoresNothingForKeysOutsideTheNamespace(t *testing.T) {
	raw := newRawClient(t)
	c := newTestConfig(t, WithPrefix("watch-outside/"))
	t.Cleanup(func() { _ = c.Close() })
	c.Watch()
	ensureWatching(t, raw, c, "watch-outside/")

	// A sibling key sharing the namespace as a mere string prefix, and an
	// unrelated key — both outside the slash boundary.
	rawPut(t, raw, "/watch-outside-other/leaked", "x")
	rawPut(t, raw, "/watch-outsideX/leaked2", "y")
	rawPut(t, raw, "/watch-outside/inside", "ok")
	waitFor(t, 10*time.Second, "the inside key to reach the cache", func() bool {
		got, _ := c.Get("inside", nil, CacheOnly(true))
		return got == "ok"
	})
	time.Sleep(300 * time.Millisecond)

	snapshot := c.cacheSnapshot()
	if _, ok := snapshot["leaked"]; ok {
		t.Errorf("cache stored an entry for a key outside the namespace: %v", snapshot)
	}
	if _, ok := snapshot["leaked2"]; ok {
		t.Errorf("cache stored an entry for a string-prefix sibling: %v", snapshot)
	}
	if snapshot["inside"] != "ok" {
		t.Errorf("cache = %v, want the inside key stored", snapshot)
	}
}

// 20-api@Config.Close::SIDE-EFFECT: Close stops the watcher without
// closing other cluster connections, and succeeds when called twice.
func TestCloseStopsWatcherAndSucceedsTwice(t *testing.T) {
	raw := newRawClient(t)
	c := newTestConfig(t, WithPrefix("watch-close/"))

	c.Watch()
	watcher := c.watcher

	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	// The watcher's event channel closes when it stops.
	select {
	case _, ok := <-watcher.Events():
		if ok {
			t.Fatal("an event arrived on a stopped watcher's channel")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher's event channel never closed after Close")
	}

	// The cluster connection still serves the config.
	rawPut(t, raw, "/watch-close/after", `"close"`)
	got, err := c.Get("after", nil, Cached(false))
	if err != nil {
		t.Fatalf("Get after Close: %v", err)
	}
	if got != "close" {
		t.Errorf("Get after Close = %v, want the cluster connection alive", got)
	}
}

// compile-time check that the etcd import stays used by the raw helpers.
var _ = etcd.TrimKey
