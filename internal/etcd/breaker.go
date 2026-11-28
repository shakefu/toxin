package etcd

import (
	"context"
	"errors"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/status"
)

// failCooldown is how long a client fails fast after a call fails on the
// transport or its deadline, before it lets a single probe through. It is
// no longer than one request deadline.
const failCooldown = 5 * time.Second

// breaker remembers a transport or deadline failure so the calls that
// follow fail fast instead of each waiting out the full retry budget. A
// deadline expiry or an Unavailable failure from any source trips it:
// gRPC's own status, or an etcd server error such as "no leader" that the
// etcd client translated. Its zero value is closed (not tripped) and
// ready to use.
type breaker struct {
	mu sync.Mutex
	// openedAt is when the breaker last tripped, or when a cancelled
	// probe last restarted the cooldown; lastErr is the failure it
	// tripped on, nil while the breaker is closed.
	openedAt time.Time
	lastErr  *Error
	// probing is set while the single half-open probe is in flight.
	probing bool
	// gen counts trips, so a readiness check that ran outside the lock
	// cannot close a breaker that tripped again meanwhile.
	gen uint64
	// readyUntrusted is set when the breaker tripped while the connection
	// still reported ready: gRPC keeps reporting a connection to a dead
	// peer as ready until keepalive notices, so that report no longer
	// clears the breaker. It is cleared once the connection reports not
	// ready, or when a call reaches the cluster.
	readyUntrusted bool

	// now and ready are injectable for tests. A nil now defaults to
	// time.Now; a nil ready defers to the readiness check the client
	// passes in.
	now   func() time.Time
	ready func() bool
}

func (b *breaker) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

// readyFunc picks the readiness check: the injected one, else the
// client's.
func (b *breaker) readyFunc(connReady func() bool) func() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ready != nil {
		return b.ready
	}
	return connReady
}

// admit decides how a call proceeds. A closed breaker, or a tripped one
// whose connection became ready after the trip, allows the full retry
// budget. A tripped breaker inside its cooldown, or with a probe already
// in flight, fails fast with the remembered error. Otherwise the caller
// becomes the single probe with one attempt, and must call endProbe when
// it finishes.
func (b *breaker) admit(connReady func() bool) (attempts int, probe bool, fast *Error) {
	b.mu.Lock()
	tripped, gen := b.lastErr != nil, b.gen
	b.mu.Unlock()
	if !tripped {
		return maxAttempts, false, nil
	}

	// The readiness check runs outside the lock, since it may touch the
	// gRPC connection; its answer is acted on only if no trip landed
	// while it ran.
	ready := b.readyFunc(connReady)
	isReady := ready != nil && ready()

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.lastErr == nil {
		// Cleared while the readiness check ran.
		return maxAttempts, false, nil
	}
	if b.gen == gen {
		switch {
		case isReady && !b.readyUntrusted:
			b.closeLocked()
			return maxAttempts, false, nil
		case !isReady:
			// The connection noticed the failure; a later ready report
			// is a real reconnect.
			b.readyUntrusted = false
		}
	}
	if b.probing || b.clock().Sub(b.openedAt) < failCooldown {
		return 0, false, b.lastErr
	}
	b.probing = true
	return 1, true, nil
}

// endProbe ends the half-open probe, however the probe finished. A probe
// its caller cancelled says nothing about the cluster, so when the
// breaker is still tripped it restarts the cooldown rather than handing
// the next caller a fresh probe at once.
func (b *breaker) endProbe(ctx context.Context) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.probing = false
	if ctx.Err() != nil && b.lastErr != nil {
		b.openedAt = b.clock()
	}
}

// reset closes the breaker.
func (b *breaker) reset() {
	b.mu.Lock()
	b.closeLocked()
	b.mu.Unlock()
}

// closeLocked closes the breaker with b.mu held. An in-flight probe still
// owns the probing flag and clears it when it ends, so at most one probe
// runs at a time.
func (b *breaker) closeLocked() {
	b.openedAt = time.Time{}
	b.lastErr = nil
	b.readyUntrusted = false
}

// trip opens the breaker, or refreshes it, on the given failure. readyNow
// is whether the connection reported ready as the failure landed.
func (b *breaker) trip(e *Error, readyNow bool) {
	b.mu.Lock()
	b.gen++
	b.openedAt = b.clock()
	b.lastErr = e
	b.readyUntrusted = readyNow
	b.mu.Unlock()
}

// settle records how a call finished and returns its error in the shape
// this layer produces. Success, or any failure that shows the cluster
// answered, closes the breaker; a transport or deadline failure trips it.
// A call the caller cancelled or whose deadline the caller set leaves the
// breaker untouched, because it says nothing about the cluster.
func (b *breaker) settle(ctx context.Context, err error, connReady func() bool) error {
	if err == nil {
		b.reset()
		return nil
	}
	wrapped := wrapError(err)
	if ctx.Err() != nil {
		return wrapped
	}
	if !transportFailure(err) {
		b.reset()
		return wrapped
	}
	var e *Error
	if !errors.As(wrapped, &e) || e == nil {
		// wrapError always yields an *Error; without one there is no
		// error to remember.
		return wrapped
	}
	ready := b.readyFunc(connReady)
	b.trip(e, ready != nil && ready())
	return wrapped
}

// transportFailure reports whether err says the cluster could not be
// reached: a deadline expiry, or an Unavailable failure from any source,
// whether gRPC's own status or an etcd server error such as "no leader"
// translated by the etcd client.
func transportFailure(err error) bool {
	if timedOut(err) || status.Code(err) == codes.Unavailable {
		return true
	}
	var coded interface{ Code() codes.Code }
	return errors.As(err, &coded) && coded.Code() == codes.Unavailable
}

// connReady reports whether the client's gRPC connection is ready to
// serve. An idle connection is nudged to reconnect without waiting.
func (c *Client) connReady() bool {
	if c.raw == nil {
		return false
	}
	conn := c.raw.ActiveConnection()
	if conn == nil {
		return false
	}
	switch conn.GetState() {
	case connectivity.Ready:
		return true
	case connectivity.Idle:
		conn.Connect()
	}
	return false
}
