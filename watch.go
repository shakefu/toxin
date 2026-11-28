package toxin

import (
	"context"
	"strings"

	"github.com/shakefu/toxin/internal/etcd"
)

// Watch starts the watcher that keeps the cache synchronized with etcd
// changes under the prefix. Events for a key a config file supplies leave
// the config-file value in place. At most one watcher runs per config; with
// caching disabled watching is skipped with a warning. Without configured
// etcd, watching is skipped silently. The watcher stops when the config is
// closed.
func (c *Config) Watch() {
	if c.client == nil {
		return
	}
	if !c.cacheOn {
		c.log.Warnf("Cache not enabled, skipping watch...")
		return
	}
	c.watchOnce.Do(func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.watcher = c.client.Watch(context.Background(), c.prefix)
		go c.watchLoop(c.watcher)
	})
}

// watchLoop applies watch events to the cache until the watcher stops.
func (c *Config) watchLoop(w *etcd.Watcher) {
	for ev := range w.Events() {
		switch ev.Kind {
		case etcd.EventSet:
			if key, ok := c.stripKey(ev.Key); ok {
				if !c.caseSensitive {
					key = strings.ToLower(key)
				}
				c.storeWatched(key, etcd.DecodeValue(ev.Value))
			}
		case etcd.EventDelete:
			if key, ok := c.stripKey(ev.Key); ok {
				if !c.caseSensitive {
					key = strings.ToLower(key)
				}
				c.dropWatched(key)
			}
		case etcd.EventDeleteDir:
			// A directory delete names the watched root, covering the
			// whole namespace.
			if c.allowClear {
				c.emptyCacheKeepingFiles()
			}
		case etcd.EventReconnect:
			// A signal, not a data event.
		}
	}
}

// storeWatched stores a watched value unless a config file supplies the
// key: config files outrank the etcd namespace.
func (c *Config) storeWatched(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.isFileOverride(key) {
		c.cache[key] = value
	}
}

// dropWatched drops a watched key unless a config file supplies it.
func (c *Config) dropWatched(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.isFileOverride(key) {
		delete(c.cache, key)
	}
}

// emptyCacheKeepingFiles empties the cache, then restores the active
// config-file values, which no etcd delete removes.
func (c *Config) emptyCacheKeepingFiles() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]any, len(c.fileOverrides))
	for k, v := range c.fileOverrides {
		c.cache[k] = v
	}
}

// Close stops the config's watcher without closing the cluster connection
// the config shares with its relatives. It succeeds when called twice.
func (c *Config) Close() error {
	c.mu.Lock()
	w := c.watcher
	c.mu.Unlock()
	if w != nil {
		w.Stop()
	}
	return nil
}
