package toxin

import (
	"testing"
	"time"
)

// 20-api@Config.Set::INPUT / 10-etcd3@ValueEncoding / EtcdKeys: a string
// value is stored verbatim and any other value as its JSON encoding, and
// the lowercased key is written under the prefix.
func TestSetStoresStringVerbatimOthersJSONUnderPrefix(t *testing.T) {
	raw := newRawClient(t)
	c := newTestConfig(t, WithPrefix("set-form/"))

	cases := []struct {
		key      string
		value    any
		wantRaw  string
		wantAddr string
	}{
		{key: "App.Str", value: "hello", wantRaw: "hello", wantAddr: "/set-form/app.str"},
		{key: "app.num", value: 42, wantRaw: "42", wantAddr: "/set-form/app.num"},
		{key: "app.float", value: 3.5, wantRaw: "3.5", wantAddr: "/set-form/app.float"},
		{key: "app.bool", value: false, wantRaw: "false", wantAddr: "/set-form/app.bool"},
		{key: "app.null", value: nil, wantRaw: "null", wantAddr: "/set-form/app.null"},
		{key: "app.list", value: []any{1, "two"}, wantRaw: "[1,\"two\"]", wantAddr: "/set-form/app.list"},
	}
	for _, tc := range cases {
		if err := c.Set(tc.key, tc.value); err != nil {
			t.Fatalf("Set(%q): %v", tc.key, err)
		}
		got, ok := rawGet(t, raw, tc.wantAddr)
		if !ok {
			t.Fatalf("no raw value at %s after Set(%q)", tc.wantAddr, tc.key)
		}
		if got != tc.wantRaw {
			t.Errorf("raw value at %s = %q, want %q", tc.wantAddr, got, tc.wantRaw)
		}
	}
}

// root@toxin::INVARIANT: nothing is written outside the configured
// namespace prefix.
func TestSetWritesNothingOutsideTheNamespacePrefix(t *testing.T) {
	raw := newRawClient(t)
	c := newTestConfig(t, WithPrefix("set-inside/"))

	if err := c.Set("app.key", "value"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	entries, err := raw.Read(t.Context(), "/set-inside", true)
	if err != nil {
		t.Fatalf("raw read: %v", err)
	}
	for _, e := range entries {
		if e.Key != "/set-inside/app.key" {
			t.Errorf("Set wrote key %q outside the namespace prefix", e.Key)
		}
	}
}

// 20-api@Config.Set::SIDE-EFFECT: with cacheOnly the cache alone is
// written — and nothing at all when caching is disabled.
func TestSetCacheOnlyWritesCacheOnly(t *testing.T) {
	raw := newRawClient(t)
	c := newTestConfig(t, WithPrefix("set-cacheonly/"))

	if err := c.Set("app.key", "cached", CacheOnly(true)); err != nil {
		t.Fatalf("Set(cacheOnly): %v", err)
	}
	if _, ok := rawGet(t, raw, "/set-cacheonly/app.key"); ok {
		t.Error("Set(cacheOnly) wrote to etcd, want the cache only")
	}
	got, err := c.Get("app.key", nil, CacheOnly(true))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "cached" {
		t.Errorf("cache after Set(cacheOnly) = %v, want %q", got, "cached")
	}

	off := newTestConfig(t, WithPrefix("set-cacheoff/"), WithCache(false))
	if err := off.Set("app.key", "nothing", CacheOnly(true)); err != nil {
		t.Fatalf("Set(cacheOnly, cache disabled): %v", err)
	}
	if _, ok := rawGet(t, raw, "/set-cacheoff/app.key"); ok {
		t.Error("Set(cacheOnly) with caching disabled wrote to etcd, want nothing at all")
	}
}

// 20-api@Config.Set::SIDE-EFFECT: without cacheOnly, etcd is written first
// and the cache updated only after the write succeeds — observable as a
// failed write leaving no cache entry, and a successful one landing in the
// cache.
func TestSetUpdatesCacheOnlyAfterEtcdWriteSucceeds(t *testing.T) {
	c := newTestConfig(t, WithPrefix("set-afterwrite/"))

	if err := c.Set("app.key", "value"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := c.Get("app.key", nil, CacheOnly(true))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "value" {
		t.Errorf("cache after Set = %v, want %q", got, "value")
	}

	// A closed cluster fails the write; the cache must stay without the
	// new key.
	c2 := newTestConfig(t, WithPrefix("set-afterfail/"))
	if err := c2.client.Close(); err != nil {
		t.Fatalf("closing the client: %v", err)
	}
	if err := c2.Set("app.key", "value"); err == nil {
		t.Fatal("Set succeeded against a closed cluster, want the write failure reported")
	}
	got, err = c2.Get("app.key", nil, CacheOnly(true))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("cache holds %v after a failed Set, want no entry", got)
	}
}

// 20-api@Config.Set::INPUT: the ttl option grants the written key a lease
// that expires it that long after the write.
func TestSetWithTTLExpiresTheKey(t *testing.T) {
	raw := newRawClient(t)
	c := newTestConfig(t, WithPrefix("set-ttl/"))

	if err := c.Set("app.key", "fleeting", WithTTL(1)); err != nil {
		t.Fatalf("Set(ttl): %v", err)
	}
	if _, ok := rawGet(t, raw, "/set-ttl/app.key"); !ok {
		t.Fatal("key missing immediately after a ttl write")
	}

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := rawGet(t, raw, "/set-ttl/app.key"); !ok {
			return // expired
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Error("key still present seconds after its lease should have expired it")
}
