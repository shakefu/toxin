package toxin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/shakefu/toxin/internal/etcd"
)

// Dump returns every setting under the prefix as a map from
// prefix-stripped, case-coerced key to decoded value, reading the whole
// subtree as the etcd read produces it and trusting the read's ordering
// and boundary guarantees.
//
// With inheritance allowed (the default) the parent dump is overlaid
// first, so the config's own keys win over equal keys in any ancestor. A
// parent namespace that holds nothing is not an error.
//
// When no entry exists under the prefix and the prefix is not the root,
// Dump fails with the not-found error rendered as 'Key not found:
// /<prefix>'; the root with nothing stored returns an empty map.
func (c *Config) Dump(opts ...CallOption) (map[string]any, error) {
	o := dumpDefaults(opts)

	out := map[string]any{}
	if o.allowInherited && c.inherit {
		if p := c.parentConfig(); p != nil {
			pm, err := p.Dump()
			if err != nil {
				if !etcd.IsNotFound(err) {
					c.log.Warnf("Unable to inherit: %v", err)
				}
				// A parent namespace that holds nothing is not an error.
				pm = map[string]any{}
			}
			for k, v := range pm {
				out[k] = v
			}
		}
	}

	entries, err := c.client.Read(context.Background(), c.prefix, true)
	if err != nil {
		if etcd.IsNotFound(err) {
			return nil, renderNotFound(err)
		}
		return nil, err
	}
	for _, e := range entries {
		k, ok := c.stripKey(e.Key)
		if !ok {
			continue
		}
		if !c.caseSensitive {
			k = strings.ToLower(k)
		}
		out[k] = etcd.DecodeValue(e.Value)
	}
	return out, nil
}

// renderNotFound renders a not-found error as 'Key not found: <cause>',
// keeping the underlying error wrapped so it still reads as not-found.
func renderNotFound(err error) error {
	var e *etcd.Error
	if errors.As(err, &e) {
		return fmt.Errorf("%w: %s", err, e.Cause)
	}
	return err
}
