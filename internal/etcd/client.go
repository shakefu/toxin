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

	tlog "github.com/shakefu/toxin/internal/log"
	"go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
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
)

// warningLogger receives this layer's warnings, such as an incomplete
// TOXIN_AUTH value. It is a variable so tests can capture the output. Its
// warnings are unconditional, independent of TOXIN_LOGLEVEL: they report
// configuration the user supplied and toxin ignored, which the default
// critical level would otherwise hide.
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

// Options is the pre-resolved configuration NewWithOptions builds a Client
// from. NewWithOptions never reads the environment; Env.Options resolves
// Options from TOXIN_ETCD, TOXIN_AUTH, TOXIN_SSL_CA, TOXIN_SSL_CERT and
// TOXIN_SSL_KEY.
type Options struct {
	// Members is the member list: comma-separated strings, one member per
	// element, or a mix. Each member is host, host:port,
	// scheme://host:port, or scheme://user:password@host:port.
	Members []string
	// Auth carries "username:password" basic authentication credentials,
	// overriding any credentials a member carries. An incomplete value is
	// ignored with a warning.
	Auth string
	// SSLCA holds comma-separated CA certificate file paths, SSLCert the
	// client certificate file path and SSLKey the client key file path.
	SSLCA   string
	SSLCert string
	SSLKey  string
	// Logger receives the underlying etcd client's diagnostics at its own
	// level. When nil, the diagnostics are discarded.
	Logger *tlog.Logger
}

// Env holds the raw values of the TOXIN_* variables this layer honors. The
// zero Env stands for an environment that sets none of them.
type Env struct {
	Etcd    string // TOXIN_ETCD
	Auth    string // TOXIN_AUTH
	SSLCA   string // TOXIN_SSL_CA
	SSLCert string // TOXIN_SSL_CERT
	SSLKey  string // TOXIN_SSL_KEY
}

// ReadEnv reads the TOXIN_* variables this layer honors. It only reads;
// Env.Options validates.
func ReadEnv() Env {
	return Env{
		Etcd:    os.Getenv("TOXIN_ETCD"),
		Auth:    os.Getenv("TOXIN_AUTH"),
		SSLCA:   os.Getenv("TOXIN_SSL_CA"),
		SSLCert: os.Getenv("TOXIN_SSL_CERT"),
		SSLKey:  os.Getenv("TOXIN_SSL_KEY"),
	}
}

// Options resolves e against a programmatic member list: a non-empty
// TOXIN_ETCD replaces members. When no members remain, the credentials and
// TLS paths are left out unexamined. Otherwise an incomplete TOXIN_AUTH is
// ignored with a warning naming TOXIN_AUTH, regardless of the log level and
// without echoing the value, so NewWithOptions never sees it.
func (e Env) Options(members ...string) Options {
	opts := Options{Members: members}
	if e.Etcd != "" {
		opts.Members = []string{e.Etcd}
	}
	if len(splitMembers(opts.Members)) == 0 {
		return opts
	}
	if e.Auth != "" {
		if _, _, ok := parseAuth(e.Auth); ok {
			opts.Auth = e.Auth
		} else {
			warningLogger.Printf("warning: TOXIN_AUTH must be of the form 'username:password'; ignoring the incomplete credentials")
		}
	}
	opts.SSLCA, opts.SSLCert, opts.SSLKey = e.SSLCA, e.SSLCert, e.SSLKey
	return opts
}

// New builds a Client from a member list. The list may be given as a
// single comma-separated string or as one member per argument (or a mix);
// each member is host, host:port, scheme://host:port, or
// scheme://user:password@host:port. Surrounding whitespace is trimmed from
// each member and entries that trim to nothing are dropped. If no members
// remain, New returns a nil client and no error without loading TLS material,
// processing authentication, or constructing an underlying client.
// TOXIN_ETCD, when non-empty, is used in place of any programmatic member
// list, and TOXIN_AUTH, TOXIN_SSL_CA, TOXIN_SSL_CERT and TOXIN_SSL_KEY supply
// the credentials and TLS material. The underlying client's diagnostics are
// discarded; use NewWithOptions to route them through a Logger.
func New(members ...string) (*Client, error) {
	return NewWithOptions(ReadEnv().Options(members...))
}

// NewWithOptions builds a Client from pre-resolved options without reading
// the environment: not TOXIN_ETCD, TOXIN_AUTH or TOXIN_SSL_*, nor
// ETCD_CLIENT_DEBUG for the client's logger. Members are split, trimmed and filtered as New does;
// if none remain it returns a nil client and no error without loading TLS
// material, processing authentication, or constructing an underlying
// client. The underlying client always logs through opts.Logger (or
// nowhere when it is nil), never through a logger of its own on stderr.
func NewWithOptions(opts Options) (*Client, error) {
	raws := splitMembers(opts.Members)
	if len(raws) == 0 {
		return nil, nil
	}

	tlsCfg, tlsConfigured, err := loadTLS(opts.SSLCA, opts.SSLCert, opts.SSLKey)
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

	if opts.Auth != "" {
		if user, pass, ok := parseAuth(opts.Auth); ok {
			username, password, haveCreds = user, pass, true
		} else {
			// The raw value is never echoed: it may hold a password.
			warningLogger.Printf("warning: auth credentials must be of the form 'username:password'; ignoring the incomplete credentials")
		}
	}
	if haveCreds {
		cfg.Username, cfg.Password = username, password
	}

	// Always supply a logger, so clientv3 never builds its default stderr
	// JSON logger or consults ETCD_CLIENT_DEBUG for its level.
	if opts.Logger != nil {
		cfg.Logger = opts.Logger.Zap("etcd-client")
	} else {
		cfg.Logger = zap.NewNop()
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

// splitMembers splits every member argument on commas, trims surrounding
// whitespace from each entry, and drops entries that trim to nothing.
func splitMembers(members []string) []string {
	var raws []string
	for _, m := range members {
		for _, e := range strings.Split(m, ",") {
			if e = strings.TrimSpace(e); e != "" {
				raws = append(raws, e)
			}
		}
	}
	return raws
}

// parseAuth splits "username:password" credentials on the first colon, so
// a password may itself hold colons. The credentials are complete only
// when both the username and the password are non-empty.
func parseAuth(raw string) (user, pass string, ok bool) {
	user, pass, found := strings.Cut(raw, ":")
	if !found || user == "" || pass == "" {
		return "", "", false
	}
	return user, pass, true
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
