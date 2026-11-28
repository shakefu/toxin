package toxin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearConfigEnv resets every TOXIN_ runtime variable, TOXIN_CONFIG
// included, so a test's own options and variables decide the
// configuration.
func clearConfigEnv(t *testing.T) {
	t.Helper()
	clearToxinEnv(t)
	t.Setenv("TOXIN_CONFIG", "")
}

// writeConfigFile writes body to a file named name under dir and returns
// its path.
func writeConfigFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config file %s: %v", path, err)
	}
	return path
}

// getCached reads a key from the cache alone, failing the test on error.
func getCached(t *testing.T, c *Config, key string) any {
	t.Helper()
	got, err := c.Get(key, nil, CacheOnly(true))
	if err != nil {
		t.Fatalf("Get(%q): %v", key, err)
	}
	return got
}

// assertConfigError checks err is the config-file read error naming path.
func assertConfigError(t *testing.T, err error, path string) {
	t.Helper()
	if err == nil {
		t.Fatal("got no error, want \"Could not read toxin config: ...\"")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "Could not read toxin config: ") {
		t.Errorf("error = %q, want the \"Could not read toxin config: \" prefix", msg)
	}
	if !strings.Contains(msg, path) {
		t.Errorf("error = %q, want it to name the path %s", msg, path)
	}
}

const nestedConfigJSON = `{
  "App": {
    "Name": "toxin",
    "db": {"host": "db.example.com", "port": 9007199254740993},
    "hosts": ["a", "b"],
    "empty": {}
  },
  "already.dotted": {"kept": "whole"},
  "top": "level"
}`

// 20-api@toxin.New and root@Sources: one config file flattens like
// Load(WithConfig(...)) and its keys answer right after New, with JSON
// numbers kept as json.Number and keys lowercased by default.
func TestConfigFileSingleFileFlattensAndServesAtOnce(t *testing.T) {
	clearConfigEnv(t)
	path := writeConfigFile(t, t.TempDir(), "app.json", nestedConfigJSON)

	c, err := New(WithConfigFile(path))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	want := map[string]any{
		"app.name":       "toxin",
		"app.db.host":    "db.example.com",
		"app.hosts":      []any{"a", "b"},
		"app.empty":      map[string]any{},
		"already.dotted": map[string]any{"kept": "whole"},
		"top":            "level",
	}
	for key, w := range want {
		if got := getCached(t, c, key); !jsonEqual(t, got, w) {
			t.Errorf("Get(%q) = %#v, want %#v", key, got, w)
		}
	}
	port := getCached(t, c, "APP.DB.PORT")
	if n, ok := port.(json.Number); !ok || n.String() != "9007199254740993" {
		t.Errorf("app.db.port = %#v (%T), want json.Number 9007199254740993", port, port)
	}
	if got := len(c.cacheSnapshot()); got != len(want)+1 {
		t.Errorf("cache holds %d keys, want %d: %v", got, len(want)+1, c.cacheSnapshot())
	}
}

// 20-api@toxin.New: with case sensitivity enabled config-file keys keep
// their case.
func TestConfigFileCaseSensitiveKeepsKeyCase(t *testing.T) {
	clearConfigEnv(t)
	path := writeConfigFile(t, t.TempDir(), "app.json", nestedConfigJSON)

	c, err := New(WithCaseSensitive(true), WithConfigFile(path))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := getCached(t, c, "App.Name"); got != "toxin" {
		t.Errorf("Get(App.Name) = %v, want toxin", got)
	}
	if got := getCached(t, c, "app.name"); got != nil {
		t.Errorf("Get(app.name) = %v, want nil with case sensitivity", got)
	}
}

// root@Sources: later files win over earlier files per key; list entries
// split on commas with blanks dropped, and repeated options append.
func TestConfigFileLaterFilesOverrideEarlier(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	a := writeConfigFile(t, dir, "a.json", `{"shared": "a", "app": {"a": 1, "both": "a"}}`)
	b := writeConfigFile(t, dir, "b.json", `{"shared": "b", "app": {"b": 2, "both": "b"}}`)
	d := writeConfigFile(t, dir, "d.json", `{"shared": "d"}`)

	for name, opts := range map[string][]Option{
		"WithConfigFiles":           {WithConfigFiles(a, b)},
		"comma list with blanks":    {WithConfigFile(" " + a + " , ,," + b + " ")},
		"repeated options append":   {WithConfigFile(a), WithConfigFile(b)},
		"mixed list and later file": {WithConfigFiles(d+","+a, b)},
	} {
		t.Run(name, func(t *testing.T) {
			c, err := New(opts...)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			for key, w := range map[string]any{
				"shared": "b", "app.a": 1, "app.b": 2, "app.both": "b",
			} {
				if got := getCached(t, c, key); !jsonEqual(t, got, w) {
					t.Errorf("Get(%q) = %v, want %v", key, got, w)
				}
			}
		})
	}
}

// root@Sources: under case-insensitive mode, keys that differ only by case
// across files collide and the later file wins.
func TestConfigFileCaseInsensitiveCollisionLaterFileWins(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	first := writeConfigFile(t, dir, "first.json", `{"A": "one", "App": {"Key": 1}}`)
	second := writeConfigFile(t, dir, "second.json", `{"a": "two", "app": {"key": 2}}`)

	c, err := New(WithConfigFiles(first, second))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := getCached(t, c, "A"); got != "two" {
		t.Errorf("Get(A) = %v, want the later file's two", got)
	}
	if got := getCached(t, c, "app.key"); !jsonEqual(t, got, 2) {
		t.Errorf("Get(app.key) = %v, want the later file's 2", got)
	}
	if got := len(c.cacheSnapshot()); got != 2 {
		t.Errorf("cache holds %d keys, want 2: %v", got, c.cacheSnapshot())
	}
}

// root@EnvNaming: TOXIN_CONFIG appends after programmatic files and so
// overrides them per key.
func TestConfigFileTOXINConfigAppendsAfterProgrammatic(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	prog := writeConfigFile(t, dir, "prog.json", `{"shared": "prog", "prog.only": "p"}`)
	env1 := writeConfigFile(t, dir, "env1.json", `{"shared": "env1", "env.only": "e"}`)
	env2 := writeConfigFile(t, dir, "env2.json", `{"shared": "env2"}`)
	t.Setenv("TOXIN_CONFIG", " "+env1+", ,"+env2)

	c, err := New(WithConfigFile(prog))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if want := []string{prog, env1, env2}; strings.Join(c.configFiles, "|") != strings.Join(want, "|") {
		t.Errorf("effective files = %v, want %v", c.configFiles, want)
	}
	for key, w := range map[string]any{"shared": "env2", "prog.only": "p", "env.only": "e"} {
		if got := getCached(t, c, key); got != w {
			t.Errorf("Get(%q) = %v, want %v", key, got, w)
		}
	}
}

// root@EnvNaming and 20-api@toxin.New: WithEnv(false) ignores
// TOXIN_CONFIG, even one naming a missing file, and still honors
// programmatic config files.
func TestConfigFileWithEnvFalseIgnoresTOXINConfig(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	prog := writeConfigFile(t, dir, "prog.json", `{"app": {"key": "prog"}}`)
	t.Setenv("TOXIN_CONFIG", filepath.Join(dir, "missing.json"))

	if _, err := New(); err == nil {
		t.Fatal("New with TOXIN_CONFIG naming a missing file succeeded, want it read")
	}
	c, err := New(WithEnv(false), WithConfigFile(prog))
	if err != nil {
		t.Fatalf("New(WithEnv(false)): %v", err)
	}
	if got := getCached(t, c, "app.key"); got != "prog" {
		t.Errorf("Get(app.key) = %v, want the programmatic file's value", got)
	}
	if len(c.configFiles) != 1 {
		t.Errorf("effective files = %v, want only the programmatic file", c.configFiles)
	}
}

// 20-api@toxin.New::ERROR: a config file that is missing, unreadable,
// malformed, carries trailing data, or holds other than an object fails
// construction with "Could not read toxin config: <cause>".
func TestConfigFileNewRejectsBadFiles(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	cases := map[string]string{
		"missing":       filepath.Join(dir, "missing.json"),
		"directory":     dir,
		"invalid JSON":  writeConfigFile(t, dir, "invalid.json", `{"a": `),
		"trailing data": writeConfigFile(t, dir, "trailing.json", `{"a": 1} {"b": 2}`),
		"array":         writeConfigFile(t, dir, "array.json", `[]`),
		"string":        writeConfigFile(t, dir, "string.json", `"str"`),
		"number":        writeConfigFile(t, dir, "number.json", `123`),
		"null":          writeConfigFile(t, dir, "null.json", `null`),
		"empty file":    writeConfigFile(t, dir, "empty.json", ``),
	}
	if os.Geteuid() != 0 {
		unreadable := writeConfigFile(t, dir, "unreadable.json", `{"a": 1}`)
		if err := os.Chmod(unreadable, 0o000); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		cases["unreadable"] = unreadable
	}
	good := writeConfigFile(t, dir, "good.json", `{"a": 1}`)
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			c, err := New(WithConfigFiles(good, path))
			if c != nil {
				t.Errorf("New returned a Config alongside the error")
			}
			assertConfigError(t, err, path)
		})
	}
}

// root@toxin SIDE-EFFECT: without etcd, a config file serves its keys
// right after New, missing keys fall back to their defaults, and nothing
// dials a cluster or logs.
func TestConfigFileNoEtcdServesWithoutNetwork(t *testing.T) {
	clearConfigEnv(t)
	path := writeConfigFile(t, t.TempDir(), "app.json", `{"app": {"key": "file"}}`)

	var c *Config
	var err error
	var got, missing any
	stderr := captureNoEtcdStderr(t, func() {
		if c, err = New(WithConfigFile(path)); err != nil {
			return
		}
		if got, err = c.Get("app.key", "default"); err != nil {
			return
		}
		missing, err = c.Get("app.missing", "fallback")
	})
	if err != nil {
		t.Fatalf("no-etcd config file: %v", err)
	}
	if c.client != nil || len(c.Endpoints()) != 0 {
		t.Error("a config file alone created an etcd client")
	}
	if got != "file" {
		t.Errorf("Get(app.key) = %v, want the file value", got)
	}
	if missing != "fallback" {
		t.Errorf("Get(app.missing) = %v, want the default", missing)
	}
	if stderr != "" {
		t.Errorf("no-etcd config file logged %q", stderr)
	}
}

// 20-api@toxin.Default: TOXIN_CONFIG reaches the process-wide instance and
// the package-level shortcuts.
func TestConfigFileDefaultReadsTOXINConfig(t *testing.T) {
	isolateDefault(t)
	clearConfigEnv(t)
	path := writeConfigFile(t, t.TempDir(), "app.json", `{"app": {"key": "from-env-file"}}`)
	t.Setenv("TOXIN_CONFIG", path)

	got, err := Get("app.key", nil, CacheOnly(true))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "from-env-file" {
		t.Errorf("Get(app.key) = %v, want the TOXIN_CONFIG value", got)
	}
	writeConfigFile(t, filepath.Dir(path), "app.json", `{"app": {"key": "edited"}}`)
	if err := Reload(false); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if got, _ := Get("app.key", nil, CacheOnly(true)); got != "edited" {
		t.Errorf("Get(app.key) after Reload = %v, want the edited value", got)
	}
}

// 20-api@toxin.Reload: both reload flavors re-read the config files from
// disk; the clearing reload also drops keys the files no longer hold.
func TestConfigFileReloadPicksUpEdits(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := writeConfigFile(t, dir, "app.json", `{"app": {"key": "v1", "gone": "x"}}`)
	c, err := New(WithConfigFile(path))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	writeConfigFile(t, dir, "app.json", `{"app": {"key": "v2", "added": "y"}}`)
	if err := c.Reload(false); err != nil {
		t.Fatalf("Reload(false): %v", err)
	}
	if got := getCached(t, c, "app.key"); got != "v2" {
		t.Errorf("after Reload(false) app.key = %v, want v2", got)
	}
	if got := getCached(t, c, "app.added"); got != "y" {
		t.Errorf("after Reload(false) app.added = %v, want y", got)
	}

	writeConfigFile(t, dir, "app.json", `{"app": {"key": "v3"}}`)
	if err := c.Reload(true); err != nil {
		t.Fatalf("Reload(true): %v", err)
	}
	if got := getCached(t, c, "app.key"); got != "v3" {
		t.Errorf("after Reload(true) app.key = %v, want v3", got)
	}
	for _, key := range []string{"app.gone", "app.added"} {
		if got := getCached(t, c, key); got != nil {
			t.Errorf("after Reload(true) %s = %v, want it dropped", key, got)
		}
	}
}

// 20-api@Config.Load and toxin.Reload: a config file that turns missing or
// malformed fails the load with "Could not read toxin config: <cause>",
// leaves the live settings intact, and runs no reload hooks.
func TestConfigFileBadFileOnReloadPreservesSettings(t *testing.T) {
	for name, reload := range map[string]func(*Config) error{
		"Reload(true)":  func(c *Config) error { return c.Reload(true) },
		"Reload(false)": func(c *Config) error { return c.Reload(false) },
		"Load":          func(c *Config) error { _, err := c.Load(); return err },
		"Load(merge false)": func(c *Config) error {
			_, err := c.Load(Merge(false))
			return err
		},
	} {
		for breakage, breakFile := range map[string]func(t *testing.T, path string){
			"malformed": func(t *testing.T, path string) {
				writeConfigFile(t, filepath.Dir(path), filepath.Base(path), `{"app": `)
			},
			"missing": func(t *testing.T, path string) {
				if err := os.Remove(path); err != nil {
					t.Fatalf("removing config file: %v", err)
				}
			},
		} {
			t.Run(name+"/"+breakage, func(t *testing.T) {
				clearConfigEnv(t)
				dir := t.TempDir()
				first := writeConfigFile(t, dir, "first.json", `{"first": "kept"}`)
				path := writeConfigFile(t, dir, "app.json", `{"app": {"key": "live"}}`)
				c, err := New(WithConfigFiles(first, path))
				if err != nil {
					t.Fatalf("New: %v", err)
				}
				if err := c.Set("set.key", "explicit"); err != nil {
					t.Fatalf("Set: %v", err)
				}
				hooks := 0
				c.AddReloadHook(func() { hooks++ })

				breakFile(t, path)
				assertConfigError(t, reload(c), path)
				for key, w := range map[string]any{
					"app.key": "live", "first": "kept", "set.key": "explicit",
				} {
					if got := getCached(t, c, key); got != w {
						t.Errorf("after failed reload %s = %v, want %v", key, got, w)
					}
				}
				if hooks != 0 {
					t.Errorf("reload hooks ran %d times after a failed reload, want 0", hooks)
				}
			})
		}
	}
}

// root@Sources and 20-api@Config.Load: explicit in-process writes after
// the last load win over config files; a supplied config does not re-read
// the files, and the next plain load restores the file value.
func TestConfigFileExplicitWritesWinUntilNextLoad(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := writeConfigFile(t, dir, "app.json", `{"app": {"key": "file", "other": "file"}}`)
	c, err := New(WithConfigFile(path))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := c.Set("app.key", "set"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := getCached(t, c, "app.key"); got != "set" {
		t.Errorf("after Set app.key = %v, want set", got)
	}

	// A malformed file does not matter to a supplied config: the files are
	// not re-read.
	writeConfigFile(t, dir, "app.json", `{"app": `)
	cache, err := c.Load(WithConfig(map[string]any{"app": map[string]any{"other": "given"}}), Merge(false))
	if err != nil {
		t.Fatalf("Load(WithConfig): %v", err)
	}
	if cache["app.other"] != "given" {
		t.Errorf("Load(WithConfig) app.other = %v, want the supplied value", cache["app.other"])
	}
	if cache["app.key"] != "file" {
		t.Errorf("Load(WithConfig, merge false) app.key = %v, want the config-file value kept beneath it", cache["app.key"])
	}

	writeConfigFile(t, dir, "app.json", `{"app": {"key": "file", "other": "file"}}`)
	if _, err := c.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := getCached(t, c, "app.other"); got != "file" {
		t.Errorf("after plain Load app.other = %v, want the file value restored", got)
	}
}

// 20-api@toxin.New: with caching disabled the config files are still read
// and validated at construction.
func TestConfigFileCacheDisabledStillValidates(t *testing.T) {
	clearConfigEnv(t)
	bad := writeConfigFile(t, t.TempDir(), "bad.json", `[1]`)
	_, err := New(WithCache(false), WithConfigFile(bad))
	assertConfigError(t, err, bad)
}

// 20-api@toxin.Reload::ERROR: a clearing reload whose cache-file read
// fails returns the error before wiping anything, keeping the previous
// settings, config-file values included, and running no hooks.
func TestConfigFileReloadCacheFileFailurePreservesSettings(t *testing.T) {
	clearConfigEnv(t)
	path := writeConfigFile(t, t.TempDir(), "app.json", `{"file": {"key": "file"}}`)
	c, err := New(WithFileCache(t.TempDir()), WithConfigFile(path))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Set("set.key", "explicit"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := c.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	before := c.cacheSnapshot()
	hooks := 0
	c.AddReloadHook(func() { hooks++ })

	if err := os.WriteFile(c.fileCache.Filename(), []byte(`{"set.key": `), 0o600); err != nil {
		t.Fatalf("corrupting the cache file: %v", err)
	}
	err = c.Reload(true)
	if err == nil || !strings.HasPrefix(err.Error(), "Could not read toxin cache: ") {
		t.Fatalf("Reload(true) = %v, want \"Could not read toxin cache: ...\"", err)
	}
	if after := c.cacheSnapshot(); !jsonEqual(t, after, before) {
		t.Errorf("cache after a failed Reload(true) = %v, want it unchanged %v", after, before)
	}
	if hooks != 0 {
		t.Errorf("hooks ran %d times after a failed reload, want 0", hooks)
	}
}
