package etcd

import (
	"context"
	"testing"
	"time"
)

// etcd.Put RETURN: return the revision the write landed on.
func TestPutReturnsRevisionTheWriteLandedOn(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	rev, err := c.Put(ctx, "/put-rev/k", "v1", 0)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	// After the write, the cluster's current revision is the revision the
	// write landed on.
	resp, err := c.raw.Get(ctx, "/put-rev/k")
	if err != nil {
		t.Fatalf("raw Get: %v", err)
	}
	if resp.Header.Revision != rev {
		t.Errorf("Put returned revision %d, cluster is on %d", rev, resp.Header.Revision)
	}

	rev2, err := c.Put(ctx, "/put-rev/k", "v2", 0)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if rev2 <= rev {
		t.Errorf("second write landed on revision %d, want one after %d", rev2, rev)
	}
}

// etcd.Put SIDE-EFFECT: store the value under the key verbatim.
func TestPutStoresValueVerbatim(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	values := []string{"hello", "", `{"already":"json"}`, "line one\nline two"}
	for i, value := range values {
		key := "/put-verb/" + string(rune('a'+i))
		if _, err := c.Put(ctx, key, value, 0); err != nil {
			t.Fatalf("Put(%q): %v", key, err)
		}
		entries, err := c.Read(ctx, key, false)
		if err != nil || len(entries) != 1 {
			t.Fatalf("Read(%q) = %+v, %v; want the exact stored entry", key, entries, err)
		}
		if entries[0].Value != value {
			t.Errorf("stored %q, want the value verbatim %q", entries[0].Value, value)
		}
	}
}

// etcd.Put SIDE-EFFECT: a ttl grants a fresh one-shot lease of that many
// seconds with no keepalive, so the key expires that long after its most
// recent write.
func TestPutWithTTLGrantsFreshLeasePerWrite(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	key := "/put-ttl/k"

	// The first lease expires at t0+2; the second write at t0+0.6 grants
	// a fresh lease expiring at t0+2.6.
	start := time.Now()
	if _, err := c.Put(ctx, key, "first", 2); err != nil {
		t.Fatalf("Put with ttl: %v", err)
	}
	time.Sleep(600 * time.Millisecond)
	if _, err := c.Put(ctx, key, "second", 2); err != nil {
		t.Fatalf("refreshing Put with ttl: %v", err)
	}

	// At t0+2.2 the first lease has expired but the key survives on its
	// fresh lease: it expires after its most recent write.
	time.Sleep(time.Until(start.Add(2200 * time.Millisecond)))
	entries, err := c.Read(ctx, key, false)
	if err != nil || len(entries) != 1 || entries[0].Value != "second" {
		t.Errorf("read at 2.2s = %+v, %v; want the key alive on its fresh lease", entries, err)
	}

	// At t0+3.4 the second lease has expired too — with no keepalive.
	time.Sleep(time.Until(start.Add(3400 * time.Millisecond)))
	if _, err := c.Read(ctx, key, false); !IsNotFound(err) {
		t.Errorf("read at 3.4s err = %v, want the expired key gone (not-found)", err)
	}
}

// etcd.Put SIDE-EFFECT: a put with no ttl detaches the key from any lease
// it held.
func TestPutWithoutTTLDetachesKeyFromLease(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	key := "/put-detach/k"

	if _, err := c.Put(ctx, key, "leased", 2); err != nil {
		t.Fatalf("Put with ttl: %v", err)
	}
	if _, err := c.Put(ctx, key, "detached", 0); err != nil {
		t.Fatalf("Put without ttl: %v", err)
	}
	// The lease expires at ~2s; the key outlives it once detached.
	time.Sleep(2600 * time.Millisecond)
	entries, err := c.Read(ctx, key, false)
	if err != nil || len(entries) != 1 || entries[0].Value != "detached" {
		t.Errorf("read after lease expiry = %+v, %v; want the detached key to remain", entries, err)
	}
}

// The ttl path must not keep the lease alive: the key expires on its own
// after the granted lease runs out. etcd v3.5.14 rounds a requested TTL of
// one second up to a granted TTL of two (its minimum lease TTL), so the
// timing allows the server's rounding and expiry-check granularity.
func TestPutWithTTLExpiresAfterTTLSeconds(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	key := "/put-exp/k"

	if _, err := c.Put(ctx, key, "v", 1); err != nil {
		t.Fatalf("Put with ttl 1: %v", err)
	}
	entries, err := c.Read(ctx, key, false)
	if err != nil || len(entries) != 1 {
		t.Fatalf("read right after the ttl put = %+v, %v; want the key present", entries, err)
	}
	time.Sleep(4500 * time.Millisecond)
	if _, err := c.Read(ctx, key, false); !IsNotFound(err) {
		t.Errorf("read at 4.5s err = %v, want not-found once the lease expired without keepalive", err)
	}
}
