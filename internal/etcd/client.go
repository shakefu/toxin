// Package etcd implements toxin's etcd v3 client layer: the key and value
// forms that cross the etcd boundary, and the reads, writes, deletes and
// watches the configuration layers build on.
package etcd

import (
	"context"
	"crypto/tls"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Every single-request call carries this deadline and is retried when it
// times out, so an unreachable cluster surfaces failure after roughly
// three deadlines in total rather than one.
const (
	requestTimeout = 5 * time.Second
	maxAttempts    = 3
	defaultMember  = "127.0.0.1:2379"
)

// warningLogger receives this layer's warnings, such as an incomplete
// TOXIN_AUTH value. It is a variable so tests can capture the output.
var warningLogger = log.New(os.Stderr, "", 0)

// Client is an etcd v3 client holding the resolved member list and
// credentials; it serves every read, write, delete and watch.
type Client struct {
	raw *clientv3.Client

	// The resolved configuration this client was built from.
	endpoints []string
	username  string
	password  string
	tls       *tls.Config

	closed    chan struct{}
	closeOnce sync.Once
}

// New builds a Client from a member list. The list may be given as a
// single comma-separated string or as one member per argument (or a mix);
// each member is host, host:port, scheme://host:port, or
// scheme://user:password@host:port. Surrounding whitespace is trimmed from
// each member and entries that trim to nothing are dropped; an absent
// member list resolves to the single member 127.0.0.1:2379. TOXIN_ETCD,
// when set, is used in place of any programmatic member list, and
// TOXIN_AUTH, TOXIN_SSL_CA, TOXIN_SSL_CERT and TOXIN_SSL_KEY supply the
// credentials and TLS material.
func New(members ...string) (*Client, error) {
	list := members
	if env := os.Getenv("TOXIN_ETCD"); env != "" {
		list = []string{env}
	}

	var entries []string
	for _, m := range list {
		entries = append(entries, strings.Split(m, ",")...)
	}
	var raws []string
	for _, e := range entries {
		if e = strings.TrimSpace(e); e != "" {
			raws = append(raws, e)
		}
	}
	if len(raws) == 0 {
		raws = []string{defaultMember}
	}

	tlsCfg, tlsConfigured, err := loadTLS()
	if err != nil {
		return nil, err
	}
	defaultScheme := "http"
	if tlsConfigured {
		defaultScheme = "https"
	}

	cfg := clientv3.Config{TLS: tlsCfg}
	var username, password string
	haveCreds := false
	for _, raw := range raws {
		m, err := parseMember(raw, defaultScheme)
		if err != nil {
			return nil, err
		}
		cfg.Endpoints = append(cfg.Endpoints, m.endpoint)
		if m.hasCreds {
			// Several members may carry credentials; the last one wins,
			// as pyconfig did when it read the last JETCONFIG_ETCD entry.
			username, password, haveCreds = m.username, m.password, true
		}
	}

	if auth := os.Getenv("TOXIN_AUTH"); auth != "" {
		if user, pass, ok := strings.Cut(auth, ":"); ok {
			username, password, haveCreds = user, pass, true
		} else {
			warningLogger.Printf("warning: TOXIN_AUTH must be of the form 'username:password'; ignoring the incomplete credentials %q", auth)
		}
	}
	if haveCreds {
		cfg.Username, cfg.Password = username, password
	}

	// Wait for the connection rather than failing fast, so a member that
	// is down produces a timed-out call (which the retry loop handles)
	// instead of an immediate failure. DialTimeout bounds the client's
	// eager credential check: with credentials configured against an
	// unreachable cluster, clientv3 would otherwise block in New forever.
	cfg.DialTimeout = requestTimeout
	cfg.DialOptions = []grpc.DialOption{
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
	}

	raw, err := clientv3.New(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{
		raw:       raw,
		endpoints: cfg.Endpoints,
		username:  username,
		password:  password,
		tls:       tlsCfg,
		closed:    make(chan struct{}),
	}, nil
}

// Endpoints returns a copy of the resolved member list the client holds.
func (c *Client) Endpoints() []string {
	out := make([]string, len(c.endpoints))
	copy(out, c.endpoints)
	return out
}

// Close closes the client and terminates every watch it serves. Watchers
// stop without reconnect signals.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closed)
		err = c.raw.Close()
	})
	return err
}

// closedCh exposes the client's closed channel to watchers.
func (c *Client) closedCh() <-chan struct{} { return c.closed }

// call runs a single-request call under the request deadline, retrying a
// timed-out call so an unreachable cluster surfaces failure after roughly
// fifteen seconds rather than five.
func (c *Client) call(ctx context.Context, fn func(context.Context) error) error {
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		cctx, cancel := context.WithTimeout(ctx, requestTimeout)
		err = fn(cctx)
		cancel()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil || !timedOut(err) {
			break
		}
	}
	return wrapError(err)
}

// timedOut reports whether err is a client or gRPC deadline expiry.
func timedOut(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) ||
		status.Code(err) == codes.DeadlineExceeded
}
