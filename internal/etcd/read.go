package etcd

import (
	"context"
	"sort"
	"strings"

	"go.etcd.io/etcd/client/v3"
)

// Entry is one entry of a read result: a stored key/value pair, or a
// synthesized immediate-child directory.
type Entry struct {
	// Key is the full etcd key, in the slash-directory form.
	Key string
	// Value is the stored value, verbatim. A synthesized directory entry
	// carries no value.
	Value string
	// Dir marks an entry that stands for a directory: an immediate child
	// of a listing whose own value, if any, is not reported.
	Dir bool
	// Created and Modified are the etcd creation and modification
	// revisions. For a synthesized directory entry they cover the newest
	// entry beneath it.
	Created  int64
	Modified int64
}

// kv is one stored pair as fetched from the cluster.
type kv struct {
	key   string
	value string
	cr    int64
	mr    int64
}

// Read reads a key in the slash-directory form.
//
// With recursive unset it returns the exact stored entry when the key
// itself holds a value; otherwise, when entries exist below the key, it
// returns the immediate children, one path segment deeper, with directory
// children marked Dir. With recursive set it returns every entry strictly
// below "key/", dropping the key's own value when it holds one.
//
// Multiple entries are ordered by creation revision, ties broken by
// lexicographic key order. The root with nothing stored returns an empty
// listing; any other key where nothing matches signals not-found. A read
// never matches a key that merely shares the addressed key as a string
// prefix.
func (c *Client) Read(ctx context.Context, key string, recursive bool) ([]Entry, error) {
	addr := TrimKey(key)

	if !recursive {
		// An exact hit answers immediately, children or not.
		exact, err := c.getExact(ctx, addr)
		if err != nil {
			return nil, err
		}
		if exact != nil {
			return []Entry{*exact}, nil
		}
		children, err := c.getChildren(ctx, addr)
		if err != nil {
			return nil, err
		}
		if len(children) == 0 {
			if addr == "/" {
				return []Entry{}, nil
			}
			return nil, notFound(addr)
		}
		return immediateChildren(addr, children), nil
	}

	children, err := c.getChildren(ctx, addr)
	if err != nil {
		return nil, err
	}
	if len(children) > 0 {
		sortKVs(children)
		entries := make([]Entry, 0, len(children))
		for _, k := range children {
			entries = append(entries, Entry{
				Key:      k.key,
				Value:    k.value,
				Created:  k.cr,
				Modified: k.mr,
			})
		}
		return entries, nil
	}
	exact, err := c.getExact(ctx, addr)
	if err != nil {
		return nil, err
	}
	if exact != nil {
		return []Entry{*exact}, nil
	}
	if addr == "/" {
		return []Entry{}, nil
	}
	return nil, notFound(addr)
}

// getExact fetches the pair stored at addr, or nil when the key holds no
// value.
func (c *Client) getExact(ctx context.Context, addr string) (*Entry, error) {
	var resp *clientv3.GetResponse
	err := c.call(ctx, func(cctx context.Context) error {
		r, err := c.raw.Get(cctx, addr)
		resp = r
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Kvs) == 0 {
		return nil, nil
	}
	k := resp.Kvs[0]
	return &Entry{
		Key:      string(k.Key),
		Value:    string(k.Value),
		Created:  k.CreateRevision,
		Modified: k.ModRevision,
	}, nil
}

// getChildren fetches every pair stored strictly below "addr/", never a
// key that merely shares addr as a string prefix. The result is unordered.
func (c *Client) getChildren(ctx context.Context, addr string) ([]kv, error) {
	prefix := childPrefix(addr)
	var resp *clientv3.GetResponse
	err := c.call(ctx, func(cctx context.Context) error {
		r, err := c.raw.Get(cctx, prefix, clientv3.WithPrefix())
		resp = r
		return err
	})
	if err != nil {
		return nil, err
	}
	var out []kv
	for _, k := range resp.Kvs {
		// The root's child prefix is "/" itself, which also matches the
		// root key; the root is not its own child.
		if string(k.Key) == addr {
			continue
		}
		out = append(out, kv{
			key:   string(k.Key),
			value: string(k.Value),
			cr:    k.CreateRevision,
			mr:    k.ModRevision,
		})
	}
	return out, nil
}

// immediateChildren reduces the (creation-ordered) children of addr to one
// entry per first path segment below it.
func immediateChildren(addr string, children []kv) []Entry {
	sortKVs(children)
	prefix := childPrefix(addr)
	out := []Entry{}
	index := map[string]int{}
	for _, k := range children {
		rel := strings.TrimPrefix(k.key, prefix)
		seg, _, deeper := strings.Cut(rel, "/")
		childKey := addr + seg
		if addr != "/" {
			childKey = addr + "/" + seg
		}
		if i, ok := index[seg]; ok {
			e := &out[i]
			if k.mr > e.Modified {
				e.Modified = k.mr
			}
			if deeper {
				e.Dir = true
				e.Value = ""
			}
			continue
		}
		e := Entry{Key: childKey, Created: k.cr, Modified: k.mr, Dir: deeper}
		if !deeper {
			e.Value = k.value
		}
		index[seg] = len(out)
		out = append(out, e)
	}
	return out
}

// sortKVs orders kvs by creation revision, ties broken by key.
func sortKVs(kvs []kv) {
	sort.Slice(kvs, func(i, j int) bool {
		if kvs[i].cr != kvs[j].cr {
			return kvs[i].cr < kvs[j].cr
		}
		return kvs[i].key < kvs[j].key
	})
}
