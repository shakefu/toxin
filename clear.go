package toxin

import (
	"context"
	"errors"

	"github.com/shakefu/toxin/internal/etcd"
)

// Clear recursively deletes the prefix namespace from etcd and empties the
// cache. Deleting the etcd namespace requires WithAllowClear(true); without
// it Clear fails with "clear() is not allowed on this instance". CacheOnly(true)
// and instances without etcd always just empty the cache.
//
// With cacheOnly the cache alone is emptied and etcd is untouched. A
// namespace that is already empty succeeds without error and leaves the
// cache as it is; the cache is emptied when cacheOnly is set or the
// namespace delete removed entries. Without configured etcd, Clear empties
// the cache without a namespace delete, regardless of allowClear.
func (c *Config) Clear(opts ...CallOption) error {
	o := clearDefaults(opts)

	if o.cacheOnly {
		c.emptyCache()
		return nil
	}

	if c.client == nil {
		c.emptyCache()
		return nil
	}
	if !c.allowClear {
		return errors.New("clear() is not allowed on this instance")
	}

	_, count, err := c.client.Delete(context.Background(), c.prefix, true)
	if err != nil {
		if etcd.IsNotFound(err) {
			// The namespace is already empty.
			return nil
		}
		return err
	}
	if count > 0 {
		c.emptyCache()
	}
	return nil
}
