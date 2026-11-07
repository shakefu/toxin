package watchkill

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/shakefu/toxin/internal/etcd"
	"github.com/shakefu/toxin/internal/testutil"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testutil.Cleanup()
	os.Exit(code)
}

func clearEtcdEnv(t *testing.T) {
	t.Helper()
	for _, v := range []string{"TOXIN_ETCD", "TOXIN_AUTH", "TOXIN_SSL_CA", "TOXIN_SSL_CERT", "TOXIN_SSL_KEY"} {
		t.Setenv(v, "")
	}
}

// etcd.Watch ERROR: when the stream disconnects, signal reconnect without
// scheduling a retry. Killing this binary's own etcd container severs the
// live watch stream; the watcher must signal reconnect and keep its event
// channel open without delivering spurious data events.
func TestWatchStreamDisconnectSignalsReconnectWithoutRetry(t *testing.T) {
	clearEtcdEnv(t)
	c, err := etcd.New(testutil.Etcd(t))
	if err != nil {
		t.Fatalf("etcd.New: %v", err)
	}
	defer c.Close()
	ctx := context.Background()

	w := c.Watch(ctx, "/kill")
	defer w.Stop()

	// Prove the watch is live before severing it.
	if _, err := c.Put(ctx, "/kill/a", "1", 0); err != nil {
		t.Fatalf("Put: %v", err)
	}
	sawLive := false
	deadline := time.Now().Add(10 * time.Second)
	for !sawLive && time.Now().Before(deadline) {
		select {
		case e := <-w.Events():
			if e.Kind == etcd.EventSet && e.Key == "/kill/a" {
				sawLive = true
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !sawLive {
		t.Fatal("the watch never observed its sentinel put; cannot establish a baseline")
	}

	killEtcdContainer(t)

	// The server's death must produce a reconnect signal.
	deadline = time.Now().Add(15 * time.Second)
	signaled := false
	for !signaled && time.Now().Before(deadline) {
		select {
		case e, ok := <-w.Events():
			if !ok {
				t.Fatal("event channel closed on disconnect, want it kept open for recovery")
			}
			if e.Kind != etcd.EventReconnect {
				t.Fatalf("data event %#v on a disconnected watch, want only reconnect signals", e)
			}
			signaled = true
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !signaled {
		t.Fatal("no reconnect signal within 15s of the server dying")
	}

	// Nothing further but (at most) more reconnect signals from the
	// flapping connection: no data events are synthesized from a dead
	// stream, and the channel stays open. A watcher that scheduled a
	// retry on a bare stream disconnect would emit its next signal about
	// one retry delay — six hundred milliseconds — after an earlier one,
	// while sporadic connection flapping spaces its signals by the
	// connection's own backoff, a second or more apart. The absence of
	// that retry cadence is the "without scheduling a retry" half of the
	// obligation, so a signal landing about one first retry delay after
	// an earlier signal fails the test.
	first := time.Now()
	arrivals := []time.Duration{0} // the first signal, just received
	quiet := time.After(1500 * time.Millisecond)
	for {
		select {
		case e, ok := <-w.Events():
			if !ok {
				t.Fatal("event channel closed while recovering, want it open")
			}
			if e.Kind != etcd.EventReconnect {
				t.Fatalf("data event %#v after the disconnect, want none", e)
			}
			since := time.Since(first)
			for _, prev := range arrivals {
				if gap := since - prev; gap >= 450*time.Millisecond && gap <= 750*time.Millisecond {
					t.Fatalf("reconnect signal %v after an earlier one, about the six-hundred-millisecond first retry delay; want no scheduled retry", gap)
				}
			}
			arrivals = append(arrivals, since)
		case <-quiet:
			return
		}
	}
}

// killEtcdContainer kills the etcd container publishing the endpoint's
// port, severing every connection to it.
func killEtcdContainer(t *testing.T) {
	t.Helper()
	endpoint := testutil.Etcd(t)
	u, err := url.Parse(endpoint)
	if err != nil {
		t.Fatalf("parsing endpoint %q: %v", endpoint, err)
	}
	out, err := exec.Command("docker", "ps", "-q", "--filter", "publish="+u.Port()).Output()
	if err != nil {
		t.Fatalf("listing the etcd container by port %s (is the docker CLI available?): %v", u.Port(), err)
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		t.Fatalf("no container publishes %s; cannot sever the connection", u.Port())
	}
	for _, id := range ids {
		if out, err := exec.Command("docker", "kill", id).CombinedOutput(); err != nil {
			t.Fatalf("killing container %s: %v: %s", id, err, out)
		}
	}
}
