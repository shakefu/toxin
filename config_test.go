package toxin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shakefu/toxin/internal/log"
	"github.com/shakefu/toxin/internal/testutil"
)

// 20-api@toxin.New::INPUT: the construction defaults — prefix "config/",
// member list 127.0.0.1:2379, caching on, case sensitivity off,
// inheritance on with inherit key "config.inherit" and depth two,
// watching off, clearing off, log level critical.
func TestNewDefaults(t *testing.T) {
	// The default member list is only consulted with no programmatic list
	// and no TOXIN_ETCD; no live connection is needed to observe it.
	clearToxinEnv(t)
	c, err := New()
	if err != nil {
		t.Fatalf("toxin.New: %v", err)
	}

	if c.prefix != "config/" {
		t.Errorf("prefix = %q, want %q", c.prefix, "config/")
	}
	if got := c.Endpoints(); len(got) != 1 || got[0] != "http://127.0.0.1:2379" {
		t.Errorf("endpoints = %v, want [http://127.0.0.1:2379]", got)
	}
	if !c.cacheOn {
		t.Error("caching default = off, want on")
	}
	if c.caseSensitive {
		t.Error("case sensitivity default = on, want off")
	}
	if !c.inherit {
		t.Error("inheritance default = off, want on")
	}
	if c.inheritKey != "config.inherit" {
		t.Errorf("inherit key = %q, want %q", c.inheritKey, "config.inherit")
	}
	if c.inheritDepth != 2 {
		t.Errorf("inherit depth = %d, want 2", c.inheritDepth)
	}
	if c.watcher != nil {
		t.Error("watching default = on, want off")
	}
	if c.allowClear {
		t.Error("clearing default = on, want off")
	}
	if c.log.Level() != log.Critical {
		t.Errorf("log level = %v, want critical", c.log.Level())
	}
}

// 20-api@toxin.New::INPUT: the prefix is trimmed of whitespace and
// slashes, given exactly one trailing slash, and lowercased unless case
// sensitivity is enabled; a prefix that trims to nothing is the root "/".
func TestNewNormalizesPrefix(t *testing.T) {
	cases := []struct {
		prefix        string
		caseSensitive bool
		want          string
	}{
		{prefix: "config/", want: "config/"},
		{prefix: "  /config/  ", want: "config/"},
		// Trimming removes leading and trailing slashes only; interior
		// slashes are the prefix's own directory names.
		{prefix: "//app//db//", want: "app//db/"},
		{prefix: "APP/Config", want: "app/config/"},
		{prefix: "///", want: "/"},
		{prefix: "", want: "/"},
		{prefix: "  ", want: "/"},
		{prefix: "APP/Config", caseSensitive: true, want: "APP/Config/"},
	}
	for _, tc := range cases {
		opts := []Option{WithPrefix(tc.prefix)}
		if tc.caseSensitive {
			opts = append(opts, WithCaseSensitive(true))
		}
		c := newTestConfig(t, opts...)
		if c.prefix != tc.want {
			t.Errorf("New(prefix %q, caseSensitive %v) prefix = %q, want %q",
				tc.prefix, tc.caseSensitive, c.prefix, tc.want)
		}
	}
}

// 20-api@toxin.New::INPUT: TOXIN_PREFIX is consulted only when the prefix
// option is not supplied, and a prefix it supplies is normalized too.
func TestNewTOXINPrefixOnlyWhenOptionNotSupplied(t *testing.T) {
	clearToxinEnv(t)
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))
	t.Setenv("TOXIN_PREFIX", "/Env/")

	fromEnv, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if fromEnv.prefix != "env/" {
		t.Errorf("TOXIN_PREFIX prefix = %q, want %q (normalized)", fromEnv.prefix, "env/")
	}

	fromOption, err := New(WithPrefix("option/"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if fromOption.prefix != "option/" {
		t.Errorf("prefix with option supplied = %q, want %q", fromOption.prefix, "option/")
	}
}

// 20-api@toxin.New::INPUT: TOXIN_WATCH is consulted only when the watch
// option is not supplied; any non-empty value is truthy, an empty value
// reads as unset.
func TestNewTOXINWatchOnlyWhenOptionNotSupplied(t *testing.T) {
	// Any non-empty value enables watching.
	c := newTestConfigWithEnv(t, map[string]string{"TOXIN_WATCH": "1"})
	if c.watcher == nil {
		t.Error("TOXIN_WATCH=1 did not start the watcher at construction")
	}

	// An empty value reads as unset.
	c = newTestConfigWithEnv(t, map[string]string{"TOXIN_WATCH": ""})
	if c.watcher != nil {
		t.Error("empty TOXIN_WATCH started the watcher, want it to read as unset")
	}

	// A supplied option wins over the environment.
	c = newTestConfigWithEnv(t, map[string]string{"TOXIN_WATCH": "yes"}, WithWatch(false))
	if c.watcher != nil {
		t.Error("TOXIN_WATCH overrode a supplied WithWatch(false), want option to win")
	}
}

// 20-api@toxin.New::INPUT: TOXIN_LOGLEVEL overrides the log level option,
// with a valid value.
func TestNewTOXINLOGLEVELOverridesOption(t *testing.T) {
	c := newTestConfigWithEnv(t, map[string]string{"TOXIN_LOGLEVEL": "debug"}, WithLogLevel("info"))
	if c.log.Level() != log.Debug {
		t.Errorf("log level = %v, want debug from TOXIN_LOGLEVEL", c.log.Level())
	}
}

// 20-api@toxin.New::INPUT: TOXIN_CACHE supplies the file cache directory,
// overriding the option; the cache directory is created and a file in it
// opened at construction.
func TestNewTOXINCacheSuppliesFileCacheDirectory(t *testing.T) {
	optionDir := t.TempDir()
	envDir := t.TempDir()

	clearToxinEnv(t)
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))
	t.Setenv("TOXIN_CACHE", envDir)

	c, err := New(WithFileCache(optionDir), WithPrefix("cfg-env-cache/"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.fileCache == nil {
		t.Fatal("no file cache configured from TOXIN_CACHE")
	}
	if got := c.fileCache.Filename(); got != filepath.Join(envDir, "cfg-env-cache.json") {
		t.Errorf("cache file = %q, want it under the TOXIN_CACHE directory %q", got, envDir)
	}
	if _, err := os.Stat(filepath.Join(envDir, ".toxin")); err != nil {
		t.Errorf("no file opened in the cache directory at construction: %v", err)
	}
}

// 20-api@toxin.New::INPUT: a configured file cache has its directory
// created and a file in it opened at construction.
func TestNewCreatesFileCacheDirectoryAtConstruction(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "cache")

	clearToxinEnv(t)
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))
	if _, err := New(WithFileCache(dir), WithPrefix("cfg-mkcache/")); err != nil {
		t.Fatalf("New with a fresh cache directory: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("cache directory not created at construction: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".toxin")); err != nil {
		t.Errorf("no file opened in the cache directory at construction: %v", err)
	}
}

// 10-etcd3@etcd.Client::INPUT, bound by toxin.New's whole-spec CONFORMS:
// TOXIN_ETCD is used in place of any programmatic member list.
func TestNewTOXINEtcdOverridesProgrammaticHosts(t *testing.T) {
	clearToxinEnv(t)
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))

	c, err := New(WithHosts("10.255.255.1:2379"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	endpoint := testutil.Etcd(t)
	if got := c.Endpoints(); len(got) != 1 || got[0] != endpoint {
		t.Errorf("endpoints = %v, want [%s] from TOXIN_ETCD", got, endpoint)
	}
}

// 10-etcd3@etcd.Client::INPUT, bound by toxin.New's whole-spec CONFORMS:
// the member list may be a single comma-separated string, entries that
// trim to nothing are dropped, and an absent list resolves to
// 127.0.0.1:2379.
func TestNewMemberListForms(t *testing.T) {
	c := newTestConfig(t, WithHosts(" "+testutil.Etcd(t)+" , ,"+testutil.Etcd(t)))
	got := c.Endpoints()
	// Duplicate endpoints collapse in the client's dialing; what binds
	// here is that both members were accepted from one comma-separated
	// string with whitespace and empty entries.
	if len(got) == 0 {
		t.Fatal("no endpoints resolved from a comma-separated member list")
	}
	for _, ep := range got {
		if ep != testutil.Etcd(t) {
			t.Errorf("endpoint %q is not the test server", ep)
		}
	}
}

// newTestConfigWithEnv constructs a Config with extra TOXIN_* variables
// set, on top of the cleared environment and the test server endpoint.
func newTestConfigWithEnv(t *testing.T, env map[string]string, opts ...Option) *Config {
	t.Helper()
	clearToxinEnv(t)
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))
	for k, v := range env {
		t.Setenv(k, v)
	}
	c, err := New(opts...)
	if err != nil {
		t.Fatalf("toxin.New: %v", err)
	}
	return c
}
