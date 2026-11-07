package toxin

import (
	"context"

	"github.com/shakefu/toxin/internal/etcd"
)

// Set stores value under the setting key. A string value is stored
// verbatim and any other JSON-encodable value as its JSON encoding; the
// key is lowercased and written under the prefix unless case sensitivity
// is enabled. With cacheOnly the cache alone is written — and nothing at
// all when caching is disabled; otherwise etcd is written first and the
// cache updated only after the write succeeds. WithTTL grants the key a
// lease that expires it that many seconds after the write.
func (c *Config) Set(key string, value any, opts ...CallOption) error {
	o := setDefaults(opts)
	k := c.coerceKey(key)

	if o.cacheOnly {
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
