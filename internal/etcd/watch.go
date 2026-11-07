package etcd

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.etcd.io/etcd/api/v3/mvccpb"
	"go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc/connectivity"
)

// EventKind distinguishes delivered watch events from reconnect signals.
type EventKind uint8

const (
	// EventSet reports a value put to a key in the watched range.
	EventSet EventKind = iota
	// EventDelete reports exactly one key deleted in one revision.
	EventDelete
	// EventDeleteDir reports several keys deleted within one revision,
	// coalesced into a single directory delete naming the watched root.
	EventDeleteDir
	// EventReconnect signals that the watch stream dropped, failed to be
	// created, or was canceled by the server, and is being re-established
	// (or, for a bare stream disconnect, left to the client's own stream
	// recovery). It carries Err and is a signal, not a data event.
	EventReconnect
)

// Event is one event delivered by a Watcher.
type Event struct {
	Kind EventKind
	// Key is the event's key: the written or deleted key, or the watched
	// root for a coalesced directory delete.
	Key string
	// Value carries the put value on EventSet.
	Value string
	// Revision is the revision the event landed on.
	Revision int64
	// Err carries the failure on EventReconnect.
	Err error
}

// WatchOption tunes Watch.
type WatchOption func(*watchConfig)

type watchConfig struct {
	recursive      bool
	startRevision  int64
	hasStartRevisi bool
}

// WithExactKey watches the exact key instead of the prefix. Watch is
// recursive by default.
func WithExactKey() WatchOption {
	return func(w *watchConfig) { w.recursive = false }
}

// WithStartRevision resumes the watch from the given revision, delivering
// the events that landed at and after it.
func WithStartRevision(rev int64) WatchOption {
	return func(w *watchConfig) { w.startRevision, w.hasStartRevisi = rev, true }
}

// Watcher is one established watch.
type Watcher struct {
	events chan Event

	client   *Client
	norm     string // the normalized key, prefix-watched without trimming
	root     string // the trimmed address events are ranged against
	cfg      watchConfig
	cancel   context.CancelFunc
	stopOnce sync.Once
}

// Watch watches key. The recursive flag defaults to true: the normalized
// key is watched as a prefix without trimming its trailing slash, so
// events on the watched key itself are delivered when it carries no
// trailing slash and are not delivered when it carries one; pass
// WithExactKey to watch the exact key instead. Events are delivered in
// revision order, never for keys outside the slash boundary of the watched
// root. A start revision may be given to resume from.
//
// The returned Watcher delivers events on its Events channel until Stop is
// called, the context is canceled or the client is closed. Failures are
// signaled as EventReconnect events: a bare stream disconnect is only
// signaled (the client re-establishes the stream itself), while a failed
// watch creation or a server-side cancel is additionally retried with a
// delay of six hundred milliseconds doubling per attempt, capped at five
// seconds, re-established from the current revision.
func (c *Client) Watch(ctx context.Context, key string, opts ...WatchOption) *Watcher {
	cfg := watchConfig{recursive: true}
	for _, opt := range opts {
		opt(&cfg)
	}
	wctx, cancel := context.WithCancel(ctx)
	w := &Watcher{
		events: make(chan Event, 128),
		client: c,
		norm:   NormalizeKey(key),
		root:   TrimKey(key),
		cfg:    cfg,
		cancel: cancel,
	}
	go w.run(wctx)
	go w.monitorConn(wctx)
	return w
}

// Events is the channel this watcher delivers on.
func (w *Watcher) Events() <-chan Event { return w.events }

// Key is the key the watch was created with, in the normalized form.
func (w *Watcher) Key() string { return w.norm }

// Stop stops the watch; the Events channel closes.
func (w *Watcher) Stop() {
	w.stopOnce.Do(w.cancel)
}

func (w *Watcher) run(ctx context.Context) {
	defer close(w.events)
	attempts := 0
	startRev := w.cfg.startRevision
	if !w.cfg.hasStartRevisi {
		startRev = 0
	}

	for {
		watchOpts := []clientv3.OpOption{clientv3.WithCreatedNotify()}
		if w.cfg.recursive {
			watchOpts = append(watchOpts, clientv3.WithPrefix())
		}
		if startRev > 0 {
			watchOpts = append(watchOpts, clientv3.WithRev(startRev))
		}
		ch := w.client.raw.Watch(ctx, w.norm, watchOpts...)

		var failure error
		for resp := range ch {
			if resp.Canceled {
				// The server canceled the watch (compaction, permission
				// loss) or the stream closed with an error: fall through
				// to the reconnect-and-retry path below.
				failure = resp.Err()
				if failure == nil {
					failure = errors.New("watch canceled by the server")
				}
				break
			}
			if resp.Created {
				attempts = 0
				continue
			}
			if !w.deliver(ctx, resp) {
				return
			}
		}

		// The channel closed. If we stopped it (context canceled or
		// client closed) this watcher is done; otherwise the watch failed
		// and is retried below.
		select {
		case <-ctx.Done():
			return
		case <-w.client.closedCh():
			return
		default:
		}
		if failure == nil {
			failure = errors.New("watch stream closed")
		}

		// Signal reconnect and retry with a delay of 600ms doubling per
		// attempt, capped at five seconds.
		attempts++
		if !w.emit(ctx, Event{Kind: EventReconnect, Err: failure}) {
			return
		}
		delay := retryDelay(attempts)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		case <-w.client.closedCh():
			return
		}
		// A start revision that the server canceled on (it was compacted
		// away) would fail again forever; resume from the current
		// revision.
		startRev = 0
	}
}

// deliver translates one watch stream response into events: puts become
// set events naming the key and carrying the value; deletes become delete
// events naming the key, coalesced into a single directory delete naming
// the watched root when several keys were deleted within one revision. It
// reports whether the watcher should keep running.
func (w *Watcher) deliver(ctx context.Context, resp clientv3.WatchResponse) bool {
	type pendingDelete struct {
		key string
		rev int64
	}
	var pending []pendingDelete
	flush := func() bool {
		if len(pending) == 0 {
			return true
		}
		var e Event
		if len(pending) > 1 {
			e = Event{Kind: EventDeleteDir, Key: w.root, Revision: pending[0].rev}
		} else {
			e = Event{Kind: EventDelete, Key: pending[0].key, Revision: pending[0].rev}
		}
		pending = nil
		return w.emit(ctx, e)
	}

	for _, ev := range resp.Events {
		key := string(ev.Kv.Key)
		// A prefix watch on "/config" also sees "/configuration"; never
		// deliver events outside the slash boundary of the watched root.
		if !inRange(w.root, key) {
			continue
		}
		switch ev.Type {
		case mvccpb.PUT:
			if !flush() {
				return false
			}
			if !w.emit(ctx, Event{
				Kind:     EventSet,
				Key:      key,
				Value:    string(ev.Kv.Value),
				Revision: ev.Kv.ModRevision,
			}) {
				return false
			}
		case mvccpb.DELETE:
			if len(pending) > 0 && pending[0].rev != ev.Kv.ModRevision {
				if !flush() {
					return false
				}
			}
			pending = append(pending, pendingDelete{key: key, rev: ev.Kv.ModRevision})
		}
	}
	return flush()
}

// emit sends e on the events channel unless the watcher is stopping.
func (w *Watcher) emit(ctx context.Context, e Event) bool {
	select {
	case w.events <- e:
		return true
	case <-ctx.Done():
		return false
	case <-w.client.closedCh():
		return false
	}
}

// monitorConn signals reconnect when the client's connection drops. A
// bare stream disconnect recovers through the client's own stream
// management, so no retry is scheduled here.
func (w *Watcher) monitorConn(ctx context.Context) {
	conn := w.client.raw.ActiveConnection()
	if conn == nil {
		return
	}
	for {
		state := conn.GetState()
		if state == connectivity.TransientFailure {
			if !w.emit(ctx, Event{Kind: EventReconnect, Err: errors.New("etcd connection lost; reconnecting")}) {
				return
			}
		}
		if !conn.WaitForStateChange(ctx, state) {
			return
		}
	}
}

// retryDelay is the delay before retry attempt n (1-based): six hundred
// milliseconds doubling per attempt, capped at five seconds.
func retryDelay(attempt int) time.Duration {
	d := 600 * time.Millisecond * (1 << (attempt - 1))
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}
