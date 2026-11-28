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
// otherwise from etcd under the prefix, or from an empty external source
// when etcd is not configured, and then every configured config file is
// read anew from disk and overlaid on it, a later file winning per key.
//
// The per-call options default to cacheOnly true, merge true,
// allowInherited true and fileCacheOnly false. A cache file that was read
// populates the cache without contacting etcd, clearing the cache or
// saving the cache file; the config files still overlay it. Otherwise,
// with fileCacheOnly unset, a merge of false clears the cache before
// loading, the loaded keys are merged into the cache, cacheOnly false
// writes every loaded key, config-file keys included, to configured etcd —
// merged over what exists rather than replacing it — and a configured file
// cache is saved after loading. FileCacheOnly never contacts etcd. Config
// files are never written. The registered reload hooks run after the load.
//
// Every source is read before the cache is touched: a config file that
// cannot be read fails the load with "Could not read toxin config:
// <cause>", leaves the live settings intact and runs no reload hooks. A
// failure to save the cache file is different: it is returned after the
// loaded settings were installed and before the reload hooks run.
func (c *Config) Load(opts ...CallOption) (map[string]any, error) {
	return c.load(loadDefaults(opts), false)
}

// load performs Load; with clear set the cache is wiped as the loaded
// sources are installed, after every source was read successfully.
func (c *Config) load(o callConfig, clear bool) (map[string]any, error) {
	var flat, files map[string]any
	// A supplied config is an explicit in-process write: it does not
	// re-read the config files.
	reread := !o.hasConfig
	if o.hasConfig {
		flat = flattenConfig(o.config)
	} else {
		var err error
		if files, err = c.readConfigFiles(); err != nil {
			return nil, err
		}
	}

	// A configured file cache stands in for etcd when no config was given;
	// fileCacheOnly never contacts etcd at all.
	if o.fileCacheOnly || (flat == nil && c.fileCache != nil) {
		if c.fileCache != nil {
			cached := map[string]any{}
			read, err := c.fileCache.Load(cached)
			if err != nil {
				return nil, err
			}
			if read {
				c.install(clear, true, cached, reread, files)
				c.runHooks()
				return c.cacheSnapshot(), nil
			}
		}
		if o.fileCacheOnly {
			c.install(clear, false, nil, reread, files)
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

	c.install(clear || !o.merge, false, flat, reread, files)

	if !o.cacheOnly && c.client != nil {
		// Config-file values reach etcd only on an explicit write-through
		// load. A re-read file wins over the loaded namespace, as it does in
		// the cache; a supplied config wins over the active file values.
		written := map[string]any{}
		if reread {
			for k, v := range flat {
				written[k] = v
			}
			for k, v := range files {
				written[k] = v
			}
		} else {
			for k, v := range c.fileOverridesSnapshot() {
				written[k] = v
			}
			for k, v := range flat {
				written[k] = v
			}
		}
		for k, v := range written {
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

// install applies one load to the cache under a single lock. With clear
// the cache is emptied first, keeping the active config-file overrides when
// the files are not re-read, since a supplied config outranks them only per
// key. The loaded source map is merged in, only into absent keys when
// fillAbsent (the file cache's rule, so fresh values win). With reread the
// config files overlay the result and become the active overrides.
func (c *Config) install(clear, fillAbsent bool, loaded map[string]any, reread bool, files map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if clear {
		c.cache = map[string]any{}
		if !reread {
			for k, v := range c.fileOverrides {
				c.cache[k] = v
			}
		}
	}
	for k, v := range loaded {
		if fillAbsent {
			if _, ok := c.cache[k]; ok {
				continue
			}
		}
		c.cache[k] = v
	}
	if reread {
		for k, v := range files {
			c.cache[k] = v
		}
		c.fileOverrides = files
	}
}

// readFileCache fills the cache from the cache file, only for keys not
// already present, reporting whether cached data was read.
func (c *Config) readFileCache() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fileCache.Load(c.cache)
}

// Reload loads the configuration anew; with clear set the cached settings
// are wiped before loading. Every source, the config files included, is
// read before anything is wiped, so a failed reload leaves the previous
// settings intact and runs no reload hooks.
func (c *Config) Reload(clear bool) error {
	_, err := c.load(loadDefaults(nil), clear)
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
