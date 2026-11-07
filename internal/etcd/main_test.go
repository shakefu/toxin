package etcd

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shakefu/toxin/internal/testutil"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testutil.Cleanup()
	os.Exit(code)
}

// clearEtcdEnv resets every TOXIN_ variable this layer reads, so a test's
// own arguments decide the client's configuration. An empty value reads as
// unset, matching the EnvNaming design.
func clearEtcdEnv(t *testing.T) {
	t.Helper()
	for _, v := range []string{
		"TOXIN_ETCD", "TOXIN_AUTH", "TOXIN_SSL_CA", "TOXIN_SSL_CERT", "TOXIN_SSL_KEY",
	} {
		t.Setenv(v, "")
	}
}

// newTestClient builds a client against the shared etcd test server.
func newTestClient(t *testing.T) *Client {
	t.Helper()
	clearEtcdEnv(t)
	c, err := New(testutil.Etcd(t))
	if err != nil {
		t.Fatalf("etcd.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func mustPut(t *testing.T, c *Client, key, value string) int64 {
	t.Helper()
	rev, err := c.Put(context.Background(), key, value, 0)
	if err != nil {
		t.Fatalf("Put(%q): %v", key, err)
	}
	return rev
}

// nextEvent returns the next watch event, failing the test on timeout.
func nextEvent(t *testing.T, w *Watcher, what string) Event {
	t.Helper()
	select {
	case e, ok := <-w.events:
		if !ok {
			t.Fatalf("watch channel closed while waiting for %s", what)
		}
		return e
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for watch event: %s", what)
		return Event{}
	}
}

// expectNoEvent asserts that no watch event arrives within d.
func expectNoEvent(t *testing.T, w *Watcher, d time.Duration) {
	t.Helper()
	select {
	case e, ok := <-w.events:
		if !ok {
			t.Fatalf("watch channel closed; want it open and silent")
		}
		t.Fatalf("unexpected watch event %#v within %s", e, d)
	case <-time.After(d):
	}
}

// establishWatch puts a sentinel under prefix and waits for its event, so
// puts made after it are reliably observed.
func establishWatch(t *testing.T, c *Client, w *Watcher, prefix string) {
	t.Helper()
	sentinel := childPrefix(TrimKey(prefix)) + "-established"
	mustPut(t, c, sentinel, "1")
	for {
		e := nextEvent(t, w, "watch establishment sentinel "+sentinel)
		if e.Kind == EventSet && e.Key == sentinel {
			return
		}
	}
}

// entryFacts is the part of an Entry a read result is asserted on; the
// revisions are asserted only where the obligation is about them.
type entryFacts struct {
	Key   string
	Value string
	Dir   bool
}

func summarize(entries []Entry) []entryFacts {
	out := make([]entryFacts, len(entries))
	for i, e := range entries {
		out[i] = entryFacts{Key: e.Key, Value: e.Value, Dir: e.Dir}
	}
	return out
}
