package toxin

import (
	"errors"
	"testing"
	"time"

	"github.com/shakefu/toxin/internal/etcd"
)

// 20-api@Config.Get::RETURN / 10-etcd3@ValueEncoding: etcd holds the key,
// so the value is decoded and returned; numbers, booleans, null, arrays
// and objects survive a write-read cycle intact and a plain string that is
// not valid JSON comes back as itself. The found value is cached.
func TestGetReturnsDecodedValueAndCachesIt(t *testing.T) {
	writer := newTestConfig(t, WithPrefix("get-round/"))
	reader := newTestConfig(t, WithPrefix("get-round/"))

	cases := []struct {
		key  string
		set  any
		want any
	}{
		{key: "app.str", set: "hello", want: "hello"},
		{key: "app.num", set: 42, want: 42},
		{key: "app.float", set: 3.5, want: 3.5},
		{key: "app.bool", set: true, want: true},
		{key: "app.null", set: nil, want: nil},
		{key: "app.list", set: []any{"a", "b"}, want: []any{"a", "b"}},
		{key: "app.obj", set: map[string]any{"host": "db"}, want: map[string]any{"host": "db"}},
		// A value that is not valid JSON is delivered as the raw string.
		{key: "app.raw", set: "not json {", want: "not json {"},
		// A string that happens to be valid JSON decodes as JSON.
		{key: "app.jsonstr", set: "[1, 2]", want: []any{1.0, 2.0}},
	}
	for _, tc := range cases {
		if err := writer.Set(tc.key, tc.set); err != nil {
			t.Fatalf("Set(%q): %v", tc.key, err)
		}
	}
	for _, tc := range cases {
		got, err := reader.Get(tc.key, nil)
		if err != nil {
			t.Fatalf("Get(%q): %v", tc.key, err)
		}
		if tc.want == nil {
			if got != nil {
				t.Errorf("Get(%q) = %v, want nil", tc.key, got)
			}
			continue
		}
		if !jsonEqual(t, got, tc.want) {
			t.Errorf("Get(%q) = %#v, want %#v", tc.key, got, tc.want)
		}
	}
}

// root@toxin::INVARIANT: configuration values are data — a string that
// looks like code comes back verbatim, never executed.
func TestGetReturnsCodeLookingValuesVerbatimAsData(t *testing.T) {
	c := newTestConfig(t, WithPrefix("get-nodata/"))
	scripts := []string{
		"$(rm -rf /tmp)",
		"`cat /etc/passwd`",
		"{{ template \"x\" }}",
	}
	for _, s := range scripts {
		if err := c.Set("app.script", s); err != nil {
			t.Fatalf("Set: %v", err)
		}
		c.emptyCache()
		got, err := c.Get("app.script", nil)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got != s {
			t.Errorf("Get(script) = %q, want it back verbatim as data", got)
		}
	}
}

// 20-api@Config.Get::RETURN: caching is enabled and the key is in the
// cache, so the cached value returns without contacting etcd.
func TestGetCacheHitDoesNotContactEtcd(t *testing.T) {
	raw := newRawClient(t)
	c := newTestConfig(t, WithPrefix("get-cachehit/"))

	if err := c.Set("app.key", "v1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := c.Get("app.key", nil); err != nil {
		t.Fatalf("Get: %v", err)
	}

	// The etcd copy disappears; a cached read never notices.
	if _, _, err := raw.Delete(t.Context(), "/get-cachehit/app.key", false); err != nil {
		t.Fatalf("raw delete: %v", err)
	}
	got, err := c.Get("app.key", nil)
	if err != nil {
		t.Fatalf("Get after raw delete: %v", err)
	}
	if got != "v1" {
		t.Errorf("Get after raw delete = %v, want the cached %q", got, "v1")
	}
}

// 20-api@Config.Get::RETURN: with cached false the cache is not
// consulted, so a changed etcd value is read fresh.
func TestGetCachedFalseBypassesTheCache(t *testing.T) {
	raw := newRawClient(t)
	c := newTestConfig(t, WithPrefix("get-bypass/"))

	if err := c.Set("app.key", "v1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := c.Get("app.key", nil); err != nil {
		t.Fatalf("Get: %v", err)
	}
	rawPut(t, raw, "/get-bypass/app.key", "v2")

	got, err := c.Get("app.key", nil, Cached(false))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "v2" {
		t.Errorf("Get(cached=false) = %v, want the fresh %q", got, "v2")
	}
}

// 20-api@Config.Get::RETURN: cacheOnly is set and the key is not in the
// cache, so the default returns without contacting etcd — even though
// etcd holds the key.
func TestGetCacheOnlyReturnsDefaultWithoutContactingEtcd(t *testing.T) {
	c := newTestConfig(t, WithPrefix("get-cacheonly/"))

	if err := c.Set("app.key", "stored"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	c.emptyCache()

	got, err := c.Get("app.key", "default", CacheOnly(true))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "default" {
		t.Errorf("Get(cacheOnly) = %v, want the default %q", got, "default")
	}
}

// 20-api@Config.Get::RETURN: cacheOnly is set and caching is disabled by
// construction, so the default returns without contacting etcd.
func TestGetCacheOnlyWithCachingDisabledReturnsDefault(t *testing.T) {
	c := newTestConfig(t, WithPrefix("get-nocache/"), WithCache(false))

	if err := c.Set("app.key", "stored"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := c.Get("app.key", "default", CacheOnly(true))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "default" {
		t.Errorf("Get(cacheOnly, cache disabled) = %v, want %q", got, "default")
	}
}

// 20-api@Config.Get::RETURN: the key is unset and a default is given, so
// the default returns and is stored in the cache unless cacheResult is
// false.
func TestGetStoresDefaultInCacheUnlessCacheResultFalse(t *testing.T) {
	c := newTestConfig(t, WithPrefix("get-default/"))

	if _, err := c.Get("unset.key", "sentinel"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := c.Get("unset.key", "other", CacheOnly(true))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "sentinel" {
		t.Errorf("stored default = %v, want %q read back from the cache", got, "sentinel")
	}

	other := newTestConfig(t, WithPrefix("get-default2/"))
	if _, err := other.Get("unset.key", "sentinel", CacheResult(false)); err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err = other.Get("unset.key", "fresh", CacheOnly(true))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "fresh" {
		t.Errorf("cacheResult=false stored the default anyway: got %v, want %q", got, "fresh")
	}
}

// 20-api@Config.Get::INPUT / root@toxin::INVARIANT: the key is lowercased
// unless case sensitivity is enabled; with it enabled, key case is
// preserved through storage and lookup.
func TestGetLowercasesKeyUnlessCaseSensitive(t *testing.T) {
	insensitive := newTestConfig(t, WithPrefix("get-case/"))
	if err := insensitive.Set("APP.KEY", "lower"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := insensitive.Get("app.key", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "lower" {
		t.Errorf("case-insensitive Get(app.key) = %v, want %q", got, "lower")
	}

	sensitive := newTestConfig(t, WithPrefix("get-case/"), WithCaseSensitive(true))
	if err := sensitive.Set("Mixed.Case", "kept"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err = sensitive.GetStrict("Mixed.Case")
	if err != nil {
		t.Fatalf("GetStrict: %v", err)
	}
	if got != "kept" {
		t.Errorf("case-sensitive GetStrict(Mixed.Case) = %v, want %q", got, "kept")
	}
	if _, err := sensitive.GetStrict("mixed.case"); err == nil {
		t.Error("case-sensitive lookup of the lowercased key found the mixed-case key")
	}
}

// 20-api@Config.Get::ERROR: an etcd read failure other than not-found is
// returned to the caller.
func TestGetSurfacesEtcdReadFailure(t *testing.T) {
	c := newTestConfig(t, WithPrefix("get-fail/"))
	// A closed cluster connection fails reads without a not-found shape.
	if err := c.client.Close(); err != nil {
		t.Fatalf("closing the client: %v", err)
	}

	start := time.Now()
	_, err := c.Get("app.key", "default")
	if err == nil {
		t.Fatal("Get succeeded against a closed cluster, want the etcd failure")
	}
	if el := time.Since(start); el > 20*time.Second {
		t.Errorf("Get against a closed cluster took %s, want a fast failure", el)
	}
	var etcdErr *etcd.Error
	if !errors.As(err, &etcdErr) {
		t.Errorf("error %q is not shaped as an etcd failure", err)
	}
}

// 20-api@Config.Get::ERROR: the etcd response carries no entry, so the
// key is treated as unset with a logged warning, and an absent key is a
// value outcome answered by the default, never an error.
func TestGetTreatsMissingEntryAsUnsetValueWithWarning(t *testing.T) {
	c := newTestConfig(t, WithPrefix("get-missing/"))
	buf := captureLog(c)

	got, err := c.Get("absent.key", "fallback")
	if err != nil {
		t.Fatalf("Get of a missing key errored: %v", err)
	}
	if got != "fallback" {
		t.Errorf("Get of a missing key = %v, want the default %q", got, "fallback")
	}
	if buf.Len() == 0 {
		t.Error("no warning logged for a response carrying no entry")
	}
}

// 20-api@Config.Get::RETURN: the strict variant finds a set key.
func TestGetStrictReturnsStoredValue(t *testing.T) {
	c := newTestConfig(t, WithPrefix("get-strict/"))

	if err := c.Set("app.key", "value"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := c.GetStrict("app.key")
	if err != nil {
		t.Fatalf("GetStrict: %v", err)
	}
	if got != "value" {
		t.Errorf("GetStrict = %v, want %q", got, "value")
	}
}

// 20-api@Config.Get::INPUT: per-call options compose — a cacheOnly get
// never consults etcd even for its default.
func TestGetCacheOnlyDefaultNotCachedWithoutEtcd(t *testing.T) {
	c := newTestConfig(t, WithPrefix("get-opts/"))

	if _, err := c.Get("unset.key", "def", CacheOnly(true), CacheResult(true)); err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := c.Get("unset.key", "fresh", CacheOnly(true))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "fresh" {
		t.Errorf("cacheOnly default leaked into the cache: got %v, want %q", got, "fresh")
	}
}

// 10-etcd3@ValueEncoding: decoded numbers keep their literal form through
// a second write-read cycle.
func TestGetNumberSurvivesSecondRoundTrip(t *testing.T) {
	writer := newTestConfig(t, WithPrefix("get-num2/"))
	reader := newTestConfig(t, WithPrefix("get-num2/"))

	if err := writer.Set("app.big", 12345678901234); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, err := reader.Get("app.big", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := writer.Set("app.big", v); err != nil {
		t.Fatalf("re-Set: %v", err)
	}
	reader2 := newTestConfig(t, WithPrefix("get-num2/"))
	v2, err := reader2.Get("app.big", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !jsonEqual(t, v2, 12345678901234) {
		t.Errorf("second round trip = %v (%T), want the integer literal intact", v2, v2)
	}
}
