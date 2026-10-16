package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// selfSigned writes a self-signed cert and key for 127.0.0.1 into dir and returns their
// paths plus a pool trusting the cert.
func selfSigned(t *testing.T, dir string) (certFile, keyFile string, pool *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "serverdog test"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		// Fixed validity window: certificate fields, not code under internal/ reading time.
		NotBefore:   time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:    time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool = x509.NewCertPool()
	pool.AddCert(cert)
	return certFile, keyFile, pool
}

// startServe runs serve on a loopback listener and returns its base address.
func startServe(t *testing.T, certFile, keyFile string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		io.WriteString(w, "ok")
	})}
	errc := make(chan error, 1)
	go func() { errc <- serve(srv, ln, certFile, keyFile) }()
	t.Cleanup(func() {
		srv.Close()
		if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("serve: %v", err)
		}
	})
	return ln.Addr().String()
}

func TestServe(t *testing.T) {
	certFile, keyFile, pool := selfSigned(t, t.TempDir())
	tlsClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}

	tests := []struct {
		name          string
		certFile, key string
		scheme, wrong string // scheme that must work, scheme that must not
		wantTLS       bool
	}{
		{name: "plain HTTP when no cert is set", scheme: "http", wrong: "https"},
		{name: "HTTPS when cert and key are set", certFile: certFile, key: keyFile, scheme: "https", wrong: "http", wantTLS: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := startServe(t, tt.certFile, tt.key)

			resp, err := tlsClient.Get(tt.scheme + "://" + addr + "/")
			if err != nil {
				t.Fatalf("GET %s: %v", tt.scheme, err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK || string(body) != "ok" {
				t.Errorf("GET %s = %d %q, want 200 \"ok\"", tt.scheme, resp.StatusCode, body)
			}
			if (resp.TLS != nil) != tt.wantTLS {
				t.Errorf("response TLS = %v, want %v", resp.TLS != nil, tt.wantTLS)
			}

			// The other scheme must not get a page: plain HTTP to a TLS listener is refused
			// (400), TLS to a plain listener fails the handshake.
			resp, err = tlsClient.Get(tt.wrong + "://" + addr + "/")
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					t.Errorf("GET %s = 200, want failure", tt.wrong)
				}
			}
		})
	}
}
