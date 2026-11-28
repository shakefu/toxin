package etcd

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tlog "github.com/shakefu/toxin/internal/log"
)

// etcd.Client INPUT: accept pre-resolved options and build from them
// without reading the environment. Every TOXIN_* variable below would
// change or break New; NewWithOptions sees none of them.
func TestNewWithOptionsIgnoresEnvironment(t *testing.T) {
	warnings := captureWarnings(t)
	missing := filepath.Join(t.TempDir(), "missing.pem")
	t.Setenv("TOXIN_ETCD", "a:b:c")
	t.Setenv("TOXIN_AUTH", "justauser")
	t.Setenv("TOXIN_SSL_CA", missing)
	t.Setenv("TOXIN_SSL_CERT", missing)
	t.Setenv("TOXIN_SSL_KEY", missing)

	c, err := NewWithOptions(Options{Members: []string{" 10.1.1.1:2379 ,"}})
	if err != nil {
		t.Fatalf("NewWithOptions with hostile TOXIN_* environment: %v", err)
	}
	defer c.Close()
	if got, want := c.Endpoints(), []string{"http://10.1.1.1:2379"}; !reflect.DeepEqual(got, want) {
		t.Errorf("endpoints = %v, want %v from the options, not TOXIN_ETCD", got, want)
	}
	if c.tls != nil {
		t.Error("client loaded TLS material from TOXIN_SSL_*, want none")
	}
	if c.username != "" || c.password != "" {
		t.Errorf("credentials = %q/%q, want none from TOXIN_AUTH", c.username, c.password)
	}
	if warnings.Len() != 0 {
		t.Errorf("NewWithOptions logged %q, want no TOXIN_AUTH or TLS warning", warnings.String())
	}

	// An empty option member list stays empty even with TOXIN_ETCD set to
	// a usable member, and incomplete option credentials are not even
	// validated without members.
	t.Setenv("TOXIN_ETCD", "10.2.0.1:2379")
	for _, opts := range []Options{{}, {Members: []string{" ,, "}, Auth: "user:", SSLCA: missing}} {
		c, err := NewWithOptions(opts)
		if err != nil || c != nil {
			t.Errorf("NewWithOptions(%+v) = (%v, %v), want (nil, nil)", opts, c, err)
		}
	}
	if warnings.Len() != 0 {
		t.Errorf("NewWithOptions without members logged %q, want silence", warnings.String())
	}
}

// etcd.Client INPUT: the pre-resolved options carry the credentials and
// TLS paths, with the same validation New applies to TOXIN_AUTH.
func TestNewWithOptionsUsesSuppliedAuthAndTLS(t *testing.T) {
	clearEtcdEnv(t)
	warnings := captureWarnings(t)
	dir := t.TempDir()
	ca, _ := writeTestCert(t, filepath.Join(dir, "ca"))

	c, err := NewWithOptions(Options{Members: []string{"127.0.0.1:2379"}, SSLCA: ca})
	if err != nil {
		t.Fatalf("NewWithOptions with SSLCA: %v", err)
	}
	defer c.Close()
	if got := c.Endpoints(); !reflect.DeepEqual(got, []string{"https://127.0.0.1:2379"}) {
		t.Errorf("endpoints = %v, want the https default from the supplied CA", got)
	}
	if c.tls == nil || len(c.tls.RootCAs.Subjects()) != 1 {
		t.Error("client did not load the supplied CA")
	}

	c2, err := NewWithOptions(Options{Members: []string{"127.0.0.1:2379"}, Auth: ":secret"})
	if err != nil {
		t.Fatalf("NewWithOptions with incomplete Auth: %v", err)
	}
	defer c2.Close()
	if c2.username != "" || c2.password != "" {
		t.Errorf("credentials = %q/%q, want the incomplete value ignored", c2.username, c2.password)
	}
	// Options do not come from TOXIN_AUTH, so the warning does not name it.
	got := warnings.String()
	if !strings.Contains(got, "auth credentials must be of the form 'username:password'") ||
		strings.Contains(got, "TOXIN_AUTH") || strings.Contains(got, "secret") {
		t.Errorf("warning %q, want the generic credentials warning without echoing them", got)
	}

	// TLS failures name the offending path, not a variable.
	bad := filepath.Join(dir, "missing.pem")
	_, err = NewWithOptions(Options{Members: []string{"127.0.0.1:2379"}, SSLCA: bad})
	if err == nil || !strings.Contains(err.Error(), "reading CA certificate") || !strings.Contains(err.Error(), bad) ||
		strings.Contains(err.Error(), "TOXIN_") {
		t.Errorf("unreadable SSLCA error = %v, want a generic message naming %q", err, bad)
	}
	warnings.Reset()
	c3, err := NewWithOptions(Options{Members: []string{"127.0.0.1:2379"}, SSLCert: ca})
	if err != nil {
		t.Fatalf("NewWithOptions with a lone client certificate: %v", err)
	}
	defer c3.Close()
	if got := warnings.String(); !strings.Contains(got, "client certificate") || !strings.Contains(got, ca) ||
		strings.Contains(got, "TOXIN_") {
		t.Errorf("incomplete pair warning = %q, want a generic message naming %q", got, ca)
	}
}

// etcd.Client RETURN: the underlying etcd client's diagnostics go through
// the supplied levelled logger, or nowhere when none is supplied, and never
// to a stderr logger of the client's own, even with ETCD_CLIENT_DEBUG set.
// 127.0.0.1:1 refuses connections, so a short read makes clientv3 log a
// failed retry at warn.
func TestNewWithOptionsRoutesClientDiagnosticsThroughLogger(t *testing.T) {
	clearEtcdEnv(t)
	t.Setenv("ETCD_CLIENT_DEBUG", "true")

	cases := []struct {
		name    string
		logger  func(io.Writer) *tlog.Logger
		wantLog bool
	}{
		{"critical", func(w io.Writer) *tlog.Logger { return tlog.New(w, tlog.Critical) }, false},
		{"warn", func(w io.Writer) *tlog.Logger { return tlog.New(w, tlog.Warn) }, true},
		{"nil", func(io.Writer) *tlog.Logger { return nil }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := &lockedBuffer{}
			stderr := captureStderr(t, func() {
				c, err := NewWithOptions(Options{Members: []string{"127.0.0.1:1"}, Logger: tc.logger(logs)})
				if err != nil {
					t.Fatalf("NewWithOptions: %v", err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
				defer cancel()
				if _, err := c.Read(ctx, "/k", false); err == nil {
					t.Error("Read against a refusing member succeeded")
				}
				_ = c.Close()
			})
			if stderr != "" {
				t.Errorf("etcd client wrote %d bytes to stderr: %q, want none", len(stderr), stderr)
			}
			got := logs.String()
			switch {
			case tc.wantLog && !strings.Contains(got, "WARN etcd-client: retrying of unary invoker failed"):
				t.Errorf("logger output %q, want the client's failed-retry warning", got)
			case !tc.wantLog && got != "":
				t.Errorf("logger output %q, want none at this level", got)
			}
		})
	}
}

// lockedBuffer is a bytes.Buffer safe for the etcd client's goroutines to
// write while the test reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureStderr runs fn with os.Stderr redirected into a pipe and returns
// what was written to it.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating stderr capture pipe: %v", err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()

	var data []byte
	done := make(chan struct{})
	go func() {
		data, _ = io.ReadAll(r)
		close(done)
	}()
	fn()
	os.Stderr = old
	_ = w.Close()
	<-done
	_ = r.Close()
	return string(data)
}

// etcd.Client INPUT: credentials are "username:password", split at the
// first colon, with both parts non-empty.
func TestParseAuth(t *testing.T) {
	for _, tc := range []struct {
		raw        string
		user, pass string
		ok         bool
	}{
		{"u:p", "u", "p", true},
		{"u:a:b", "u", "a:b", true},
		{"u:", "", "", false},
		{":p", "", "", false},
		{":", "", "", false},
		{"u", "", "", false},
		{"", "", "", false},
	} {
		user, pass, ok := parseAuth(tc.raw)
		if user != tc.user || pass != tc.pass || ok != tc.ok {
			t.Errorf("parseAuth(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.raw, user, pass, ok, tc.user, tc.pass, tc.ok)
		}
	}
}

// etcd.Client ERROR: the error naming a rejected member never echoes the
// member's password.
func TestInvalidMemberErrorRedactsPassword(t *testing.T) {
	clearEtcdEnv(t)
	for _, member := range []string{
		"http://user:secret@bad host:2379",
		"http://user:secret@host:notaport",
		"user:secret@host",
		"https://user:a:secret@host/path",
	} {
		_, err := New(member)
		if err == nil {
			t.Errorf("New(%q) = no error, want rejection", member)
			continue
		}
		if strings.Contains(err.Error(), "secret") {
			t.Errorf("New(%q) error %q echoes the password", member, err)
		}
		if !strings.Contains(err.Error(), "user:***@") {
			t.Errorf("New(%q) error %q does not name the member with the password redacted", member, err)
		}
	}
}

// etcd.Client INPUT: the environment-reading path resolves TOXIN_ETCD over
// the programmatic members, and when members remain warns once, by name,
// about an incomplete TOXIN_AUTH, handing NewWithOptions no credentials.
func TestEnvOptionsResolvesTOXINVariables(t *testing.T) {
	warnings := captureWarnings(t)

	opts := Env{Etcd: "10.2.0.1:2379", Auth: "u:p", SSLCA: "ca.pem", SSLCert: "c.pem", SSLKey: "k.pem"}.Options("10.1.1.1:2379")
	want := Options{Members: []string{"10.2.0.1:2379"}, Auth: "u:p", SSLCA: "ca.pem", SSLCert: "c.pem", SSLKey: "k.pem"}
	if !reflect.DeepEqual(opts, want) {
		t.Errorf("Options = %+v, want %+v", opts, want)
	}

	opts = Env{Auth: ":secret"}.Options("10.1.1.1:2379")
	if opts.Auth != "" {
		t.Errorf("incomplete TOXIN_AUTH passed on as %q, want it dropped", opts.Auth)
	}
	got := warnings.String()
	if strings.Count(got, "warning:") != 1 || !strings.Contains(got, "TOXIN_AUTH") || strings.Contains(got, "secret") {
		t.Errorf("warning %q, want one naming TOXIN_AUTH without the value", got)
	}

	warnings.Reset()
	opts = Env{Etcd: " ,, ", Auth: ":secret", SSLCA: "ca.pem"}.Options("10.1.1.1:2379")
	if len(splitMembers(opts.Members)) != 0 || opts.Auth != "" || opts.SSLCA != "" {
		t.Errorf("Options without members = %+v, want no members, credentials or TLS paths", opts)
	}
	if warnings.Len() != 0 {
		t.Errorf("Options without members warned %q, want silence", warnings.String())
	}

	if opts := (Env{}).Options("10.1.1.1:2379"); !reflect.DeepEqual(opts, Options{Members: []string{"10.1.1.1:2379"}}) {
		t.Errorf("zero Env Options = %+v, want only the programmatic members", opts)
	}
}
