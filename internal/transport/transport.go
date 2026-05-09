package transport

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"time"

	"github.com/quic-go/quic-go"
	"tunneledge/pkg/errs"
)

const (
	MsgAuth         byte = 0x01
	MsgAuthResponse byte = 0x02
	MsgData         byte = 0x03
	MsgHeartbeat    byte = 0x04
	MsgClose        byte = 0x05

	AuthStatusOK    byte = 0x00
	AuthStatusError byte = 0x01

	maxTokenLen     = 4096
	maxTunnelIDLen  = 256
	maxPublicURLLen = 512
)

type AuthMessage struct {
	Token string
}

type AuthResponseMessage struct {
	Status    byte
	TunnelID  string
	PublicURL string
}

func EncodeAuth(w io.Writer, token string) error {
	tokenBytes := []byte(token)
	if len(tokenBytes) > maxTokenLen {
		return errs.New(errs.CodeInvalidArg, "token too long")
	}

	buf := make([]byte, 3+len(tokenBytes))
	buf[0] = MsgAuth
	binary.BigEndian.PutUint16(buf[1:3], uint16(len(tokenBytes)))
	copy(buf[3:], tokenBytes)

	_, err := w.Write(buf)
	return err
}

func DecodeAuth(r io.Reader) (*AuthMessage, error) {
	header := make([]byte, 3)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("failed to read auth header: %w", err)
	}

	if header[0] != MsgAuth {
		return nil, errs.New(errs.CodeInvalidArg, fmt.Sprintf("expected auth message type 0x01, got 0x%02x", header[0]))
	}

	tokenLen := binary.BigEndian.Uint16(header[1:3])
	if tokenLen > maxTokenLen {
		return nil, errs.New(errs.CodeInvalidArg, "token too long")
	}

	tokenBuf := make([]byte, tokenLen)
	if _, err := io.ReadFull(r, tokenBuf); err != nil {
		return nil, fmt.Errorf("failed to read auth token: %w", err)
	}

	return &AuthMessage{Token: string(tokenBuf)}, nil
}

func EncodeAuthResponse(w io.Writer, status byte, tunnelID string, publicURL string) error {
	idBytes := []byte(tunnelID)
	urlBytes := []byte(publicURL)
	if len(idBytes) > maxTunnelIDLen {
		return errs.New(errs.CodeInvalidArg, "tunnel_id too long")
	}
	if len(urlBytes) > maxPublicURLLen {
		return errs.New(errs.CodeInvalidArg, "public_url too long")
	}

	buf := make([]byte, 6+len(idBytes)+len(urlBytes))
	buf[0] = MsgAuthResponse
	buf[1] = status
	binary.BigEndian.PutUint16(buf[2:4], uint16(len(idBytes)))
	binary.BigEndian.PutUint16(buf[4:6], uint16(len(urlBytes)))
	copy(buf[6:], idBytes)
	copy(buf[6+len(idBytes):], urlBytes)

	_, err := w.Write(buf)
	return err
}

func DecodeAuthResponse(r io.Reader) (*AuthResponseMessage, error) {
	header := make([]byte, 6)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("failed to read auth response header: %w", err)
	}

	if header[0] != MsgAuthResponse {
		return nil, errs.New(errs.CodeInvalidArg, fmt.Sprintf("expected auth response type 0x02, got 0x%02x", header[0]))
	}

	status := header[1]
	idLen := binary.BigEndian.Uint16(header[2:4])
	urlLen := binary.BigEndian.Uint16(header[4:6])

	if idLen > maxTunnelIDLen {
		return nil, errs.New(errs.CodeInvalidArg, "tunnel_id too long")
	}
	if urlLen > maxPublicURLLen {
		return nil, errs.New(errs.CodeInvalidArg, "public_url too long")
	}

	var tunnelID string
	if idLen > 0 {
		idBuf := make([]byte, idLen)
		if _, err := io.ReadFull(r, idBuf); err != nil {
			return nil, fmt.Errorf("failed to read auth response tunnel_id: %w", err)
		}
		tunnelID = string(idBuf)
	}

	var publicURL string
	if urlLen > 0 {
		urlBuf := make([]byte, urlLen)
		if _, err := io.ReadFull(r, urlBuf); err != nil {
			return nil, fmt.Errorf("failed to read auth response public_url: %w", err)
		}
		publicURL = string(urlBuf)
	}

	return &AuthResponseMessage{Status: status, TunnelID: tunnelID, PublicURL: publicURL}, nil
}

func EncodeHeartbeat(w io.Writer) error {
	_, err := w.Write([]byte{MsgHeartbeat})
	return err
}

func ReadMessageType(r io.Reader) (byte, error) {
	buf := make([]byte, 1)
	_, err := io.ReadFull(r, buf)
	if err != nil {
		return 0, err
	}
	return buf[0], nil
}

func GenerateSelfSignedTLSConfig() (*tls.Config, error) {
	return generateWildcardQUICTLSConfig("localhost")
}

func GenerateWildcardSelfSignedTLSConfig(baseDomain string) (*tls.Config, error) {
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

	return buildPublicTLSConfig(tlsCert), nil
}

func generateWildcardQUICTLSConfig(baseDomain string) (*tls.Config, error) {
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

	return buildQUICTLSConfig(tlsCert), nil
}

func LoadTLSConfig(certFile, keyFile string) (*tls.Config, error) {
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

type SNIRouterFunc func(hostname string) bool

func PublicTLSConfigWithSNI(certFile, keyFile string, router SNIRouterFunc) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS cert/key: %w", err)
	}
	return publicTLSConfigWithSNICallback(cert, router), nil
}

func PublicTLSConfigWithSNISelfSigned(baseDomain string, router SNIRouterFunc) (*tls.Config, error) {
	cfg, err := GenerateWildcardSelfSignedTLSConfig(baseDomain)
	if err != nil {
		return nil, err
	}
	cert := cfg.Certificates[0]
	return publicTLSConfigWithSNICallback(cert, router), nil
}

func publicTLSConfigWithSNICallback(cert tls.Certificate, router SNIRouterFunc) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			if hello.ServerName == "" {
				return nil, fmt.Errorf("SNI hostname required")
			}
			if !router(hello.ServerName) {
				return nil, fmt.Errorf("unknown tunnel host: %s", hello.ServerName)
			}
			return &tls.Config{
				Certificates: []tls.Certificate{cert},
				MinVersion:   tls.VersionTLS12,
			}, nil
		},
	}
}

func ClientTLSConfig() *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"tunneledge"},
		MinVersion:         tls.VersionTLS13,
	}
}

func QUICDial(ctx context.Context, addr string, tlsCfg *tls.Config) (*quic.Conn, error) {
	conn, err := quic.DialAddr(ctx, addr, tlsCfg, &quic.Config{
		MaxIdleTimeout:  60 * time.Second,
		KeepAlivePeriod: 15 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to dial QUIC %s: %w", addr, err)
	}
	return conn, nil
}

func QUICListen(addr string, tlsCfg *tls.Config) (*quic.Listener, error) {
	listener, err := quic.ListenAddr(addr, tlsCfg, &quic.Config{
		MaxIdleTimeout:  60 * time.Second,
		KeepAlivePeriod: 15 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to listen QUIC on %s: %w", addr, err)
	}
	return listener, nil
}
