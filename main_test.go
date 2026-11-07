package toxin

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/shakefu/toxin/internal/etcd"
	"github.com/shakefu/toxin/internal/log"
	"github.com/shakefu/toxin/internal/testutil"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testutil.Cleanup()
	os.Exit(code)
}

// clearToxinEnv resets every TOXIN_ runtime variable, so a test's own
// options and variables decide the configuration. An empty value reads as
// unset, matching the EnvNaming design.
func clearToxinEnv(t *testing.T) {
	t.Helper()
	for _, v := range []string{
		"TOXIN_ETCD", "TOXIN_PREFIX", "TOXIN_LOGLEVEL", "TOXIN_CACHE",
		"TOXIN_WATCH", "TOXIN_INHERIT_DEPTH", "TOXIN_AUTH",
		"TOXIN_SSL_CA", "TOXIN_SSL_CERT", "TOXIN_SSL_KEY",
	} {
		t.Setenv(v, "")
	}
}

// newTestConfig constructs a Config against the shared etcd test server.
func newTestConfig(t *testing.T, opts ...Option) *Config {
	t.Helper()
	clearToxinEnv(t)
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))
	c, err := New(opts...)
	if err != nil {
		t.Fatalf("toxin.New: %v", err)
	}
	return c
}

// newRawClient builds a bare etcd layer client against the test server,
// for writing and reading the raw etcd state around a Config.
func newRawClient(t *testing.T) *etcd.Client {
	t.Helper()
	clearToxinEnv(t)
	c, err := etcd.New(testutil.Etcd(t))
	if err != nil {
		t.Fatalf("etcd.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// rawGet reads the raw stored value at an exact etcd key, reporting
// whether it is present.
func rawGet(t *testing.T, client *etcd.Client, key string) (string, bool) {
	t.Helper()
	entries, err := client.Read(context.Background(), key, false)
	if err != nil {
		return "", false
	}
	addr := etcd.TrimKey(key)
	for _, e := range entries {
		if e.Key == addr && !e.Dir {
			return e.Value, true
		}
	}
	return "", false
}

// rawPut writes a raw encoded value at an exact etcd key.
func rawPut(t *testing.T, client *etcd.Client, key, value string) {
	t.Helper()
	if _, err := client.Put(context.Background(), key, value, 0); err != nil {
		t.Fatalf("raw put %s: %v", key, err)
	}
}

// captureLog replaces the config's logger with one that captures its
// output, so a test can observe logged warnings.
func captureLog(c *Config) *bytes.Buffer {
	buf := &bytes.Buffer{}
	c.log = log.New(buf, log.Warn)
	return buf
}

// waitFor polls cond until it holds, failing the test at the timeout.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, what)
}

// jsonEqual compares two values by their JSON encoding, so decoded
// numbers (json.Number) compare equal to their plain counterparts.
func jsonEqual(t *testing.T, got, want any) bool {
	t.Helper()
	g, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("encoding got value %v: %v", got, err)
	}
	w, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("encoding want value %v: %v", want, err)
	}
	return string(g) == string(w)
}
