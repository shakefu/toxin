package etcd

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
)

// loadTLS loads the TLS credentials named by TOXIN_SSL_CA, TOXIN_SSL_CERT
// and TOXIN_SSL_KEY. An empty or unset variable contributes nothing; when
// none is set no TLS material is configured and the call returns a nil
// config. TOXIN_SSL_CA may name several certificate files, in which case
// their contents are concatenated with newlines into one CA pool. A client
// certificate needs both TOXIN_SSL_CERT and TOXIN_SSL_KEY; an incomplete
// pair is ignored with a warning, as pyconfig did.
func loadTLS() (cfg *tls.Config, configured bool, err error) {
	var cas []string
	for _, path := range strings.Split(os.Getenv("TOXIN_SSL_CA"), ",") {
		if path = strings.TrimSpace(path); path == "" {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, false, fmt.Errorf("reading TOXIN_SSL_CA: %w", err)
		}
		cas = append(cas, string(b))
	}
	certPEM, keyPEM := "", ""
	if path := os.Getenv("TOXIN_SSL_CERT"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, false, fmt.Errorf("reading TOXIN_SSL_CERT: %w", err)
		}
		certPEM = string(b)
	}
	if path := os.Getenv("TOXIN_SSL_KEY"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, false, fmt.Errorf("reading TOXIN_SSL_KEY: %w", err)
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
			return nil, false, fmt.Errorf("TOXIN_SSL_CA holds no PEM certificates")
		}
		cfg.RootCAs = pool
	}
	switch {
	case certPEM != "" && keyPEM != "":
		pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
		if err != nil {
			return nil, false, fmt.Errorf("loading TOXIN_SSL_CERT and TOXIN_SSL_KEY: %w", err)
		}
		cfg.Certificates = []tls.Certificate{pair}
	case certPEM != "" || keyPEM != "":
		warningLogger.Printf("warning: both TOXIN_SSL_CERT and TOXIN_SSL_KEY are required for client authentication; ignoring the incomplete certificate")
	}
	return cfg, true, nil
}
