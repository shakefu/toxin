package toxin

import (
	"context"
	"strings"

	"github.com/shakefu/toxin/internal/etcd"
)

// List returns the immediate children of the prefix and key — key optional,
// addressed as "" — relative to the combined prefix-and-key address.
//
// With dirOnly (the default) only directory children are returned, without
// a trailing slash; with dirOnly unset, leaf keys come back as-is and
// directory keys with a trailing slash. Nothing under the namespace yields
// an empty list.
func (c *Config) List(key string, opts ...CallOption) ([]string, error) {
	o := listDefaults(opts)

	addr := etcd.TrimKey(c.prefix + c.coerceKey(key))
	entries, err := c.client.Read(context.Background(), addr, false)
	if err != nil {
		if etcd.IsNotFound(err) {
			return []string{}, nil
		}
		return nil, err
	}

	out := []string{}
	for _, e := range entries {
		if e.Key == addr && !e.Dir {
			// The address itself holds a value; it has no children.
			continue
		}
		child, ok := stripAddress(addr, e.Key)
		if !ok {
			continue
		}
		switch {
		case e.Dir && o.dirOnly:
			out = append(out, child)
		case e.Dir:
			out = append(out, child+"/")
		case o.dirOnly:
			// A leaf, filtered out of a dirOnly listing.
		default:
			out = append(out, child)
		}
	}
	return out, nil
}

// stripAddress removes the address from a key strictly below it,
// reporting whether the key is a child.
func stripAddress(addr, key string) (string, bool) {
	p := addr + "/"
	if addr == "/" {
		p = "/"
	}
	if !strings.HasPrefix(key, p) {
		return "", false
	}
	return strings.TrimPrefix(key, p), true
}
