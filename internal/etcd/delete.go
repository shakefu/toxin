package etcd

import (
	"context"

	"go.etcd.io/etcd/client/v3"
)

// Delete deletes a key in the slash-directory form and returns the
// revision the delete landed on with the count of entries deleted.
//
// With recursive unset it deletes the exact key only. With recursive set
// it deletes the exact key and everything below "key/" in a single
// transaction that lands on one revision; the root deletes the entire
// keyspace in one range delete. A delete that removes nothing signals
// not-found.
func (c *Client) Delete(ctx context.Context, key string, recursive bool) (revision, count int64, err error) {
	addr := TrimKey(key)

	switch {
	case !recursive:
		var resp *clientv3.DeleteResponse
		err = c.call(ctx, func(cctx context.Context) error {
			r, err := c.raw.Delete(cctx, addr)
			resp = r
			return err
		})
		if err != nil {
			return 0, 0, err
		}
		revision, count = resp.Header.Revision, resp.Deleted

	case addr == "/":
		// Everything this layer stores lives below the root prefix; one
		// range delete clears the keyspace.
		var resp *clientv3.DeleteResponse
		err = c.call(ctx, func(cctx context.Context) error {
			r, err := c.raw.Delete(cctx, "/", clientv3.WithPrefix())
			resp = r
			return err
		})
		if err != nil {
			return 0, 0, err
		}
		revision, count = resp.Header.Revision, resp.Deleted

	default:
		var resp *clientv3.TxnResponse
		err = c.call(ctx, func(cctx context.Context) error {
			r, err := c.raw.Txn(cctx).
				Then(
					clientv3.OpDelete(addr),
					clientv3.OpDelete(childPrefix(addr), clientv3.WithPrefix()),
				).
				Commit()
			resp = r
			return err
		})
		if err != nil {
			return 0, 0, err
		}
		revision = resp.Header.Revision
		for _, op := range resp.Responses {
			if d := op.GetResponseDeleteRange(); d != nil {
				count += d.Deleted
			}
		}
	}

	if count == 0 {
		return 0, 0, notFound(addr)
	}
	return revision, count, nil
}
