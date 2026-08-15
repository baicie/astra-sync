package transport

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
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestServerTLSConfigRejectsEmptyPaths covers the production fail-closed
// gate: an empty certificate or key path returns ErrEmptyCertificatePath.
func TestServerTLSConfigRejectsEmptyPaths(t *testing.T) {
	if _, err := ServerTLSConfig(ServerTLSConfigInput{}); !errors.Is(err, ErrEmptyCertificatePath) {
		t.Fatalf("expected ErrEmptyCertificatePath, got %v", err)
	}
	if _, err := ServerTLSConfig(ServerTLSConfigInput{CertificateFile: "only"}); !errors.Is(err, ErrEmptyCertificatePath) {
		t.Fatalf("expected ErrEmptyCertificatePath, got %v", err)
	}
}

// TestServerTLSConfigRequiresClientCAPath covers the production fail-closed
// gate: RequireClientCert=true without ClientCAPath returns
// ErrEmptyClientCAPath.
func TestServerTLSConfigRequiresClientCAPath(t *testing.T) {
	files := writeTestCertificatePair(t)
	_, err := ServerTLSConfig(ServerTLSConfigInput{
		CertificateFile:   files.certificate,
		PrivateKeyFile:    files.privateKey,
		RequireClientCert: true,
	})
	if !errors.Is(err, ErrEmptyClientCAPath) {
		t.Fatalf("expected ErrEmptyClientCAPath, got %v", err)
	}
}

// TestServerTLSConfigLoadsServerCertificate covers the happy path that
// production deployments use: the server certificate loads and ClientCAs is
// populated.
func TestServerTLSConfigLoadsServerCertificate(t *testing.T) {
	files := writeTestCertificatePair(t)
	config, err := ServerTLSConfig(ServerTLSConfigInput{
		CertificateFile:   files.certificate,
		PrivateKeyFile:    files.privateKey,
		ClientCAPath:      files.clientCA,
		RequireClientCert: true,
	})
	if err != nil {
		t.Fatalf("build server TLS config: %v", err)
	}
	if config.MinVersion != tls.VersionTLS12 {
		t.Fatalf("expected min TLS 1.2, got %x", config.MinVersion)
	}
	if config.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("expected RequireAndVerifyClientCert, got %v", config.ClientAuth)
	}
	if config.ClientCAs == nil {
		t.Fatalf("expected ClientCAs to be populated")
	}
}

// TestServerTLSConfigDefaultsToNoClientCert covers the development posture:
// when neither RequireClientCert nor ClientCAPath is set, the server falls
// back to tls.NoClientCert and TLS 1.2.
func TestServerTLSConfigDefaultsToNoClientCert(t *testing.T) {
	files := writeTestCertificatePair(t)
	config, err := ServerTLSConfig(ServerTLSConfigInput{
		CertificateFile: files.certificate,
		PrivateKeyFile:  files.privateKey,
	})
	if err != nil {
		t.Fatalf("build server TLS config: %v", err)
	}
	if config.ClientAuth != tls.NoClientCert {
		t.Fatalf("expected NoClientCert, got %v", config.ClientAuth)
	}
	if config.MinVersion != tls.VersionTLS12 {
		t.Fatalf("expected min TLS 1.2, got %x", config.MinVersion)
	}
}

// TestServerTLSConfigRejectsMissingFiles covers the IO error path used by
// the API Server main.go startup checks.
func TestServerTLSConfigRejectsMissingFiles(t *testing.T) {
	if _, err := ServerTLSConfig(ServerTLSConfigInput{
		CertificateFile: filepath.Join(t.TempDir(), "missing.crt"),
		PrivateKeyFile:  filepath.Join(t.TempDir(), "missing.key"),
	}); err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}

// TestClientTLSConfigRequiresCAAndServerName covers the production
// fail-closed gate: missing CAPath or ServerName returns the right error.
func TestClientTLSConfigRequiresCAAndServerName(t *testing.T) {
	if _, err := ClientTLSConfig(ClientTLSConfigInput{}); !errors.Is(err, ErrEmptyServerCAPath) {
		t.Fatalf("expected ErrEmptyServerCAPath, got %v", err)
	}
	if _, err := ClientTLSConfig(ClientTLSConfigInput{CAPath: "x"}); !errors.Is(err, ErrEmptyServerName) {
		t.Fatalf("expected ErrEmptyServerName, got %v", err)
	}
}

// TestClientTLSConfigRequiresPairedClientCertificate covers the production
// fail-closed gate: a client cert without a matching key returns
// ErrEmptyCertificatePath.
func TestClientTLSConfigRequiresPairedClientCertificate(t *testing.T) {
	files := writeTestCertificatePair(t)
	if _, err := ClientTLSConfig(ClientTLSConfigInput{
		CAPath:          files.clientCA,
		ServerName:      "api-server",
		CertificateFile: "only-cert",
	}); !errors.Is(err, ErrEmptyCertificatePath) {
		t.Fatalf("expected ErrEmptyCertificatePath, got %v", err)
	}
}

// TestClientTLSConfigLoadsServerCA covers the happy path that production
// deployments use.
func TestClientTLSConfigLoadsServerCA(t *testing.T) {
	files := writeTestCertificatePair(t)
	config, err := ClientTLSConfig(ClientTLSConfigInput{
		CAPath:          files.clientCA,
		ServerName:      "api-server",
		CertificateFile: files.clientCert,
		PrivateKeyFile:  files.clientKey,
	})
	if err != nil {
		t.Fatalf("build client TLS config: %v", err)
	}
	if config.ServerName != "api-server" {
		t.Fatalf("expected server name api-server, got %q", config.ServerName)
	}
	if len(config.Certificates) != 1 {
		t.Fatalf("expected client certificate to be loaded, got %d", len(config.Certificates))
	}
	if config.RootCAs == nil {
		t.Fatalf("expected RootCAs to be populated")
	}
}

// TestMTLSEndToEnd exercises the full handshake over a real TCP listener.
// It generates a CA, a server certificate, and a client certificate, then
// checks three outcomes:
//   - the client with a valid certificate completes the handshake;
//   - the client without a certificate fails the handshake;
//   - the client with a certificate signed by an unknown CA fails the
//     handshake.
func TestMTLSEndToEnd(t *testing.T) {
	trusted := writeTestCertificatePair(t)
	foreign := writeTestCertificatePair(t)

	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{loadCertificate(t, trusted.certificate, trusted.privateKey)},
		ClientCAs:    loadPool(t, trusted.clientCA),
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	address := listener.Addr().String()
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			tlsConn := conn.(*tls.Conn)
			if handshakeErr := tlsConn.Handshake(); handshakeErr != nil {
				_ = tlsConn.Close()
				continue
			}
			_, _ = io.Copy(io.Discard, tlsConn)
			_ = tlsConn.Close()
		}
	}()

	// Happy path: client presents a certificate signed by the trusted CA.
	runHandshake(t, address, trusted, true)

	// Negative path: client does not present a certificate.
	runHandshake(t, address, certificatePair{}, false)

	// Negative path: client presents a certificate signed by a different CA.
	runHandshake(t, address, foreign, false)
}

func runHandshake(t *testing.T, address string, pair certificatePair, expectSuccess bool) {
	t.Helper()
	var certificates []tls.Certificate
	if pair.certificate != "" {
		certificates = []tls.Certificate{loadCertificate(t, pair.certificate, pair.privateKey)}
	}
	config := &tls.Config{
		Certificates: certificates,
		ServerName:   "api-server",
		MinVersion:   tls.VersionTLS12,
	}
	if pair.clientCA != "" {
		config.RootCAs = loadPool(t, pair.clientCA)
	}
	dialer := &tls.Dialer{Config: config}
	conn, err := dialer.Dial("tcp", address)
	if expectSuccess {
		if err != nil {
			t.Fatalf("expected successful handshake: %v", err)
		}
		_ = conn.Close()
		return
	}
	if err == nil {
		_ = conn.Close()
		t.Fatalf("expected handshake failure, got success")
	}
}

// TestMTLSClientRejectsUnknownServerCA covers the inverse case: a client
// configured with a CA that does not sign the server certificate refuses to
// complete the handshake.
func TestMTLSClientRejectsUnknownServerCA(t *testing.T) {
	trusted := writeTestCertificatePair(t)
	foreign := writeTestCertificatePair(t)

	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{loadCertificate(t, trusted.certificate, trusted.privateKey)},
		ClientAuth:   tls.NoClientCert,
		MinVersion:   tls.VersionTLS12,
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	address := listener.Addr().String()
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			tlsConn := conn.(*tls.Conn)
			_ = tlsConn.Handshake()
			_ = tlsConn.Close()
		}
	}()

	dialer := &tls.Dialer{
		Config: &tls.Config{
			RootCAs:    loadPool(t, foreign.clientCA),
			ServerName: "api-server",
			MinVersion: tls.VersionTLS12,
		},
	}
	if _, err := dialer.Dial("tcp", address); err == nil {
		t.Fatalf("expected dialer to reject server cert from foreign CA")
	}
}

// --- Test fixtures ----------------------------------------------------------

type certificatePair struct {
	certificate string
	privateKey  string
	clientCA    string
	clientCert  string
	clientKey   string
}

type generatedCertificate struct {
	certificate *x509.Certificate
	privateKey  *ecdsa.PrivateKey
}

// writeTestCertificatePair generates a CA, a server certificate, and a
// matching client certificate in a temporary directory. The pair satisfies
// the production fail-closed checks because every field is a real,
// on-disk, readable PEM file.
func writeTestCertificatePair(t *testing.T) certificatePair {
	t.Helper()
	directory := t.TempDir()
	caCert, caKey := generateCertificate(t, true, "", "test-ca")
	serverCert, serverKey := issueFromCA(t, caCert, caKey, true, "api-server", "api-server")
	clientCert, clientKey := issueFromCA(t, caCert, caKey, false, "", "console-client")

	caPath := filepath.Join(directory, "ca.crt")
	writePEM(t, caPath, "CERTIFICATE", caCert.Raw)

	serverCertPath := filepath.Join(directory, "server.crt")
	serverKeyPath := filepath.Join(directory, "server.key")
	writePEM(t, serverCertPath, "CERTIFICATE", serverCert.Raw)
	writeECKey(t, serverKeyPath, serverKey)

	clientCertPath := filepath.Join(directory, "client.crt")
	clientKeyPath := filepath.Join(directory, "client.key")
	writePEM(t, clientCertPath, "CERTIFICATE", clientCert.Raw)
	writeECKey(t, clientKeyPath, clientKey)

	return certificatePair{
		certificate: serverCertPath,
		privateKey:  serverKeyPath,
		clientCA:    caPath,
		clientCert:  clientCertPath,
		clientKey:   clientKeyPath,
	}
}

func generateCertificate(t *testing.T, isCA bool, dnsName, commonName string) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("random serial: %v", err)
	}
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	if isCA {
		template.IsCA = true
	} else if dnsName != "" {
		template.DNSNames = []string{dnsName}
	}
	certificateBytes, err := x509.CreateCertificate(rand.Reader, template, template, privateKey.Public(), privateKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certificate, err := x509.ParseCertificate(certificateBytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return certificate, privateKey
}

func issueFromCA(t *testing.T, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, isServer bool, dnsName, commonName string) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("random serial: %v", err)
	}
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		BasicConstraintsValid: true,
	}
	if isServer {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		if dnsName != "" {
			template.DNSNames = []string{dnsName}
		}
	} else {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	certificateBytes, err := x509.CreateCertificate(rand.Reader, template, caCert, privateKey.Public(), caKey)
	if err != nil {
		t.Fatalf("issue certificate: %v", err)
	}
	certificate, err := x509.ParseCertificate(certificateBytes)
	if err != nil {
		t.Fatalf("parse issued certificate: %v", err)
	}
	return certificate, privateKey
}

func writePEM(t *testing.T, path, block string, der []byte) {
	t.Helper()
	contents := pem.EncodeToMemory(&pem.Block{Type: block, Bytes: der})
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write pem %q: %v", path, err)
	}
}

func writeECKey(t *testing.T, path string, key *ecdsa.PrivateKey) {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	writePEM(t, path, "EC PRIVATE KEY", der)
}

func loadCertificate(t *testing.T, certificate, key string) tls.Certificate {
	t.Helper()
	pair, err := tls.LoadX509KeyPair(certificate, key)
	if err != nil {
		t.Fatalf("load key pair: %v", err)
	}
	return pair
}

func loadPool(t *testing.T, path string) *x509.CertPool {
	t.Helper()
	pem, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CA bundle %q: %v", path, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		t.Fatalf("parse CA bundle %q", path)
	}
	return pool
}
