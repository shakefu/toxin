package etcd

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
)

// loadTLS loads the TLS credentials named by caPaths (TOXIN_SSL_CA),
// certPath (TOXIN_SSL_CERT) and keyPath (TOXIN_SSL_KEY); it never reads
// the environment itself. An empty path contributes nothing; when none is
// given no TLS material is configured and the call returns a nil config.
// caPaths may name several comma-separated certificate files, in which
// case their contents are concatenated with newlines into one CA pool. A
// client certificate needs both certPath and keyPath; an incomplete pair is
// ignored with a warning, as pyconfig did.
func loadTLS(caPaths, certPath, keyPath string) (cfg *tls.Config, configured bool, err error) {
	var cas []string
	for _, path := range strings.Split(caPaths, ",") {
		if path = strings.TrimSpace(path); path == "" {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, false, fmt.Errorf("reading CA certificate %q: %w", path, err)
		}
		cas = append(cas, string(b))
	}
	certPEM, keyPEM := "", ""
	if certPath != "" {
		b, err := os.ReadFile(certPath)
		if err != nil {
			return nil, false, fmt.Errorf("reading client certificate %q: %w", certPath, err)
		}
		certPEM = string(b)
	}
	if keyPath != "" {
		b, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, false, fmt.Errorf("reading client key %q: %w", keyPath, err)
		}
		keyPEM = string(b)
	}

	if len(cas) == 0 && certPEM == "" && keyPEM == "" {
		return nil, false, nil
	}
	cfg = &tls.Config{}
	if len(cas) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(strings.Join(cas, "\n"))) {
			return nil, false, fmt.Errorf("CA certificates %q hold no PEM certificates", caPaths)
		}
		cfg.RootCAs = pool
	}
	switch {
	case certPEM != "" && keyPEM != "":
		pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
		if err != nil {
			return nil, false, fmt.Errorf("loading client certificate %q and key %q: %w", certPath, keyPath, err)
		}
		cfg.Certificates = []tls.Certificate{pair}
	case certPEM != "" || keyPEM != "":
		warningLogger.Printf("warning: client certificate %q and key %q must both be set; ignoring the incomplete pair", certPath, keyPath)
	}
	return cfg, true, nil
}
