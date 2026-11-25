package toxin

import (
	"context"

	"github.com/shakefu/toxin/internal/etcd"
)

// Set stores value under the setting key. With etcd configured, a string
// value is stored verbatim and any other JSON-encodable value as its JSON
// encoding; the key is lowercased and written under the prefix unless case
// sensitivity is enabled. With cacheOnly or no configured etcd, the cache
// alone is written — and nothing at all when caching is disabled. Otherwise
// etcd is written first and the cache updated only after the write succeeds.
// WithTTL grants an etcd key a lease that expires it that many seconds after
// the write; it has no effect without etcd.
func (c *Config) Set(key string, value any, opts ...CallOption) error {
	o := setDefaults(opts)
	k := c.coerceKey(key)

	if o.cacheOnly || c.client == nil {
		if c.cacheOn {
			c.storeCached(k, value)
		}
		return nil
	}

	encoded, err := etcd.EncodeValue(value)
	if err != nil {
		return err
	}
	if _, err := c.client.Put(context.Background(), c.etcdKey(k), encoded, o.ttl); err != nil {
		return err
	}
	if c.cacheOn {
		c.storeCached(k, value)
	}
	return nil
}
