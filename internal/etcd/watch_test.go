package etcd

import (
	"context"
	"testing"
	"time"
)

// etcd.Watch ERROR: when the server cancels the watch (here: the start
// revision was compacted away), signal reconnect and retry with backoff,
// re-establishing the watch from the current revision — a retry that kept
// the compacted start revision would cancel again and never deliver.
func TestWatchServerCancelRetriesFromCurrentRevision(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	rev1 := mustPut(t, c, "/w-cmp/a", "1")
	mustPut(t, c, "/w-cmp/b", "2")

	// Compact past rev1, so a watch asked to resume from it is canceled
	// by the server.
	current, err := c.raw.Get(ctx, "/w-cmp/b")
	if err != nil {
		t.Fatalf("raw Get: %v", err)
	}
	if _, err := c.raw.KV.Compact(ctx, current.Header.Revision); err != nil {
		t.Fatalf("compacting to %d: %v", current.Header.Revision, err)
	}

	w := c.Watch(ctx, "/w-cmp", WithStartRevision(rev1))
	defer w.Stop()

	e := nextEvent(t, w, "reconnect signal for the compacted start revision")
	if e.Kind != EventReconnect || e.Err == nil {
		t.Fatalf("event after cancel = %#v, want a reconnect signal carrying the failure", e)
	}

	// The retry fires after the 600ms first delay; once it has, the watch
	// is live again from the current revision and new writes flow.
	time.Sleep(800 * time.Millisecond)
	mustPut(t, c, "/w-cmp/c", "3")
	if e := nextEvent(t, w, "event on the re-established watch"); e.Kind != EventSet || e.Key != "/w-cmp/c" || e.Value != "3" {
		t.Fatalf("event after re-establishment = %#v, want the put of /w-cmp/c", e)
	}
}

// etcd.Watch ERROR: when creating the watch fails, signal reconnect and
// retry with a delay of six hundred milliseconds doubling per attempt.
func TestWatchCreateFailureSignalsReconnectAndRetriesWithDoublingDelay(t *testing.T) {
	c := newTestClient(t)
	w := c.Watch(context.Background(), "/w-dead")

	// Close the underlying client behind the layer's back: the live watch
	// channel closes, and every re-creation attempt fails immediately, so
	// the reconnect signals expose the retry schedule itself.
	_ = c.raw.Close()

	var arrivals []time.Duration
	start := time.Now()
	deadline := time.After(2600 * time.Millisecond)
	for len(arrivals) < 3 {
		select {
		case e, ok := <-w.events:
			if !ok {
				t.Fatalf("watch channel closed after %d signals; want retries scheduled", len(arrivals))
			}
			if e.Kind != EventReconnect {
				t.Fatalf("event %#v on a dead watch, want only reconnect signals", e)
			}
			arrivals = append(arrivals, time.Since(start))
		case <-deadline:
			t.Fatalf("only %d reconnect signals in 2.6s, want three (0ms, 600ms, 1800ms): %v", len(arrivals), arrivals)
		}
	}
	// The failures arrive on the doubling schedule: the second at least
	// half a second after the first, the third at least 1.7s in (600ms
	// then 1200ms of accumulated delay).
	if arrivals[1]-arrivals[0] < 500*time.Millisecond {
		t.Errorf("second signal %v after the first, want the 600ms first retry delay", arrivals[1]-arrivals[0])
	}
	if arrivals[2] < 1700*time.Millisecond {
		t.Errorf("third signal at %v, want it after the 600ms+1200ms delays", arrivals[2])
	}
	w.Stop()
}

// The retry delay itself: six hundred milliseconds doubling per attempt,
// capped at five seconds.
func TestRetryDelayDoublesFromSixHundredMsCappedAtFiveSeconds(t *testing.T) {
	cases := map[int]time.Duration{
		1: 600 * time.Millisecond,
		2: 1200 * time.Millisecond,
		3: 2400 * time.Millisecond,
		4: 4800 * time.Millisecond,
		5: 5 * time.Second,
		6: 5 * time.Second,
	}
	for attempt, want := range cases {
		if got := retryDelay(attempt); got != want {
			t.Errorf("retryDelay(%d) = %s, want %s", attempt, got, want)
		}
	}
}

// etcd.Watch SIDE-EFFECT: when a value is put, deliver a set event naming
// the key and carrying the value.
func TestWatchPutDeliversSetEventNamingKeyCarryingValue(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	w := c.Watch(ctx, "/w-set")
	defer w.Stop()
	establishWatch(t, c, w, "/w-set")

	rev := mustPut(t, c, "/w-set/key", "hello")
	e := nextEvent(t, w, "set event")
	if e.Kind != EventSet || e.Key != "/w-set/key" || e.Value != "hello" {
		t.Fatalf("event = %#v, want a set event naming the key and carrying the value", e)
	}
	if e.Revision != rev {
		t.Errorf("event revision = %d, want the write's revision %d", e.Revision, rev)
	}
}

// etcd.Watch SIDE-EFFECT: deliver events in revision order.
func TestWatchDeliversEventsInRevisionOrder(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	w := c.Watch(ctx, "/w-ord")
	defer w.Stop()
	establishWatch(t, c, w, "/w-ord")

	revA := mustPut(t, c, "/w-ord/a", "1")
	revB := mustPut(t, c, "/w-ord/b", "2")
	revC := mustPut(t, c, "/w-ord/c", "3")

	for i, want := range []struct {
		key string
		rev int64
	}{
		{"/w-ord/a", revA},
		{"/w-ord/b", revB},
		{"/w-ord/c", revC},
	} {
		e := nextEvent(t, w, "event in revision order")
		if e.Kind != EventSet || e.Key != want.key || e.Revision != want.rev {
			t.Errorf("event %d = %#v, want the set of %s at revision %d", i, e, want.key, want.rev)
		}
	}
}

// etcd.Watch SIDE-EFFECT: when exactly one key is deleted in a revision,
// deliver a delete event naming that key.
func TestWatchSingleDeleteNamesTheKey(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	w := c.Watch(ctx, "/w-del")
	defer w.Stop()
	establishWatch(t, c, w, "/w-del")

	mustPut(t, c, "/w-del/k", "v")
	if e := nextEvent(t, w, "set of the doomed key"); e.Kind != EventSet {
		t.Fatalf("event = %#v, want the set first", e)
	}
	rev, count, err := c.Delete(ctx, "/w-del/k", false)
	if err != nil || count != 1 {
		t.Fatalf("Delete = rev %d count %d err %v", rev, count, err)
	}
	e := nextEvent(t, w, "delete event")
	if e.Kind != EventDelete || e.Key != "/w-del/k" || e.Revision != rev {
		t.Fatalf("event = %#v, want a delete naming /w-del/k at revision %d", e, rev)
	}
	expectNoEvent(t, w, 300*time.Millisecond)
}

// etcd.Watch SIDE-EFFECT: when several keys are deleted within one
// revision, coalesce them into a single directory delete event naming the
// watched root.
func TestWatchCoalescesMultiKeyDeleteIntoDirectoryDeleteNamingWatchedRoot(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	w := c.Watch(ctx, "/w-co")
	defer w.Stop()
	establishWatch(t, c, w, "/w-co")

	mustPut(t, c, "/w-co/a", "1")
	mustPut(t, c, "/w-co/b", "2")
	nextEvent(t, w, "set of /w-co/a")
	nextEvent(t, w, "set of /w-co/b")

	// The recursive delete lands on one revision.
	rev, count, err := c.Delete(ctx, "/w-co", true)
	if err != nil || count != 3 { // sentinel, a and b
		t.Fatalf("Delete = rev %d count %d err %v, want one revision deleting all three", rev, count, err)
	}
	e := nextEvent(t, w, "coalesced directory delete")
	if e.Kind != EventDeleteDir || e.Key != "/w-co" || e.Revision != rev {
		t.Fatalf("event = %#v, want one directory delete naming the watched root /w-co at revision %d", e, rev)
	}
	// The individual deletes never arrive: they were coalesced.
	expectNoEvent(t, w, 300*time.Millisecond)
}

// The same coalescing names the root when the watched root is the root
// itself and the whole keyspace goes in one revision.
func TestWatchCoalescesRootKeyspaceDeleteNamingRoot(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	w := c.Watch(ctx, "/")
	defer w.Stop()
	establishWatch(t, c, w, "/")

	rev, count, err := c.Delete(ctx, "/", true)
	if err != nil || count == 0 {
		t.Fatalf("Delete(/) = rev %d count %d err %v", rev, count, err)
	}
	e := nextEvent(t, w, "coalesced root directory delete")
	if e.Kind != EventDeleteDir || e.Key != "/" || e.Revision != rev {
		t.Fatalf("event = %#v, want one directory delete naming the watched root / at revision %d", e, rev)
	}
	expectNoEvent(t, w, 300*time.Millisecond)
}

// etcd.Watch SIDE-EFFECT: watch the normalized key as a prefix without
// trimming its trailing slash — events on the watched key itself are
// delivered when it carries no trailing slash and are not delivered when
// it carries one.
func TestWatchRecursiveWatchesPrefixWithoutTrimmingTrailingSlash(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	// Without a trailing slash the watched key itself is in range.
	wPlain := c.Watch(ctx, "/w-ts1")
	defer wPlain.Stop()
	establishWatch(t, c, wPlain, "/w-ts1")
	mustPut(t, c, "/w-ts1", "the key itself")
	if e := nextEvent(t, wPlain, "event on the watched key itself"); e.Kind != EventSet || e.Key != "/w-ts1" {
		t.Fatalf("event = %#v, want the set of /w-ts1 itself", e)
	}

	// With a trailing slash the key itself (which carries none) is out of
	// range, while everything below stays in range.
	wSlashed := c.Watch(ctx, "/w-ts2/")
	defer wSlashed.Stop()
	establishWatch(t, c, wSlashed, "/w-ts2")
	mustPut(t, c, "/w-ts2", "not delivered")
	expectNoEvent(t, wSlashed, 400*time.Millisecond)
	mustPut(t, c, "/w-ts2/child", "delivered")
	if e := nextEvent(t, wSlashed, "event below the slashed watch root"); e.Kind != EventSet || e.Key != "/w-ts2/child" {
		t.Fatalf("event = %#v, want the set of /w-ts2/child", e)
	}
}

// etcd.Watch SIDE-EFFECT: with the recursive flag unset, watch the exact
// key instead of the prefix.
func TestWatchExactKeyWatchesOnlyThatKey(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	w := c.Watch(ctx, "/w-exact/k", WithExactKey())
	defer w.Stop()

	mustPut(t, c, "/w-exact/k", "mine")
	e := nextEvent(t, w, "event on the exact key")
	if e.Kind != EventSet || e.Key != "/w-exact/k" || e.Value != "mine" {
		t.Fatalf("event = %#v, want the set of the exact key", e)
	}

	mustPut(t, c, "/w-exact/other", "not mine")
	expectNoEvent(t, w, 400*time.Millisecond)

	mustPut(t, c, "/w-exact/k", "mine again")
	e = nextEvent(t, w, "second event on the exact key")
	if e.Kind != EventSet || e.Key != "/w-exact/k" || e.Value != "mine again" {
		t.Fatalf("event = %#v, want the second set of the exact key", e)
	}
}

// etcd.Watch MUST-NOT: never deliver events for keys outside the slash
// boundary of the watched root.
func TestWatchNeverDeliversEventsOutsideSlashBoundary(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	w := c.Watch(ctx, "/w-bound")
	defer w.Stop()
	establishWatch(t, c, w, "/w-bound")

	mustPut(t, c, "/w-boundary/key", "shares the string prefix only")
	expectNoEvent(t, w, 400*time.Millisecond)

	mustPut(t, c, "/w-bound/key", "inside the boundary")
	if e := nextEvent(t, w, "event inside the boundary"); e.Kind != EventSet || e.Key != "/w-bound/key" {
		t.Fatalf("event = %#v, want the set of /w-bound/key", e)
	}
}

// etcd.Watch INPUT: a start revision may resume the watch from a given
// point.
func TestWatchStartRevisionResumesFromGivenRevision(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	revA := mustPut(t, c, "/w-res/a", "1")
	revB := mustPut(t, c, "/w-res/b", "2")

	w := c.Watch(ctx, "/w-res", WithStartRevision(revB))
	defer w.Stop()
	e := nextEvent(t, w, "event at or after the start revision")
	if e.Kind != EventSet || e.Key != "/w-res/b" || e.Revision != revB {
		t.Fatalf("first event = %#v, want the put of /w-res/b at revision %d", e, revB)
	}
	// The event before the start revision is not replayed.
	expectNoEvent(t, w, 400*time.Millisecond)
	if revB <= revA {
		t.Errorf("revisions %d then %d, want increasing", revA, revB)
	}
}
