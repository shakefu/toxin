package toxin

import (
	"context"
	"strings"

	"github.com/shakefu/toxin/internal/etcd"
)

// Watch starts the watcher that keeps the cache synchronized with etcd
// changes under the prefix. At most one watcher runs per config; with
// caching disabled watching is skipped with a warning. The watcher stops
// when the config is closed.
func (c *Config) Watch() {
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
				c.storeCached(key, etcd.DecodeValue(ev.Value))
			}
		case etcd.EventDelete:
			if key, ok := c.stripKey(ev.Key); ok {
				if !c.caseSensitive {
					key = strings.ToLower(key)
				}
				c.dropCached(key)
			}
		case etcd.EventDeleteDir:
			// A directory delete names the watched root, covering the
			// whole namespace.
			if c.allowClear {
				c.emptyCache()
			}
		case etcd.EventReconnect:
			// A signal, not a data event.
		}
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
