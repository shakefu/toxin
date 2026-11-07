package toxin

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakefu/toxin/internal/testutil"
)

// 20-api@toxin.New::ERROR: a log level that is not one of silly, debug,
// info, warn or critical is rejected with the error
// "Unknown log level '<level>'".
func TestNewRejectsUnknownLogLevel(t *testing.T) {
	clearToxinEnv(t)
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))

	_, err := New(WithLogLevel("verbose"))
	if err == nil {
		t.Fatal("New accepted an unknown log level, want error")
	}
	if err.Error() != "Unknown log level 'verbose'" {
		t.Errorf("error = %q, want %q", err.Error(), "Unknown log level 'verbose'")
	}
}

// 20-api@toxin.New::INPUT/ERROR: TOXIN_LOGLEVEL overrides the log level
// option, so an invalid value there is the one the error names.
func TestNewTOXINLOGLEVELOverridesOptionAndValidates(t *testing.T) {
	clearToxinEnv(t)
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))
	t.Setenv("TOXIN_LOGLEVEL", "loud")

	_, err := New(WithLogLevel("info"))
	if err == nil {
		t.Fatal("New accepted an invalid TOXIN_LOGLEVEL over a valid option, want error")
	}
	if err.Error() != "Unknown log level 'loud'" {
		t.Errorf("error = %q, want %q", err.Error(), "Unknown log level 'loud'")
	}
}

// 20-api@toxin.New::ERROR: a negative inherit depth is rejected with
// "inheritDepth must be an integer greater than or equal to 0".
func TestNewRejectsNegativeInheritDepth(t *testing.T) {
	clearToxinEnv(t)
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))

	_, err := New(WithInheritDepth(-1))
	if err == nil {
		t.Fatal("New accepted a negative inherit depth, want error")
	}
	want := "inheritDepth must be an integer greater than or equal to 0"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// 20-api@toxin.New::ERROR: a consulted TOXIN_INHERIT_DEPTH holding other
// than a non-negative integer is rejected with
// 'invalid TOXIN_INHERIT_DEPTH "<value>"'.
func TestNewRejectsInvalidTOXINInheritDepth(t *testing.T) {
	clearToxinEnv(t)
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))

	for _, value := range []string{"abc", "-1", "2.5", "two"} {
		t.Setenv("TOXIN_INHERIT_DEPTH", value)
		_, err := New()
		if err == nil {
			t.Fatalf("New accepted TOXIN_INHERIT_DEPTH=%q, want error", value)
		}
		want := `invalid TOXIN_INHERIT_DEPTH "` + value + `"`
		if err.Error() != want {
			t.Errorf("TOXIN_INHERIT_DEPTH=%q error = %q, want %q", value, err.Error(), want)
		}
	}
}

// 20-api@toxin.New::INPUT: TOXIN_INHERIT_DEPTH is consulted only when the
// corresponding option is not supplied, so an invalid value there must
// not reject a supplied option.
func TestNewSkipsTOXINInheritDepthWhenOptionSupplied(t *testing.T) {
	clearToxinEnv(t)
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))
	t.Setenv("TOXIN_INHERIT_DEPTH", "not a number")

	c, err := New(WithInheritDepth(1))
	if err != nil {
		t.Fatalf("New with a supplied depth rejected over TOXIN_INHERIT_DEPTH: %v", err)
	}
	if c.inheritDepth != 1 {
		t.Errorf("inheritDepth = %d, want 1", c.inheritDepth)
	}
}

// 20-api@toxin.New::ERROR: a file cache directory that cannot be created
// rejects construction with "Could not open toxin cache: <cause>".
func TestNewRejectsUnopenableFileCacheDirectory(t *testing.T) {
	clearToxinEnv(t)
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))

	// A plain file blocks the directory creation.
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, nil, 0o644); err != nil {
		t.Fatalf("blocking the cache directory: %v", err)
	}

	_, err := New(WithFileCache(blocked))
	if err == nil {
		t.Fatal("New accepted an unopenable cache directory, want error")
	}
	if !strings.HasPrefix(err.Error(), "Could not open toxin cache: ") {
		t.Fatalf("error = %q, want prefix %q", err.Error(), "Could not open toxin cache: ")
	}
	var cause *fs.PathError
	if !errors.As(err, &cause) {
		t.Errorf("error %q does not wrap the underlying cause", err)
	}
}

// 10-etcd3@etcd.Client::ERROR, bound by toxin.New's whole-spec CONFORMS: a
// member matching none of the accepted forms is rejected with an error
// naming the member.
func TestNewRejectsInvalidEtcdMember(t *testing.T) {
	clearToxinEnv(t)

	_, err := New(WithHosts("not a member"))
	if err == nil {
		t.Fatal("New accepted an invalid etcd member, want error")
	}
	if !strings.Contains(err.Error(), "not a member") {
		t.Errorf("error = %q, want it to name the member %q", err.Error(), "not a member")
	}
}

// 20-api@Config.Get::RETURN: the strict variant finds the key unset and
// returns the error 'No setting "<key>"'.
func TestGetStrictUnsetKeyErrorsNoSetting(t *testing.T) {
	c := newTestConfig(t, WithPrefix("err-strict/"))

	_, err := c.GetStrict("app.missing")
	if err == nil {
		t.Fatal("GetStrict accepted an unset key, want error")
	}
	if err.Error() != `No setting "app.missing"` {
		t.Errorf("error = %q, want %q", err.Error(), `No setting "app.missing"`)
	}
}

// 20-api@Config.Dump::ERROR: no entry under the prefix, with the prefix
// not the root, returns the not-found error rendered as
// 'Key not found: /<prefix>'.
func TestDumpNotFoundRendersKeyNotFound(t *testing.T) {
	c := newTestConfig(t, WithPrefix("err-empty-ns/"))

	_, err := c.Dump()
	if err == nil {
		t.Fatal("Dump of an empty non-root namespace succeeded, want error")
	}
	if err.Error() != "Key not found: /err-empty-ns" {
		t.Errorf("error = %q, want %q", err.Error(), "Key not found: /err-empty-ns")
	}
}

// 20-api@Config.Clear::ERROR: Clear with the allowClear option off
// returns the error "clear() is not allowed on this instance".
func TestClearNotAllowedWithoutAllowClear(t *testing.T) {
	c := newTestConfig(t, WithPrefix("err-noclear/"), WithAllowClear(false))

	err := c.Clear()
	if err == nil {
		t.Fatal("Clear allowed without the allowClear option, want error")
	}
	if err.Error() != "clear() is not allowed on this instance" {
		t.Errorf("error = %q, want %q", err.Error(), "clear() is not allowed on this instance")
	}
}

// 20-api@toxin.FileCache::ERROR, surfaced through Config.Load: when the
// cache file cannot be written on the save after loading, Load fails with
// "Could not write toxin cache: <cause>".
func TestLoadSaveCacheFileFailureSurfacesCouldNotWrite(t *testing.T) {
	dir := t.TempDir()
	clearToxinEnv(t)
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))
	t.Setenv("TOXIN_CACHE", dir)

	c, err := New(WithPrefix("err-cache-write/"))
	if err != nil {
		t.Fatalf("toxin.New: %v", err)
	}

	// A directory at the cache file's path cannot be written as a file.
	if err := os.MkdirAll(c.fileCache.Filename(), 0o755); err != nil {
		t.Fatalf("blocking the cache file path: %v", err)
	}

	// A supplied config skips the cache file read and reaches the save.
	_, err = c.Load(WithConfig(map[string]any{"key": "value"}))
	if err == nil {
		t.Fatal("Load succeeded against an unwritable cache file, want error")
	}
	if !strings.HasPrefix(err.Error(), "Could not write toxin cache: ") {
		t.Errorf("error = %q, want prefix %q", err.Error(), "Could not write toxin cache: ")
	}
}
