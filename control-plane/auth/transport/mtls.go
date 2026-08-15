package transport

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
)

// ServerTLSConfigInput configures the API Server gRPC listener's TLS posture.
// It mirrors the production fail-closed rules established by ADR-043 (slice
// 22) and extends them with the client-certificate requirement added by
// ADR-045 (slice 23).
type ServerTLSConfigInput struct {
	// CertificateFile is the path to the PEM-encoded server certificate.
	// Required.
	CertificateFile string
	// PrivateKeyFile is the path to the PEM-encoded server private key.
	// Required.
	PrivateKeyFile string
	// ClientCAPath is the path to a PEM bundle of certificate authorities that
	// sign client certificates. Optional; when empty the server falls back to
	// tls.NoClientCert, which is appropriate only for development.
	ClientCAPath string
	// RequireClientCert, when true, makes the server reject any handshake that
	// does not present a certificate signed by ClientCAPath. The slice
	// refuses to start a production deployment with this set to false.
	RequireClientCert bool
	// MinVersion is the minimum TLS version. Defaults to TLS 1.2.
	MinVersion uint16
}

// ClientTLSConfigInput configures the Console BFF's outbound gRPC TLS posture.
// It mirrors the server-only TLS posture established by slice 22 and adds the
// client-certificate requirement added by ADR-045.
type ClientTLSConfigInput struct {
	// CertificateFile is the path to the PEM-encoded client certificate.
	// Optional; when empty the client presents no certificate. Slice 23
	// refuses to start a production deployment that omits this.
	CertificateFile string
	// PrivateKeyFile is the path to the PEM-encoded client private key.
	// Optional; required when CertificateFile is set.
	PrivateKeyFile string
	// CAPath is the path to a PEM bundle of certificate authorities that
	// sign the server certificate. Required.
	CAPath string
	// ServerName pins the server certificate SAN. Required.
	ServerName string
	// MinVersion is the minimum TLS version. Defaults to TLS 1.2.
	MinVersion uint16
}

// ErrEmptyCertificatePath is returned when a required certificate or key path
// is empty. Callers translate this into a fail-closed startup error.
var ErrEmptyCertificatePath = errors.New("transport: certificate path must not be empty")

// ErrEmptyClientCAPath is returned when ClientCAPath is empty but
// RequireClientCert is true. Slice 23 fails closed on this combination.
var ErrEmptyClientCAPath = errors.New("transport: client CA path must be configured when client certificates are required")

// ErrEmptyServerCAPath is returned when a client configuration is missing
// its CAPath.
var ErrEmptyServerCAPath = errors.New("transport: server CA path must be configured")

// ErrEmptyServerName is returned when a client configuration is missing its
// ServerName pin.
var ErrEmptyServerName = errors.New("transport: server name must be configured")

// ServerTLSConfig builds the *tls.Config used by the API Server gRPC
// listener. It loads the server certificate, optionally configures a client
// CA pool, and sets ClientAuth according to RequireClientCert.
func ServerTLSConfig(input ServerTLSConfigInput) (*tls.Config, error) {
	if input.CertificateFile == "" || input.PrivateKeyFile == "" {
		return nil, ErrEmptyCertificatePath
	}
	certificate, err := tls.LoadX509KeyPair(input.CertificateFile, input.PrivateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load server TLS identity: %w", err)
	}
	minVersion := input.MinVersion
	if minVersion == 0 {
		minVersion = tls.VersionTLS12
	}
	config := &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   minVersion,
	}
	if input.RequireClientCert {
		if input.ClientCAPath == "" {
			return nil, ErrEmptyClientCAPath
		}
		pool, err := loadCAPool(input.ClientCAPath)
		if err != nil {
			return nil, err
		}
		config.ClientCAs = pool
		config.ClientAuth = tls.RequireAndVerifyClientCert
	} else if input.ClientCAPath != "" {
		pool, err := loadCAPool(input.ClientCAPath)
		if err != nil {
			return nil, err
		}
		config.ClientCAs = pool
		config.ClientAuth = tls.VerifyClientCertIfGiven
	}
	return config, nil
}

// ClientTLSConfig builds the *tls.Config used by the Console BFF to dial
// the API Server. It validates the server certificate against CAPath and
// presents a client certificate when one is configured.
func ClientTLSConfig(input ClientTLSConfigInput) (*tls.Config, error) {
	if input.CAPath == "" {
		return nil, ErrEmptyServerCAPath
	}
	if input.ServerName == "" {
		return nil, ErrEmptyServerName
	}
	pool, err := loadCAPool(input.CAPath)
	if err != nil {
		return nil, err
	}
	minVersion := input.MinVersion
	if minVersion == 0 {
		minVersion = tls.VersionTLS12
	}
	config := &tls.Config{
		RootCAs:    pool,
		ServerName: input.ServerName,
		MinVersion: minVersion,
	}
	if input.CertificateFile != "" || input.PrivateKeyFile != "" {
		if input.CertificateFile == "" || input.PrivateKeyFile == "" {
			return nil, ErrEmptyCertificatePath
		}
		certificate, err := tls.LoadX509KeyPair(input.CertificateFile, input.PrivateKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client TLS identity: %w", err)
		}
		config.Certificates = []tls.Certificate{certificate}
	}
	return config, nil
}

func loadCAPool(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read CA bundle %q: %w", path, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("parse CA bundle %q: no certificates found", path)
	}
	return pool, nil
}
