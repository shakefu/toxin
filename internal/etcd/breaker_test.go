package etcd

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/status"
)

// fakeClock is a settable clock for the breaker's cooldown.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (f *fakeClock) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeClock) advance(d time.Duration) {
	f.mu.Lock()
	f.t = f.t.Add(d)
	f.mu.Unlock()
}

// breakerClient builds a Client with no cluster behind it whose breaker
// runs on a fake clock and a fake connection-readiness check.
func breakerClient(t *testing.T) (*Client, *fakeClock, *atomic.Bool) {
	t.Helper()
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	ready := &atomic.Bool{}
	c := &Client{}
	c.breaker.now = clock.now
	c.breaker.ready = ready.Load
	return c, clock, ready
}

// failing returns a call body that counts its invocations and fails with
// err every time.
func failing(err error) (func(context.Context) error, *atomic.Int32) {
	n := &atomic.Int32{}
	return func(context.Context) error {
		n.Add(1)
		return err
	}, n
}

func succeeding() (func(context.Context) error, *atomic.Int32) {
	return failing(nil)
}

func (b *breaker) state() (tripped, probing bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastErr != nil, b.probing
}

// tripWith trips c's breaker on err, failing the test if it does not.
func tripWith(t *testing.T, c *Client, err error) error {
	t.Helper()
	fn, _ := failing(err)
	got := c.call(context.Background(), fn)
	if got == nil {
		t.Fatal("failing call returned nil")
	}
	if tripped, _ := c.breaker.state(); !tripped {
		t.Fatalf("breaker did not trip on %v", err)
	}
	return got
}

// assertFastFail asserts a call fails at once with want, without running
// its body. The attempt count proves no attempt ran; the time bound is
// loose so it holds under -race and load.
func assertFastFail(t *testing.T, c *Client, want error) {
	t.Helper()
	fn, n := succeeding()
	start := time.Now()
	err := c.call(context.Background(), fn)
	if el := time.Since(start); el > 100*time.Millisecond {
		t.Errorf("fast failure took %s, want it immediate", el)
	}
	if n.Load() != 0 {
		t.Errorf("call body ran %d times inside the cooldown, want 0", n.Load())
	}
	if err != want {
		t.Errorf("fast failure = %v, want the remembered error %v", err, want)
	}
}

// etcd.Client INVARIANT: a call that times out trips the breaker after the
// full retry budget, and the next call inside the cooldown fails fast with
// the remembered error.
func TestBreakerTripsOnTimeout(t *testing.T) {
	for name, err := range map[string]error{
		"context deadline": context.DeadlineExceeded,
		"grpc deadline":    status.Error(codes.DeadlineExceeded, "deadline"),
	} {
		t.Run(name, func(t *testing.T) {
			c, _, _ := breakerClient(t)
			fn, n := failing(err)
			got := c.call(context.Background(), fn)
			if n.Load() != maxAttempts {
				t.Errorf("closed breaker ran %d attempts, want the full %d", n.Load(), maxAttempts)
			}
			var e *Error
			if !errors.As(got, &e) {
				t.Fatalf("error %v is not an *Error", got)
			}
			assertFastFail(t, c, got)
		})
	}
}

// etcd.Client INVARIANT: an Unavailable failure, raised by gRPC or
// translated into an etcd server error, trips the breaker.
func TestBreakerTripsOnUnavailable(t *testing.T) {
	for name, err := range map[string]error{
		"grpc status":    status.Error(codes.Unavailable, "connection refused"),
		"etcd no leader": rpctypes.ErrNoLeader,
	} {
		t.Run(name, func(t *testing.T) {
			c, _, _ := breakerClient(t)
			got := tripWith(t, c, err)
			assertFastFail(t, c, got)
		})
	}
}

// etcd.Client INVARIANT: the breaker MUST NOT trip on not-found, on the
// caller's cancellation or deadline, or on a failure the cluster answered
// with, such as an authentication error.
func TestBreakerDoesNotTripOnNonTransportFailures(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()

	cases := map[string]struct {
		ctx context.Context
		err error
	}{
		"not found":         {context.Background(), notFound("/k")},
		"caller cancel":     {cancelled, context.Canceled},
		"caller deadline":   {expired, context.DeadlineExceeded},
		"permission denied": {context.Background(), status.Error(codes.PermissionDenied, "denied")},
		"auth failed":       {context.Background(), rpctypes.ErrAuthFailed},
		"unauthenticated":   {context.Background(), status.Error(codes.Unauthenticated, "who")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c, _, _ := breakerClient(t)
			fn, _ := failing(tc.err)
			if err := c.call(tc.ctx, fn); err == nil {
				t.Fatal("failing call returned nil")
			}
			if tripped, _ := c.breaker.state(); tripped {
				t.Errorf("breaker tripped on %v", tc.err)
			}
			next, n := succeeding()
			if err := c.call(context.Background(), next); err != nil {
				t.Errorf("next call = %v, want it to run normally", err)
			}
			if n.Load() != 1 {
				t.Errorf("next call body ran %d times, want 1", n.Load())
			}
		})
	}

	// A closed client fails without a transport failure, so it must not
	// trip the breaker either: a tripped breaker would hand back the
	// same remembered error and hide the closed-client failure.
	t.Run("closed client", func(t *testing.T) {
		c := newTestClient(t)
		if err := c.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		first, err := c.Read(context.Background(), "/breaker-closed/k", false)
		if err == nil {
			t.Fatalf("Read on a closed client = %v, want a failure", first)
		}
		if tripped, _ := c.breaker.state(); tripped {
			t.Errorf("breaker tripped on the closed-client failure %v", err)
		}
	})
}

// etcd.Client INVARIANT: inside the cooldown every call fails fast; the
// cooldown is measured from the trip.
func TestBreakerFailsFastThroughoutCooldown(t *testing.T) {
	c, clock, _ := breakerClient(t)
	want := tripWith(t, c, context.DeadlineExceeded)
	for _, step := range []time.Duration{0, failCooldown / 2, failCooldown/2 - time.Nanosecond} {
		clock.advance(step)
		assertFastFail(t, c, want)
	}
	if failCooldown > requestTimeout {
		t.Errorf("cooldown %s exceeds the request deadline %s", failCooldown, requestTimeout)
	}
}

// etcd.Client INVARIANT: once the cooldown expires exactly one probe runs,
// with a single attempt, while concurrent callers keep failing fast; the
// probe's success closes the breaker.
func TestBreakerHalfOpenProbeIsSingleAttempt(t *testing.T) {
	c, clock, _ := breakerClient(t)
	want := tripWith(t, c, context.DeadlineExceeded)
	clock.advance(failCooldown)

	started := make(chan struct{})
	release := make(chan error)
	var probeRuns atomic.Int32
	probeDone := make(chan error, 1)
	go func() {
		probeDone <- c.call(context.Background(), func(context.Context) error {
			if probeRuns.Add(1) == 1 {
				close(started)
			}
			return <-release
		})
	}()
	<-started

	var wg sync.WaitGroup
	var concurrentRuns atomic.Int32
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- c.call(context.Background(), func(context.Context) error {
				concurrentRuns.Add(1)
				return nil
			})
		}()
	}
	wg.Wait()
	close(errs)
	if concurrentRuns.Load() != 0 {
		t.Errorf("%d concurrent calls ran during the probe, want 0", concurrentRuns.Load())
	}
	for err := range errs {
		if err != want {
			t.Errorf("concurrent call during the probe = %v, want the fast failure %v", err, want)
		}
	}

	release <- nil
	if err := <-probeDone; err != nil {
		t.Fatalf("probe = %v, want success", err)
	}
	if probeRuns.Load() != 1 {
		t.Errorf("probe ran %d attempts, want 1", probeRuns.Load())
	}
	if tripped, probing := c.breaker.state(); tripped || probing {
		t.Errorf("after a successful probe tripped=%v probing=%v, want both false", tripped, probing)
	}
	fn, n := succeeding()
	if err := c.call(context.Background(), fn); err != nil || n.Load() != 1 {
		t.Errorf("call after recovery = %v with %d runs, want a normal call", err, n.Load())
	}
}

// etcd.Client INVARIANT: a probe that fails on the transport makes one
// attempt and refreshes the cooldown and the remembered error.
func TestBreakerFailedProbeRefreshesCooldown(t *testing.T) {
	c, clock, _ := breakerClient(t)
	tripWith(t, c, context.DeadlineExceeded)
	clock.advance(failCooldown)

	fn, n := failing(status.Error(codes.Unavailable, "still down"))
	probeErr := c.call(context.Background(), fn)
	if n.Load() != 1 {
		t.Errorf("probe ran %d attempts, want 1", n.Load())
	}
	var e *Error
	if !errors.As(probeErr, &e) || e.Msg != probeErr.Error() || e.Code != int(codes.Unavailable) {
		t.Fatalf("probe error = %#v, want the Unavailable failure", probeErr)
	}
	if _, probing := c.breaker.state(); probing {
		t.Error("probing still set after the probe ended")
	}
	clock.advance(failCooldown - time.Nanosecond)
	assertFastFail(t, c, probeErr)
}

// etcd.Client INVARIANT: a connection that becomes ready after the trip
// closes the breaker at once, even inside the cooldown, restoring the
// full retry budget.
func TestBreakerRecoversWhenConnectionBecomesReady(t *testing.T) {
	c, _, ready := breakerClient(t)
	tripWith(t, c, context.DeadlineExceeded)

	ready.Store(true)
	fn, n := failing(context.DeadlineExceeded)
	_ = c.call(context.Background(), fn)
	if n.Load() != maxAttempts {
		t.Errorf("call on a ready connection ran %d attempts, want the full %d", n.Load(), maxAttempts)
	}

	// That failure landed while the connection still reported ready, so
	// the ready report no longer clears the breaker; the connection must
	// first report not ready, then ready again.
	assertFastFail(t, c, mustTripped(t, c))
	ready.Store(false)
	assertFastFail(t, c, mustTripped(t, c))
	ready.Store(true)
	ok, okRuns := succeeding()
	if err := c.call(context.Background(), ok); err != nil || okRuns.Load() != 1 {
		t.Errorf("call on a reconnected connection = %v with %d runs, want a normal call", err, okRuns.Load())
	}
	if tripped, _ := c.breaker.state(); tripped {
		t.Error("breaker still tripped after a successful call")
	}
}

// mustTripped returns the breaker's remembered error, failing the test
// when the breaker is closed.
func mustTripped(t *testing.T, c *Client) *Error {
	t.Helper()
	c.breaker.mu.Lock()
	defer c.breaker.mu.Unlock()
	if c.breaker.lastErr == nil {
		t.Fatal("breaker is closed, want it tripped")
	}
	return c.breaker.lastErr
}

// etcd.Client INVARIANT: a connection that still reports ready when the
// failure lands (gRPC reports a connection to a dead peer as ready until
// keepalive notices) does not clear the breaker, so calls inside the
// cooldown keep failing fast and later calls are single-attempt probes.
func TestBreakerIgnoresReadyThatSurvivedTheTrip(t *testing.T) {
	c, clock, ready := breakerClient(t)
	ready.Store(true)

	fn, n := failing(context.DeadlineExceeded)
	want := c.call(context.Background(), fn)
	if n.Load() != maxAttempts {
		t.Fatalf("first call ran %d attempts, want the full %d", n.Load(), maxAttempts)
	}
	assertFastFail(t, c, want)
	clock.advance(failCooldown / 2)
	assertFastFail(t, c, want)

	clock.advance(failCooldown / 2)
	probe, probeRuns := failing(context.DeadlineExceeded)
	again := c.call(context.Background(), probe)
	if probeRuns.Load() != 1 {
		t.Errorf("call past the cooldown ran %d attempts, want a single-attempt probe", probeRuns.Load())
	}
	assertFastFail(t, c, again)

	// A call that reaches the cluster trusts the ready report again.
	clock.advance(failCooldown)
	ok, _ := succeeding()
	if err := c.call(context.Background(), ok); err != nil {
		t.Fatalf("successful probe = %v", err)
	}
	c.breaker.mu.Lock()
	tripped, untrusted := c.breaker.lastErr != nil, c.breaker.readyUntrusted
	c.breaker.mu.Unlock()
	if tripped || untrusted {
		t.Errorf("after a successful probe tripped=%v readyUntrusted=%v, want both false", tripped, untrusted)
	}
}

// etcd.Client INVARIANT: a readiness check that answers ready cannot
// close a breaker that tripped again while the check ran.
func TestBreakerReadyCheckCannotEraseConcurrentTrip(t *testing.T) {
	c, _, _ := breakerClient(t)
	tripWith(t, c, context.DeadlineExceeded)

	fresh := &Error{Msg: "tripped during the readiness check"}
	var once sync.Once
	c.breaker.ready = func() bool {
		once.Do(func() { c.breaker.trip(fresh, false) })
		return true
	}
	fn, n := succeeding()
	err := c.call(context.Background(), fn)
	if n.Load() != 0 {
		t.Errorf("call body ran %d times after a concurrent trip, want 0", n.Load())
	}
	if err != fresh {
		t.Errorf("call = %v, want the fast failure %v from the concurrent trip", err, fresh)
	}
	if got := mustTripped(t, c); got != fresh {
		t.Errorf("remembered error = %v, want %v", got, fresh)
	}

	// With no trip racing it, the next ready report closes the breaker.
	if err := c.call(context.Background(), fn); err != nil || n.Load() != 1 {
		t.Errorf("next call = %v with %d runs, want a normal call", err, n.Load())
	}
}

// etcd.Client INVARIANT: a probe that panics or is cancelled by its caller
// clears the probing flag, so a later caller past the cooldown can probe;
// a cancelled probe restarts the cooldown.
func TestBreakerProbeCleanupOnPanicAndCancel(t *testing.T) {
	c, clock, _ := breakerClient(t)
	tripWith(t, c, context.DeadlineExceeded)
	clock.advance(failCooldown)

	func() {
		defer func() {
			if recover() == nil {
				t.Error("probe body did not panic")
			}
		}()
		_ = c.call(context.Background(), func(context.Context) error { panic("boom") })
	}()
	if tripped, probing := c.breaker.state(); !tripped || probing {
		t.Errorf("after a panicking probe tripped=%v probing=%v, want tripped and not probing", tripped, probing)
	}

	ctx, cancel := context.WithCancel(context.Background())
	err := c.call(ctx, func(context.Context) error {
		cancel()
		return context.Canceled
	})
	if err == nil {
		t.Fatal("cancelled probe returned nil")
	}
	if tripped, probing := c.breaker.state(); !tripped || probing {
		t.Errorf("after a cancelled probe tripped=%v probing=%v, want tripped and not probing", tripped, probing)
	}

	// The cancelled probe restarted the cooldown, so the next caller
	// fails fast until it expires, then probes.
	assertFastFail(t, c, mustTripped(t, c))
	clock.advance(failCooldown)
	fn, n := succeeding()
	if err := c.call(context.Background(), fn); err != nil || n.Load() != 1 {
		t.Errorf("next probe = %v with %d runs, want it to run and succeed", err, n.Load())
	}
}

// etcd.Client INVARIANT: a call the cluster answers, even with a failure
// that is not a transport failure, closes a tripped breaker.
func TestBreakerClearsWhenClusterAnswers(t *testing.T) {
	c, clock, _ := breakerClient(t)
	tripWith(t, c, context.DeadlineExceeded)
	clock.advance(failCooldown)

	fn, _ := failing(status.Error(codes.PermissionDenied, "denied"))
	if err := c.call(context.Background(), fn); err == nil {
		t.Fatal("probe returned nil")
	}
	if tripped, _ := c.breaker.state(); tripped {
		t.Error("breaker still tripped after the cluster answered")
	}
}

// connReady reports a client with no connection as not ready.
func TestConnReadyWithoutConnection(t *testing.T) {
	if (&Client{}).connReady() {
		t.Error("a client with no connection reports ready")
	}
}

// connReady nudges an idle connection to connect without waiting: a fresh
// client's connection is idle until used, and repeated readiness checks
// alone bring it to ready against a live cluster.
func TestConnReadyConnectsIdleConnection(t *testing.T) {
	c := newTestClient(t)
	conn := c.raw.ActiveConnection()
	if state := conn.GetState(); state != connectivity.Idle {
		t.Skipf("fresh client connection is %s, not idle; the idle branch is unreachable here", state)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !c.connReady() {
		if time.Now().After(deadline) {
			t.Fatalf("connection still %s after readiness checks, want it connected", conn.GetState())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
