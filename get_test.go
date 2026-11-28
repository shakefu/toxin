package toxin

import (
	"errors"
	"os"
	"strings"
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

// 20-api@Config.Get::ERROR: an etcd read failure other than not-found is
// returned to the caller by the strict variant too.
func TestGetStrictSurfacesEtcdReadFailure(t *testing.T) {
	c := newTestConfig(t, WithPrefix("get-strict-fail/"))
	if err := c.client.Close(); err != nil {
		t.Fatalf("closing the client: %v", err)
	}

	_, err := c.GetStrict("app.key")
	if err == nil {
		t.Fatal("GetStrict succeeded against a closed cluster, want the etcd failure")
	}
	var etcdErr *etcd.Error
	if !errors.As(err, &etcdErr) {
		t.Errorf("error %q is not shaped as an etcd failure", err)
	}
}

// 20-api@Config.Get::ERROR: the etcd response carries no entry — here the
// directory-key shape, entries below the key with none matching the key
// itself — so the key is treated as unset with a logged warning and the
// default returns. (A dot is not a path separator in the key form —
// "app.x" lives beside "app", not below it — so the child is stored with a
// slash.)
func TestGetTreatsDirectoryKeyAsUnsetValueWithWarning(t *testing.T) {
	c := newTestConfig(t, WithPrefix("get-dirkey/"))
	if err := c.Set("app/leaf", "v"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	buf := captureLog(c)

	got, err := c.Get("app", "fallback")
	if err != nil {
		t.Fatalf("Get of a directory key errored: %v", err)
	}
	if got != "fallback" {
		t.Errorf("Get of a directory key = %v, want the default %q", got, "fallback")
	}
	if !strings.Contains(buf.String(), "Key not found: /get-dirkey/app") {
		t.Errorf("warning %q does not treat the directory key as unset", buf.String())
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

// closedClientConfig returns a Config whose etcd connection is closed, so
// every read fails without a not-found shape.
func closedClientConfig(t *testing.T, opts ...Option) *Config {
	t.Helper()
	c := newTestConfig(t, opts...)
	if err := c.client.Close(); err != nil {
		t.Fatalf("closing the client: %v", err)
	}
	return c
}

// 20-api@Config.Get::ERROR / INVARIANT: a failed etcd read returns the
// call-site default alongside the error, without caching it; a nil
// default yields nil with the error, and the strict variant returns no
// value with the error.
func TestGetFailedReadReturnsDefaultWithErrorUncached(t *testing.T) {
	c := closedClientConfig(t, WithPrefix("get-fail-def/"))

	got, err := c.Get("app.key", "default")
	var etcdErr *etcd.Error
	if !errors.As(err, &etcdErr) {
		t.Fatalf("Get error = %v, want the etcd failure", err)
	}
	if got != "default" {
		t.Errorf("failed Get = %v, want the call-site default %q", got, "default")
	}
	if v, ok := c.cachedValue("app.key"); ok {
		t.Errorf("failed Get cached the default %v; want nothing cached", v)
	}

	got, err = c.Get("app.key", nil)
	if err == nil || got != nil {
		t.Errorf("failed Get with no default = %v, %v; want nil with the error", got, err)
	}

	got, err = c.GetStrict("app.key")
	if err == nil || got != nil {
		t.Errorf("failed GetStrict = %v, %v; want nil with the error", got, err)
	}
	if errors.Is(err, ErrNoSetting) {
		t.Errorf("failed GetStrict = %v, want the etcd failure rather than an unset key", err)
	}
}

// 20-api@Config.Get::ERROR: a file-cache-loaded value still answers from
// the cache once etcd fails, and a miss against the failing cluster
// returns the default with the error without caching it or writing it to
// the file cache.
func TestGetFileCacheWithFailingClusterReturnsDefaultUncached(t *testing.T) {
	dir := t.TempDir()
	c := newTestConfig(t, WithPrefix("get-fc-fail/"), WithFileCache(dir))
	if err := c.fileCache.Save(map[string]any{"app.cached": "fromfile"}); err != nil {
		t.Fatalf("saving the file cache: %v", err)
	}
	if _, err := c.Load(); err != nil {
		t.Fatalf("Load from the file cache: %v", err)
	}
	before, err := os.ReadFile(c.fileCache.Filename())
	if err != nil {
		t.Fatalf("reading the file cache: %v", err)
	}
	if err := c.client.Close(); err != nil {
		t.Fatalf("closing the client: %v", err)
	}

	got, err := c.Get("app.cached", "def")
	if err != nil || got != "fromfile" {
		t.Errorf("cached Get = %v, %v; want the file-cache value without error", got, err)
	}

	got, err = c.Get("app.miss", "def")
	if err == nil || got != "def" {
		t.Errorf("missing Get = %v, %v; want the default with the etcd failure", got, err)
	}
	if v, ok := c.cachedValue("app.miss"); ok {
		t.Errorf("failed Get cached the default %v; want nothing cached", v)
	}
	after, err := os.ReadFile(c.fileCache.Filename())
	if err != nil {
		t.Fatalf("reading the file cache: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("file cache changed to %s after a failed Get; want %s", after, before)
	}
}

// 20-api@Config.Get::ERROR / 10-etcd3@etcd.Client::INVARIANT: against an
// unreachable cluster the first miss waits out the retries, then repeat
// misses and Env lookups through Get fail fast, each answering with its
// own default. 192.0.2.1 is TEST-NET-1, reserved and never reachable.
func TestGetUnreachableClusterFailsFastAfterFirstFailure(t *testing.T) {
	clearToxinEnv(t)
	c, err := New(WithHosts("192.0.2.1:2379"), WithPrefix("get-unreach/"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.client.Close() })

	got, err := c.Get("app.first", "first-default")
	if err == nil || got != "first-default" {
		t.Fatalf("first Get = %v, %v; want the default with the etcd failure", got, err)
	}

	for _, key := range []string{"app.second", "app.first", "app.third"} {
		start := time.Now()
		got, err := c.Get(key, key+"-default")
		if el := time.Since(start); el >= time.Second {
			t.Errorf("repeat Get(%q) took %s, want a fast failure under one second", key, el)
		}
		if err == nil || got != key+"-default" {
			t.Errorf("repeat Get(%q) = %v, %v; want the default with the etcd failure", key, got, err)
		}
		if v, ok := c.cachedValue(key); ok {
			t.Errorf("failed Get(%q) cached %v; want nothing cached", key, v)
		}
	}

	const envKey = "TOXIN_TEST_UNREACHABLE_DB_HOST"
	t.Setenv(envKey, "")
	if err := os.Unsetenv(envKey); err != nil {
		t.Fatalf("unsetting %s: %v", envKey, err)
	}
	lookup := func(setting string) (any, bool) {
		v, err := c.Get(setting, nil)
		return v, err == nil && v != nil
	}
	start := time.Now()
	for i := 0; i < 3; i++ {
		if v := Env(envKey, lookup, "env-default"); v != "env-default" {
			t.Errorf("Env = %v, want the default %q", v, "env-default")
		}
	}
	if el := time.Since(start); el >= time.Second {
		t.Errorf("Env lookups took %s, want fast failures under one second", el)
	}
}

// 20-api@toxin.Default::RETURN: when the shared instance cannot be
// constructed, package-level Get returns its default with the error.
func TestPackageGetReturnsDefaultWhenDefaultFails(t *testing.T) {
	isolateDefault(t)
	clearToxinEnv(t)
	t.Setenv("TOXIN_LOGLEVEL", "not-a-level")

	got, err := Get("app.key", "default")
	if err == nil {
		t.Fatal("package Get succeeded with an unconstructible shared instance")
	}
	if got != "default" {
		t.Errorf("package Get = %v, want the default %q alongside the error", got, "default")
	}
	got, err = Get("app.key", nil)
	if err == nil || got != nil {
		t.Errorf("package Get with no default = %v, %v; want nil with the error", got, err)
	}
	got, err = GetStrict("app.key")
	if err == nil || got != nil {
		t.Errorf("package GetStrict = %v, %v; want nil with the error", got, err)
	}
}
