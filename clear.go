package toxin

import (
	"context"
	"errors"

	"github.com/shakefu/toxin/internal/etcd"
)

// Clear recursively deletes the prefix namespace from etcd and empties the
// cache. It requires the allowClear construction option; without it Clear
// fails with "clear() is not allowed on this instance".
//
// With cacheOnly the cache alone is emptied and etcd is untouched. A
// namespace that is already empty succeeds without error and leaves the
// cache as it is; the cache is emptied when cacheOnly is set or the
// namespace delete removed entries.
func (c *Config) Clear(opts ...CallOption) error {
	o := clearDefaults(opts)

	if !c.allowClear {
		return errors.New("clear() is not allowed on this instance")
	}

	if o.cacheOnly {
		c.emptyCache()
		return nil
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
