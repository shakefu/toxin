package toxin

import (
	"strings"

	"github.com/shakefu/toxin/internal/etcd"
)

// Load loads configuration into the cache and returns the resulting cache
// map.
//
// WithConfig supplies a nested map to load, flattening it into dot-notation
// keys by recursing through nested maps only: a slice stays a single
// value, a top-level key that already contains a dot is kept as-is, and a
// nested empty map is stored as an empty map value. Without one, the
// configuration comes from a configured file cache when its file exists,
// and otherwise from etcd under the prefix.
//
// The per-call options default to cacheOnly true, merge true,
// allowInherited true and fileCacheOnly false. A cache file that was read
// populates the cache without contacting etcd, clearing the cache or
// saving the cache file. Otherwise, with fileCacheOnly unset, a merge of
// false clears the cache before loading, the loaded keys are merged into
// the cache, cacheOnly false writes every loaded key to etcd — merged over
// what exists rather than replacing it — and a configured file cache is
// saved after loading. FileCacheOnly never contacts etcd. The registered
// reload hooks run after the load.
func (c *Config) Load(opts ...CallOption) (map[string]any, error) {
	o := loadDefaults(opts)

	var flat map[string]any
	if o.hasConfig {
		flat = flattenConfig(o.config)
	}

	// A configured file cache stands in for etcd when no config was given;
	// fileCacheOnly never contacts etcd at all.
	if o.fileCacheOnly || (flat == nil && c.fileCache != nil) {
		if c.fileCache != nil {
			read, err := c.readFileCache()
			if err != nil {
				return nil, err
			}
			if read {
				c.runHooks()
				return c.cacheSnapshot(), nil
			}
		}
		if o.fileCacheOnly {
			c.runHooks()
			return c.cacheSnapshot(), nil
		}
	}

	if flat == nil {
		m, err := c.Dump(AllowInherited(o.allowInherited))
		if err != nil {
			if !etcd.IsNotFound(err) {
				return nil, err
			}
			// The namespace holds nothing: an empty cache without error.
			m = map[string]any{}
		}
		flat = m
	} else if !c.caseSensitive {
		lowered := make(map[string]any, len(flat))
		for k, v := range flat {
			lowered[strings.ToLower(k)] = v
		}
		flat = lowered
	}

	if !o.merge {
		c.emptyCache()
	}
	c.mergeIntoCache(flat)

	if !o.cacheOnly {
		for k, v := range flat {
			if err := c.Set(k, v); err != nil {
				return nil, err
			}
		}
	}

	if c.fileCache != nil {
		if err := c.fileCache.Save(c.cacheSnapshot()); err != nil {
			return nil, err
		}
	}

	c.runHooks()
	return c.cacheSnapshot(), nil
}

// readFileCache fills the cache from the cache file, only for keys not
// already present, reporting whether cached data was read.
func (c *Config) readFileCache() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fileCache.Load(c.cache)
}

// mergeIntoCache merges the loaded keys into the cache.
func (c *Config) mergeIntoCache(loaded map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range loaded {
		c.cache[k] = v
	}
}

// Reload loads the configuration anew; with clear set the cached settings
// are wiped before loading.
func (c *Config) Reload(clear bool) error {
	if clear {
		c.emptyCache()
	}
	_, err := c.Load()
	return err
}

// AddReloadHook registers a hook that runs, bare, after every subsequent
// load. Hooks run in registration order and stay registered for the
// process lifetime.
func (c *Config) AddReloadHook(hook func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hooks = append(c.hooks, hook)
}

// runHooks calls the registered reload hooks in registration order.
func (c *Config) runHooks() {
	c.mu.Lock()
	hooks := make([]func(), len(c.hooks))
	copy(hooks, c.hooks)
	c.mu.Unlock()
	for _, hook := range hooks {
		hook()
	}
}

// flattenConfig flattens a nested map into dot-notation keys by recursing
// through nested maps only.
func flattenConfig(data map[string]any) map[string]any {
	out := map[string]any{}
	flattenInto(out, data, "")
	return out
}

// flattenInto recurses through cur, prefixing nested keys with prop in dot
// notation. A non-map value is a leaf; a top-level key that already
// contains a dot is kept as-is with its whole value; an empty nested map
// is stored as an empty map value.
func flattenInto(out map[string]any, cur map[string]any, prop string) {
	if len(cur) == 0 {
		out[prop] = map[string]any{}
		return
	}
	for k, v := range cur {
		if prop == "" && hasInnerDot(k) {
			out[k] = v
			continue
		}
		name := k
		if prop != "" {
			name = prop + "." + k
		}
		if child, ok := v.(map[string]any); ok {
			flattenInto(out, child, name)
			continue
		}
		out[name] = v
	}
}

// hasInnerDot reports whether s contains a dot with at least one character
// on each side, the shape of an already flattened dot-notation key.
func hasInnerDot(s string) bool {
	i := strings.Index(s, ".")
	return i > 0 && i < len(s)-1
}
