package etcd

import (
	"context"

	"go.etcd.io/etcd/client/v3"
)

// Put stores value under key verbatim, in the slash-directory form, and
// returns the revision the write landed on. The value is already encoded
// per the value rules (see EncodeValue).
//
// A ttl of one second or more grants a fresh one-shot lease of that many
// seconds with no keepalive, so the key expires that long after its most
// recent write. A ttl below one detaches the key from any lease it held.
func (c *Client) Put(ctx context.Context, key, value string, ttl int64) (int64, error) {
	norm := NormalizeKey(key)
	if ttl >= 1 {
		var leaseID clientv3.LeaseID
		err := c.call(ctx, func(cctx context.Context) error {
			resp, err := c.raw.Lease.Grant(cctx, ttl)
			if err == nil {
				leaseID = resp.ID
			}
			return err
		})
		if err != nil {
			return 0, err
		}
		return c.put(ctx, norm, value, clientv3.WithLease(leaseID))
	}
	return c.put(ctx, norm, value)
}

func (c *Client) put(ctx context.Context, key, value string, opts ...clientv3.OpOption) (int64, error) {
	var resp *clientv3.PutResponse
	err := c.call(ctx, func(cctx context.Context) error {
		r, err := c.raw.Put(cctx, key, value, opts...)
		resp = r
		return err
	})
	if err != nil {
		return 0, err
	}
	return resp.Header.Revision, nil
}
