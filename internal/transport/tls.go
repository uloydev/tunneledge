package transport

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"
)

func GenerateSelfSignedQUICTLSConfig() (*tls.Config, error) {
	return generateWildcardCert(buildQUICTLSConfig, "localhost")
}

func GenerateWildcardSelfSignedTLSConfig(baseDomain string) (*tls.Config, error) {
	return generateWildcardCert(buildPublicTLSConfig, baseDomain)
}

func LoadQUICTLSConfig(certFile, keyFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS cert/key: %w", err)
	}
	return buildQUICTLSConfig(cert), nil
}

func LoadPublicTLSConfig(certFile, keyFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS cert/key: %w", err)
	}
	return buildPublicTLSConfig(cert), nil
}

func ClientTLSConfig() *tls.Config {
	// WARNING: InsecureSkipVerify=true disables server certificate validation.
	// This is acceptable for local development but MUST NOT be used in production.
	// Set TLSCAFile in the agent config to enable proper CA validation.
	return &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // intentional dev-only fallback
		NextProtos:         []string{"tunneledge"},
		MinVersion:         tls.VersionTLS13,
	}
}

func ClientTLSConfigWithCA(caFile string) (*tls.Config, error) {
	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA cert: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA cert")
	}

	return &tls.Config{
		RootCAs:    pool,
		NextProtos: []string{"tunneledge"},
		MinVersion: tls.VersionTLS13,
	}, nil
}

type certBuilder func(tls.Certificate) *tls.Config

func generateWildcardCert(build certBuilder, baseDomain string) (*tls.Config, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}

	serialNumber, _ := rand.Int(rand.Reader, big.NewInt(0).Lsh(big.NewInt(1), 128))

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"TunnelEdge Dev"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames: []string{
			baseDomain,
			"*." + baseDomain,
			// Include common Docker service / container hostnames so that
			// agents in the same Compose network can verify the certificate
			// without requiring InsecureSkipVerify.
			"gateway",
		},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	tlsCert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}

	return build(tlsCert), nil
}

func buildQUICTLSConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"tunneledge"},
		MinVersion:   tls.VersionTLS13,
	}
}

func buildPublicTLSConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
}

// LoadMTLSServerConfig returns a TLS config for a server that requires and
// validates client certificates signed by the specified CA.
func LoadMTLSServerConfig(certFile, keyFile, clientCAFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load server cert/key: %w", err)
	}
	caCert, err := os.ReadFile(clientCAFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read client CA cert: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse client CA cert")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		NextProtos:   []string{"tunneledge"},
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// LoadMTLSClientConfig returns a TLS config for a client that presents a
// certificate to the server and validates the server against the provided CA.
func LoadMTLSClientConfig(certFile, keyFile, caFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load client cert/key: %w", err)
	}
	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA cert: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA cert")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		NextProtos:   []string{"tunneledge"},
		MinVersion:   tls.VersionTLS13,
	}, nil
}
