package toxin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shakefu/toxin/internal/testutil"
)

// 20-api@toxin.Default::RETURN: one process-wide Config shared by every
// caller, constructed and loaded on first use rather than at package
// initialization; the package-level Get, Set, GetStrict and Clear, reload
// and reload hook registration operate on the shared instance.
func TestDefaultSharedInstanceAndPackageShortcuts(t *testing.T) {
	isolateDefault(t)
	cacheDir := t.TempDir()
	clearToxinEnv(t)
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))
	t.Setenv("TOXIN_PREFIX", "default-test/")
	t.Setenv("TOXIN_CACHE", cacheDir)

	first, err := Default()
	if err != nil {
		t.Fatalf("Default on first use: %v", err)
	}
	if first == nil {
		t.Fatal("Default returned no config on first use")
	}
	if first.prefix != "default-test/" {
		t.Errorf("default prefix = %q, want it from TOXIN_PREFIX", first.prefix)
	}

	second, err := Default()
	if err != nil {
		t.Fatalf("Default on second use: %v", err)
	}
	if first != second {
		t.Error("Default returned different configs, want one shared instance")
	}

	t.Run("loads on first use", func(t *testing.T) {
		// The load on first use saved the (empty) cache file.
		if _, err := os.Stat(filepath.Join(cacheDir, "default-test.json")); err != nil {
			t.Errorf("no cache file saved by the first-use load: %v", err)
		}
	})

	t.Run("package Set and Get operate on the shared instance", func(t *testing.T) {
		if err := Set("app.key", "shared"); err != nil {
			t.Fatalf("package Set: %v", err)
		}
		got, err := Get("app.key", nil)
		if err != nil {
			t.Fatalf("package Get: %v", err)
		}
		if got != "shared" {
			t.Errorf("package Get = %v, want %q through the shared instance", got, "shared")
		}
	})

	t.Run("package GetStrict", func(t *testing.T) {
		got, err := GetStrict("app.key")
		if err != nil {
			t.Fatalf("package GetStrict: %v", err)
		}
		if got != "shared" {
			t.Errorf("package GetStrict = %v, want %q", got, "shared")
		}
		if _, err := GetStrict("missing.key"); err == nil {
			t.Error("package GetStrict accepted an unset key")
		} else if err.Error() != `No setting "missing.key"` {
			t.Errorf("package GetStrict error = %q, want %q", err.Error(), `No setting "missing.key"`)
		}
	})

	t.Run("package Clear honors the clear guard on the shared instance", func(t *testing.T) {
		err := Clear()
		if err == nil {
			t.Fatal("package Clear allowed the default instance, want the guard error")
		}
		if err.Error() != "clear() is not allowed on this instance" {
			t.Errorf("package Clear error = %q, want the shared instance's guard error", err.Error())
		}
	})

	t.Run("package ReloadHook and Reload run in registration order", func(t *testing.T) {
		var order []string
		ReloadHook(func() { order = append(order, "first") })
		ReloadHook(func() { order = append(order, "second") })

		if err := Reload(false); err != nil {
			t.Fatalf("package Reload: %v", err)
		}
		if len(order) != 2 || order[0] != "first" || order[1] != "second" {
			t.Errorf("hook order = %v, want [first second] on the shared instance", order)
		}

		// The reload reloaded the shared instance's namespace from etcd:
		// with the cache file out of the way and the in-memory cache
		// cleared directly, the value written through package Set comes
		// back.
		first.emptyCache()
		if err := os.Remove(filepath.Join(cacheDir, "default-test.json")); err != nil {
			t.Fatalf("removing the cache file so the reload reads etcd: %v", err)
		}
		if err := Reload(false); err != nil {
			t.Fatalf("package Reload: %v", err)
		}
		got, err := Get("app.key", nil, CacheOnly(true))
		if err != nil {
			t.Fatalf("package Get: %v", err)
		}
		if got != "shared" {
			t.Errorf("reload left the shared instance without its value: %v", got)
		}
	})
}

// 20-api@toxin.Default::RETURN: without etcd the first-use load succeeds and
// the package shortcuts and reloads operate on in-process settings.
func TestDefaultWithoutEtcdUsesInProcessShortcuts(t *testing.T) {
	isolateDefault(t)
	clearToxinEnv(t)

	first, err := Default()
	if err != nil {
		t.Fatalf("Default without etcd: %v", err)
	}
	if first.client != nil {
		t.Fatal("Default without etcd created a client")
	}
	second, err := Default()
	if err != nil || second != first {
		t.Errorf("second Default = (%p, %v), want the shared %p", second, err, first)
	}
	if err := Set("app.key", "local"); err != nil {
		t.Fatalf("package Set without etcd: %v", err)
	}
	got, err := GetStrict("app.key")
	if err != nil || got != "local" {
		t.Errorf("package GetStrict without etcd = (%v, %v), want (local, nil)", got, err)
	}

	hooks := 0
	ReloadHook(func() { hooks++ })
	if err := Reload(false); err != nil {
		t.Fatalf("Reload(false) without etcd: %v", err)
	}
	got, err = Get("app.key", nil)
	if err != nil || got != "local" {
		t.Errorf("Reload(false) discarded the package setting: (%v, %v)", got, err)
	}
	if err := Reload(true); err != nil {
		t.Fatalf("Reload(true) without etcd: %v", err)
	}
	got, err = Get("app.key", "fallback")
	if err != nil || got != "fallback" {
		t.Errorf("Reload(true) left (%v, %v), want the fallback", got, err)
	}
	if hooks != 2 {
		t.Errorf("package reload hooks ran %d times, want 2", hooks)
	}
}

// isolateDefault gives each Default test a fresh process-wide instance and
// restores the prior state afterward. These tests must not run in parallel.
func isolateDefault(t *testing.T) {
	t.Helper()
	defaultMu.Lock()
	savedConf, savedErr := defaultConf, defaultErr
	defaultConf, defaultErr = nil, nil
	defaultMu.Unlock()
	t.Cleanup(func() {
		defaultMu.Lock()
		defaultConf, defaultErr = savedConf, savedErr
		defaultMu.Unlock()
	})
}
