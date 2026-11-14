package toxin

import (
	"context"
	"errors"
	"fmt"

	"github.com/shakefu/toxin/internal/etcd"
)

// ErrNoSetting is wrapped by the strict variant's unset-key error; the
// rendered message stays 'No setting "<key>"'. Match it with errors.Is.
var ErrNoSetting = errors.New("No setting")

// Get returns the value of the setting key, or def when the key is unset.
// A nil def means no default: an unset key yields nil rather than an
// error, because an absent key is a value outcome. Per-call options tune
// the cache consultation (cached), whether the result is cached
// (cacheResult), whether etcd is contacted at all (cacheOnly) and whether
// inheritance is consulted (allowInherited).
func (c *Config) Get(key string, def any, opts ...CallOption) (any, error) {
	o := getDefaults(opts)
	k := c.coerceKey(key)
	v, found, err := c.lookup(k, o)
	if err != nil {
		return nil, err
	}
	if found {
		return v, nil
	}
	// A cacheOnly miss returns the default without touching anything
	// else, as jetconfig did; otherwise the default is stored for later
	// calls unless cacheResult is false.
	if def != nil && c.cacheOn && o.cacheResult && !o.cacheOnly {
		c.storeCached(k, def)
	}
	return def, nil
}

// GetStrict returns the value of the setting key, rejecting an unset key
// with the error 'No setting "<key>"' rather than falling back to a
// default. It takes the same per-call options as Get.
func (c *Config) GetStrict(key string, opts ...CallOption) (any, error) {
	o := getDefaults(opts)
	k := c.coerceKey(key)
	v, found, err := c.lookup(k, o)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w %q", ErrNoSetting, k)
	}
	return v, nil
}

// lookup resolves a coerced setting key to a value: the cache when
// allowed, then etcd, then the inherited chain when allowed. It reports
// whether a value was found; an unset key is not an error.
func (c *Config) lookup(key string, o callConfig) (any, bool, error) {
	if c.cacheOn && o.cached {
		if v, ok := c.cachedValue(key); ok {
			return v, true, nil
		}
	}
	if o.cacheOnly {
		// A cacheOnly call never contacts etcd, not even to consult
		// inheritance — not when caching is disabled either, by
		// construction or for the call.
		return nil, false, nil
	}

	v, found, err := c.etcdGet(key)
	if err != nil {
		return nil, false, err
	}
	if found {
		if c.cacheOn && o.cacheResult {
			c.storeCached(key, v)
		}
		return v, true, nil
	}

	if o.allowInherited && c.inherit {
		if p := c.parentConfig(); p != nil {
			// The parent consults its own cache and its own inheritance;
			// cacheOnly never reaches the parent.
			po := callConfig{cached: o.cached, cacheResult: o.cacheResult, allowInherited: true}
			pv, pFound, pErr := p.lookup(key, po)
			switch {
			case pErr != nil:
				// An inherited lookup that fails falls back to the
				// default, as jetconfig did.
				c.log.Warnf("Error getting inherited key: %v", pErr)
			case pFound:
				if c.cacheOn && o.cacheResult {
					c.storeCached(key, pv)
				}
				return pv, true, nil
			}
		}
	}
	return nil, false, nil
}

// etcdGet reads the exact value stored under a setting key, without the
// cache and without inheritance. A response that carries no entry for the
// key reports the key as unset, with a warning.
func (c *Config) etcdGet(key string) (any, bool, error) {
	addr := etcd.TrimKey(c.etcdKey(key))
	entries, err := c.client.Read(context.Background(), addr, false)
	if err != nil {
		if etcd.IsNotFound(err) {
			c.log.Warnf("Key not found: %s", addr)
			return nil, false, nil
		}
		return nil, false, err
	}
	for _, e := range entries {
		if e.Key == addr && !e.Dir {
			return etcd.DecodeValue(e.Value), true, nil
		}
	}
	// The key holds no value of its own; it is a directory name.
	c.log.Warnf("Key not found: %s", addr)
	return nil, false, nil
}
