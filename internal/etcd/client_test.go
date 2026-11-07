package etcd

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shakefu/toxin/internal/testutil"
	"go.etcd.io/etcd/client/v3"
)

// etcd.Client ERROR: a member matching none of host, host:port,
// scheme://host:port or scheme://user:password@host:port is rejected with
// an error naming the member.
func TestNewRejectsMemberMatchingNoForm(t *testing.T) {
	clearEtcdEnv(t)
	for _, member := range []string{
		"host:notaport",  // port is not numeric
		"host:",          // empty port
		"a:b:c",          // two colons without a scheme
		"http://",        // empty host
		":2379",          // empty host
		"user:pass@host", // credentials without a scheme
		"1http://host",   // scheme does not start with a letter
		"host/with/slashes",
	} {
		_, err := New(member)
		if err == nil {
			t.Errorf("New(%q) = no error, want rejection", member)
			continue
		}
		if !strings.Contains(err.Error(), member) {
			t.Errorf("New(%q) error %q does not name the member", member, err)
		}
	}
}

// etcd.Client INPUT: accept a member list as a single comma-separated
// string or a list of member strings.
func TestNewAcceptsCommaSeparatedStringAndMemberList(t *testing.T) {
	clearEtcdEnv(t)
	want := []string{"http://10.1.1.1:2379", "http://10.1.1.2:2379"}

	c, err := New("10.1.1.1:2379,10.1.1.2:2379")
	if err != nil {
		t.Fatalf("New(comma-separated string): %v", err)
	}
	if got := c.Endpoints(); !reflect.DeepEqual(got, want) {
		t.Errorf("endpoints = %v, want %v", got, want)
	}
	_ = c.Close()

	c, err = New("10.1.1.1:2379", "10.1.1.2:2379")
	if err != nil {
		t.Fatalf("New(member list): %v", err)
	}
	if got := c.Endpoints(); !reflect.DeepEqual(got, want) {
		t.Errorf("endpoints = %v, want %v", got, want)
	}
	_ = c.Close()
}

// etcd.Client INPUT: trim surrounding whitespace from each member and drop
// entries that trim to nothing.
func TestNewTrimsWhitespaceAndDropsEmptyMembers(t *testing.T) {
	clearEtcdEnv(t)
	c, err := New("  10.1.1.1:2379 , ,, 10.1.1.2:2379\t,\t")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	want := []string{"http://10.1.1.1:2379", "http://10.1.1.2:2379"}
	if got := c.Endpoints(); !reflect.DeepEqual(got, want) {
		t.Errorf("endpoints = %v, want %v", got, want)
	}
}

// etcd.Client INPUT: treat an absent member list as the single member
// 127.0.0.1:2379.
func TestNewDefaultsToLoopbackWhenMemberListAbsent(t *testing.T) {
	clearEtcdEnv(t)
	for _, args := range [][]string{nil, {" ,, "}} {
		c, err := New(args...)
		if err != nil {
			t.Fatalf("New(%v): %v", args, err)
		}
		want := []string{"http://127.0.0.1:2379"}
		if got := c.Endpoints(); !reflect.DeepEqual(got, want) {
			t.Errorf("New(%v) endpoints = %v, want %v", args, got, want)
		}
		_ = c.Close()
	}
}

// etcd.Client INPUT: TOXIN_ETCD, when set, is used in place of any
// programmatic member list; an empty value reads as unset.
func TestNewTOXINETCDOverridesProgrammaticMembers(t *testing.T) {
	clearEtcdEnv(t)
	t.Setenv("TOXIN_ETCD", "10.2.0.1:2379, 10.2.0.2:2379")
	c, err := New("10.1.1.1:2379")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	want := []string{"http://10.2.0.1:2379", "http://10.2.0.2:2379"}
	if got := c.Endpoints(); !reflect.DeepEqual(got, want) {
		t.Errorf("endpoints = %v, want %v", got, want)
	}

	// An empty TOXIN_ETCD reads as unset, so the programmatic list wins.
	t.Setenv("TOXIN_ETCD", "")
	c2, err := New("10.1.1.1:2379")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c2.Close()
	if got := c2.Endpoints(); !reflect.DeepEqual(got, []string{"http://10.1.1.1:2379"}) {
		t.Errorf("endpoints with empty TOXIN_ETCD = %v, want the programmatic member", got)
	}
}

// etcd.Client INPUT: accept each member as host, host:port,
// scheme://host:port, or scheme://user:password@host:port, defaulting the
// scheme to http.
func TestNewAcceptsEachMemberForm(t *testing.T) {
	clearEtcdEnv(t)
	cases := []struct {
		member   string
		endpoint string
	}{
		{"etcd1", "http://etcd1"},
		{"etcd1:2379", "http://etcd1:2379"},
		{"https://etcd1:2379", "https://etcd1:2379"},
	}
	for _, c := range cases {
		cl, err := New(c.member)
		if err != nil {
			t.Errorf("New(%q): %v", c.member, err)
			continue
		}
		if got := cl.Endpoints(); !reflect.DeepEqual(got, []string{c.endpoint}) {
			t.Errorf("New(%q) endpoints = %v, want [%v]", c.member, got, c.endpoint)
		}
		_ = cl.Close()
	}

	// The credential-bearing forms resolve the same way; they cannot be
	// constructed end to end against no cluster, because clientv3
	// validates credentials at construction (bounded by the dial timeout).
	// The live-cluster TOXIN_AUTH test below exercises them in full.
	credCases := []struct {
		member   string
		endpoint string
		username string
		password string
	}{
		{"https://user:pass@etcd1:2379", "https://etcd1:2379", "user", "pass"},
		{"http://u:p@etcd1", "http://etcd1", "u", "p"},
	}
	for _, c := range credCases {
		m, err := parseMember(c.member, "http")
		if err != nil {
			t.Errorf("parseMember(%q): %v", c.member, err)
			continue
		}
		if m.endpoint != c.endpoint || m.username != c.username || m.password != c.password || !m.hasCreds {
			t.Errorf("parseMember(%q) = %+v, want endpoint %s credentials %s/%s",
				c.member, m, c.endpoint, c.username, c.password)
		}
	}
}

// etcd.Client INPUT: default the scheme of a member without one to http,
// or to https when TLS material is configured.
func TestNewDefaultsSchemeToHTTPSWhenTLSMaterialConfigured(t *testing.T) {
	clearEtcdEnv(t)
	dir := t.TempDir()
	caPath, _ := writeTestCert(t, filepath.Join(dir, "ca"))
	t.Setenv("TOXIN_SSL_CA", caPath)

	c, err := New("127.0.0.1:2379")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	want := []string{"https://127.0.0.1:2379"}
	if got := c.Endpoints(); !reflect.DeepEqual(got, want) {
		t.Errorf("endpoints = %v, want %v", got, want)
	}

	// Without TLS material the default is http, and an explicit scheme
	// always stands.
	t.Setenv("TOXIN_SSL_CA", "")
	c2, err := New("127.0.0.1:2379")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c2.Close()
	if got := c2.Endpoints(); !reflect.DeepEqual(got, []string{"http://127.0.0.1:2379"}) {
		t.Errorf("endpoints = %v, want the http default", got)
	}
}

// etcd.Client INPUT: load the contents of TOXIN_SSL_CA, TOXIN_SSL_CERT and
// TOXIN_SSL_KEY as the TLS credentials when set, concatenating multiple CA
// certificates with newlines.
func TestNewLoadsTLSCredentialsFromEnv(t *testing.T) {
	clearEtcdEnv(t)
	dir := t.TempDir()
	ca1, _ := writeTestCert(t, filepath.Join(dir, "ca1"))
	ca2, _ := writeTestCert(t, filepath.Join(dir, "ca2"))
	cert, key := writeTestCert(t, filepath.Join(dir, "client"))

	t.Setenv("TOXIN_SSL_CA", ca1+","+ca2)
	t.Setenv("TOXIN_SSL_CERT", cert)
	t.Setenv("TOXIN_SSL_KEY", key)

	c, err := New("127.0.0.1:2379")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	if c.tls == nil {
		t.Fatal("client carries no TLS config, want the loaded credentials")
	}
	if got := len(c.tls.RootCAs.Subjects()); got != 2 {
		t.Errorf("CA pool holds %d certificates, want both concatenated CAs", got)
	}
	if len(c.tls.Certificates) != 1 {
		t.Errorf("client certificate pool holds %d certificates, want the loaded pair", len(c.tls.Certificates))
	}

	// A file that cannot be read is an error.
	t.Setenv("TOXIN_SSL_CA", filepath.Join(dir, "missing.pem"))
	if _, err := New("127.0.0.1:2379"); err == nil {
		t.Error("New with an unreadable TOXIN_SSL_CA = no error, want a load failure")
	}
}

// etcd.Client INPUT: when TOXIN_AUTH carries no colon, log a warning and
// ignore the incomplete credentials.
func TestNewWarnsAndIgnoresIncompleteTOXINAUTH(t *testing.T) {
	clearEtcdEnv(t)
	warnings := captureWarnings(t)
	t.Setenv("TOXIN_AUTH", "justauser")

	c, err := New(testutil.Etcd(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	if c.username != "" || c.password != "" {
		t.Errorf("credentials = %q/%q, want the incomplete value ignored", c.username, c.password)
	}
	if !strings.Contains(warnings.String(), "TOXIN_AUTH") {
		t.Errorf("warning %q does not mention TOXIN_AUTH", warnings.String())
	}

	// The client without credentials still serves requests.
	if _, err := c.Put(context.Background(), "/client-warn/k", "v", 0); err != nil {
		t.Errorf("Put after ignored credentials: %v", err)
	}
}

// enableAuth turns on authentication for the cluster behind endpoint with
// the root user "root"/"sekretpw", disabling auth and removing the user
// again when the test ends.
func enableAuth(t *testing.T, endpoint string) {
	t.Helper()
	ctx := context.Background()

	admin, err := clientv3.New(clientv3.Config{Endpoints: []string{endpoint}})
	if err != nil {
		t.Fatalf("clientv3.New: %v", err)
	}
	defer admin.Close()
	if _, err := admin.Auth.UserAdd(ctx, "root", "sekretpw"); err != nil && !alreadyExists(err) {
		t.Fatalf("adding the root user: %v", err)
	}
	if _, err := admin.Auth.UserGrantRole(ctx, "root", "root"); err != nil && !alreadyExists(err) {
		t.Fatalf("granting the root role: %v", err)
	}
	if _, err := admin.Auth.AuthEnable(ctx); err != nil {
		t.Fatalf("enabling auth: %v", err)
	}
	t.Cleanup(func() {
		root, err := clientv3.New(clientv3.Config{
			Endpoints: []string{endpoint},
			Username:  "root",
			Password:  "sekretpw",
		})
		if err != nil {
			return
		}
		defer root.Close()
		_, _ = root.Auth.AuthDisable(context.Background())
		_, _ = root.Auth.UserDelete(context.Background(), "root")
	})
}

// etcd.Client INPUT: when TOXIN_AUTH carries user:password credentials,
// use them for basic authentication.
func TestNewUsesTOXINAUTHCredentialsForBasicAuthentication(t *testing.T) {
	clearEtcdEnv(t)
	endpoint := testutil.Etcd(t)
	ctx := context.Background()

	// The unauthenticated client must face a cluster demanding basic
	// authentication.
	plain, err := New(endpoint)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer plain.Close()
	enableAuth(t, endpoint)

	// Without credentials the cluster refuses the request, so the server
	// is really demanding basic authentication.
	if _, err := plain.Put(ctx, "/client-auth/k", "v", 0); err == nil {
		t.Fatal("unauthenticated Put succeeded, want the cluster to refuse it")
	}

	t.Setenv("TOXIN_AUTH", "root:sekretpw")
	authed, err := New(endpoint)
	if err != nil {
		t.Fatalf("New with TOXIN_AUTH: %v", err)
	}
	defer authed.Close()
	if _, err := authed.Put(ctx, "/client-auth/k", "authenticated", 0); err != nil {
		t.Fatalf("authenticated Put: %v", err)
	}
	entries, err := authed.Read(ctx, "/client-auth/k", false)
	if err != nil || len(entries) != 1 || entries[0].Value != "authenticated" {
		t.Errorf("authenticated Read = %+v, %v; want the written value back", summarize(entries), err)
	}
}

// etcd.Client INPUT: a member of the form
// scheme://user:password@host:port carries basic authentication
// credentials, which are used for the client's requests.
func TestNewUsesMemberCredentialsForBasicAuthentication(t *testing.T) {
	clearEtcdEnv(t)
	endpoint := testutil.Etcd(t)
	ctx := context.Background()
	enableAuth(t, endpoint)

	// Without credentials the cluster refuses the request, so the server
	// is really demanding basic authentication.
	plain, err := New(endpoint)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer plain.Close()
	if _, err := plain.Put(ctx, "/member-auth/k", "v", 0); err == nil {
		t.Fatal("unauthenticated Put succeeded, want the cluster to refuse it")
	}

	authed, err := New("http://root:sekretpw@" + strings.TrimPrefix(endpoint, "http://"))
	if err != nil {
		t.Fatalf("New with member credentials: %v", err)
	}
	defer authed.Close()
	if got := authed.Endpoints(); !reflect.DeepEqual(got, []string{endpoint}) {
		t.Errorf("endpoints = %v, want [%s]", got, endpoint)
	}
	if _, err := authed.Put(ctx, "/member-auth/k", "authenticated", 0); err != nil {
		t.Fatalf("authenticated Put: %v", err)
	}
	entries, err := authed.Read(ctx, "/member-auth/k", false)
	if err != nil || len(entries) != 1 || entries[0].Value != "authenticated" {
		t.Errorf("authenticated Read = %+v, %v; want the written value back", summarize(entries), err)
	}
}

func alreadyExists(err error) bool {
	s := err.Error()
	return strings.Contains(s, "already exist") || strings.Contains(s, "already has") || strings.Contains(s, "granted")
}

// etcd.Client INVARIANT: a five second deadline on every single-request
// call, retried about three times, so an unreachable cluster surfaces
// failure after roughly fifteen seconds rather than five. 192.0.2.1 is
// TEST-NET-1, reserved and never reachable.
func TestUnreachableClusterFailsAfterRoughlyFifteenSeconds(t *testing.T) {
	clearEtcdEnv(t)
	c, err := New("192.0.2.1:2379")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	start := time.Now()
	_, err = c.Read(context.Background(), "/k", false)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("Read against an unreachable cluster succeeded")
	}
	if elapsed < 10*time.Second {
		t.Errorf("failure after %s, want roughly fifteen seconds (at least two retries past the first deadline)", elapsed)
	}
	if elapsed > 30*time.Second {
		t.Errorf("failure after %s, want the retries bounded near fifteen seconds", elapsed)
	}
}

// captureWarnings redirects this layer's warnings into a buffer for the
// duration of the test.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := warningLogger
	warningLogger = log.New(&buf, "", 0)
	t.Cleanup(func() { warningLogger = old })
	return &buf
}

// writeTestCert writes a fresh self-signed certificate and key in PEM form
// as <name>.crt and <name>.key under dir, returning their paths.
func writeTestCert(t *testing.T, name string) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: filepath.Base(name)},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}
	certPath = name + ".crt"
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("writing certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshaling key: %v", err)
	}
	keyPath = name + ".key"
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("writing key: %v", err)
	}
	return certPath, keyPath
}
