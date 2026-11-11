package toxin

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 20-api@toxin.FileCache::INPUT: the cache file is named from the
// prefix with leading and trailing slashes trimmed, ".." collapsed to
// "-", and a ".json" extension, keeping interior prefix slashes as
// directory separators nested under the configured cache directory.
func TestFileCacheNamesCacheFileFromPrefix(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		prefix string
		want   string
	}{
		{"config/", filepath.Join(dir, "config.json")},
		{"/config/", filepath.Join(dir, "config.json")},
		{"app/db/", filepath.Join(dir, "app", "db.json")},
		{"config/../app/", filepath.Join(dir, "config", "-", "app.json")},
		{"/", filepath.Join(dir, ".json")},
	}
	for _, tc := range cases {
		fc := NewFileCache(tc.prefix, dir)
		if got := fc.Filename(); got != tc.want {
			t.Errorf("NewFileCache(%q).Filename() = %q, want %q", tc.prefix, got, tc.want)
		}
		if !strings.HasSuffix(tc.want, ".json") {
			t.Errorf("cache file %q lacks .json extension", tc.want)
		}
	}
}

// 20-api@toxin.FileCache::SIDE-EFFECT: saving creates the nested prefix
// directories under the cache directory.
func TestFileCacheSaveCreatesNestedPrefixDirectories(t *testing.T) {
	dir := t.TempDir()
	fc := NewFileCache("app/db/", dir)

	if err := fc.Save(map[string]any{"app.db.host": "db.example.com"}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "app", "db.json")); err != nil {
		t.Errorf("cache file missing after save: %v", err)
	}
}

// 20-api@toxin.FileCache::SIDE-EFFECT: the cache map is written as JSON
// on save.
func TestFileCacheSaveWritesCacheMapAsJSON(t *testing.T) {
	dir := t.TempDir()
	fc := NewFileCache("config/", dir)
	cache := map[string]any{
		"app.setting.str": "value",
		"app.setting.num": float64(7),
	}

	if err := fc.Save(cache); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	data, err := os.ReadFile(fc.Filename())
	if err != nil {
		t.Fatalf("reading saved cache: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("saved cache is not JSON (%q): %v", data, err)
	}
	for key, want := range cache {
		if got[key] != want {
			t.Errorf("saved cache[%q] = %v, want %v", key, got[key], want)
		}
	}
}

// 20-api@toxin.FileCache::SIDE-EFFECT: loading fills cache entries only
// for keys not already present, so freshly loaded values win.
func TestFileCacheLoadFillsOnlyKeysNotAlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	fc := NewFileCache("config/", dir)
	if err := fc.Save(map[string]any{
		"shared.key": "from-file",
		"file.key":   "from-file",
	}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	dst := map[string]any{"shared.key": "fresh"}
	found, err := fc.Load(dst)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !found {
		t.Error("Load reported no cached data for an existing cache file")
	}
	if dst["shared.key"] != "fresh" {
		t.Errorf("already-present key overwritten: shared.key = %v, want %q", dst["shared.key"], "fresh")
	}
	if dst["file.key"] != "from-file" {
		t.Errorf("missing key not filled: file.key = %v, want %q", dst["file.key"], "from-file")
	}
}

// 20-api@toxin.FileCache::ERROR: when the cache file cannot be written
// on save, Save fails with the error
// "Could not write toxin cache: <cause>".
func TestFileCacheSaveFailsWithCouldNotWriteToxinCache(t *testing.T) {
	dir := t.TempDir()
	fc := NewFileCache("config/", dir)
	// A directory at the cache file's path cannot be written as a file.
	if err := os.MkdirAll(fc.Filename(), 0o755); err != nil {
		t.Fatalf("blocking the cache file path: %v", err)
	}

	err := fc.Save(map[string]any{"key": "value"})
	if err == nil {
		t.Fatal("Save succeeded against an unwritable cache file, want error")
	}
	var cause *fs.PathError
	if !errors.As(err, &cause) {
		t.Fatalf("error %q does not wrap the underlying cause", err)
	}
	want := "Could not write toxin cache: " + cause.Error()
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// 20-api@toxin.FileCache::ERROR: when the cache file exists but cannot
// be read, Load fails with the error
// "Could not read toxin cache: <cause>".
func TestFileCacheLoadFailsWithCouldNotReadToxinCache(t *testing.T) {
	dir := t.TempDir()
	fc := NewFileCache("config/", dir)
	// A directory at the cache file's path exists but cannot be read as
	// a cache file.
	if err := os.MkdirAll(fc.Filename(), 0o755); err != nil {
		t.Fatalf("blocking the cache file path: %v", err)
	}

	_, err := fc.Load(map[string]any{})
	if err == nil {
		t.Fatal("Load succeeded against an unreadable cache file, want error")
	}
	var cause *fs.PathError
	if !errors.As(err, &cause) {
		t.Fatalf("error %q does not wrap the underlying cause", err)
	}
	want := "Could not read toxin cache: " + cause.Error()
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// 20-api@toxin.FileCache::ERROR: a cache file whose contents cannot be
// read as the cache's JSON is also a read failure.
func TestFileCacheLoadFailsWithCouldNotReadToxinCacheOnInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	fc := NewFileCache("config/", dir)
	if err := os.WriteFile(fc.Filename(), []byte("not json"), 0o644); err != nil {
		t.Fatalf("writing bad cache file: %v", err)
	}

	_, err := fc.Load(map[string]any{})
	if err == nil {
		t.Fatal("Load succeeded against a non-JSON cache file, want error")
	}
	if !strings.HasPrefix(err.Error(), "Could not read toxin cache: ") {
		t.Errorf("error = %q, want prefix %q", err.Error(), "Could not read toxin cache: ")
	}
}

// 20-api@toxin.FileCache::ERROR: when no cache file exists, Load
// reports no cached data without error.
func TestFileCacheLoadReportsNoCachedDataWithoutErrorWhenNoFileExists(t *testing.T) {
	dir := t.TempDir()
	fc := NewFileCache("config/", dir)
	dst := map[string]any{"existing": "kept"}

	found, err := fc.Load(dst)
	if err != nil {
		t.Fatalf("Load failed with no cache file: %v", err)
	}
	if found {
		t.Error("Load reported cached data with no cache file")
	}
	if dst["existing"] != "kept" {
		t.Errorf("dst changed with no cache file: %v", dst)
	}
}
