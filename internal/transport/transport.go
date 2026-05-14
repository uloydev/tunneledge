package transport

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"time"

	"tunneledge/pkg/errs"

	"github.com/quic-go/quic-go"
)

const (
	// Protocol version constants. Increment ProtocolVersion when making
	// incompatible wire-format changes.
	ProtocolVersion uint16 = 2

	// MsgHello is the first frame sent by a connecting agent. It carries the
	// protocol version so the gateway can reject incompatible clients early,
	// before spending time on TLS key-exchange validation.
	MsgHello        byte = 0x00
	MsgAuth         byte = 0x01
	MsgAuthResponse byte = 0x02
	MsgData         byte = 0x03
	MsgHeartbeat    byte = 0x04
	MsgClose        byte = 0x05
	MsgAuthV2       byte = 0x06
	MsgAuthRespV2   byte = 0x07
	MsgStreamLabel  byte = 0x08

	AuthStatusOK           byte = 0x00
	AuthStatusError        byte = 0x01
	AuthStatusVersionError byte = 0x02 // client protocol version not supported

	maxTokenLen     = 4096
	maxTunnelIDLen  = 256
	maxPublicURLLen = 512
	maxLabelLen     = 64
	maxLocalAddrLen = 256
	maxTunnelCount  = 32
	maxVersionLen   = 64
)

// HelloMessage is the connection-level handshake frame. Agents send it as the
// very first message on the QUIC control stream so the gateway can validate
// the protocol version before doing any auth work.
type HelloMessage struct {
	Version       uint16 // must equal ProtocolVersion
	ClientVersion string // human-readable agent version string (e.g. "tunneledge/1.2.3")
}

// EncodeHello writes a MsgHello frame: [0x00][version uint16][clientVer len uint16][clientVer bytes].
func EncodeHello(w io.Writer, clientVersion string) error {
	vb := []byte(clientVersion)
	if len(vb) > maxVersionLen {
		vb = vb[:maxVersionLen]
	}
	buf := make([]byte, 5+len(vb))
	buf[0] = MsgHello
	binary.BigEndian.PutUint16(buf[1:3], ProtocolVersion)
	binary.BigEndian.PutUint16(buf[3:5], uint16(len(vb)))
	copy(buf[5:], vb)
	_, err := w.Write(buf)
	return err
}

// DecodeHello reads a MsgHello frame from r. Returns an error if the frame is
// malformed or carries an unsupported protocol version.
func DecodeHello(r io.Reader) (*HelloMessage, error) {
	// First byte is already consumed by ReadMessageType — start from version.
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("failed to read hello frame: %w", err)
	}
	version := binary.BigEndian.Uint16(header[0:2])
	cvLen := binary.BigEndian.Uint16(header[2:4])
	if cvLen > maxVersionLen {
		return nil, errs.New(errs.CodeInvalidArg, "client version string too long")
	}
	var cv string
	if cvLen > 0 {
		cvBuf := make([]byte, cvLen)
		if _, err := io.ReadFull(r, cvBuf); err != nil {
			return nil, fmt.Errorf("failed to read client version: %w", err)
		}
		cv = string(cvBuf)
	}
	return &HelloMessage{Version: version, ClientVersion: cv}, nil
}

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

type TunnelEntry struct {
	Label     string
	LocalAddr string
}

type AuthV2Message struct {
	Token   string
	Tunnels []TunnelEntry
}

type TunnelHostEntry struct {
	Label    string
	Hostname string
}

type AuthV2ResponseMessage struct {
	Status   byte
	TunnelID string
	Tunnels  []TunnelHostEntry
}

func EncodeAuthV2(w io.Writer, token string, tunnels []TunnelEntry) error {
	tokenBytes := []byte(token)
	if len(tokenBytes) > maxTokenLen {
		return errs.New(errs.CodeInvalidArg, "token too long")
	}
	if len(tunnels) > maxTunnelCount {
		return errs.New(errs.CodeInvalidArg, "too many tunnels")
	}

	size := 3 + len(tokenBytes) + 1
	for _, t := range tunnels {
		size += 1 + len(t.Label) + 2 + len(t.LocalAddr)
	}

	buf := make([]byte, size)
	off := 0
	buf[off] = MsgAuthV2
	off++
	binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(tokenBytes)))
	off += 2
	copy(buf[off:], tokenBytes)
	off += len(tokenBytes)
	buf[off] = byte(len(tunnels))
	off++

	for _, t := range tunnels {
		if len(t.Label) > maxLabelLen {
			return errs.New(errs.CodeInvalidArg, fmt.Sprintf("label too long: %s", t.Label))
		}
		if len(t.LocalAddr) > maxLocalAddrLen {
			return errs.New(errs.CodeInvalidArg, fmt.Sprintf("local_addr too long: %s", t.LocalAddr))
		}
		buf[off] = byte(len(t.Label))
		off++
		copy(buf[off:], t.Label)
		off += len(t.Label)
		binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(t.LocalAddr)))
		off += 2
		copy(buf[off:], t.LocalAddr)
		off += len(t.LocalAddr)
	}

	_, err := w.Write(buf[:off])
	return err
}

func DecodeAuthV2(r io.Reader) (*AuthV2Message, error) {
	header := make([]byte, 3)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("failed to read auth v2 header: %w", err)
	}

	if header[0] != MsgAuthV2 {
		return nil, errs.New(errs.CodeInvalidArg, fmt.Sprintf("expected auth v2 message type 0x06, got 0x%02x", header[0]))
	}

	tokenLen := binary.BigEndian.Uint16(header[1:3])
	if tokenLen > maxTokenLen {
		return nil, errs.New(errs.CodeInvalidArg, "token too long")
	}

	tokenBuf := make([]byte, tokenLen)
	if _, err := io.ReadFull(r, tokenBuf); err != nil {
		return nil, fmt.Errorf("failed to read auth v2 token: %w", err)
	}

	countBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, countBuf); err != nil {
		return nil, fmt.Errorf("failed to read tunnel count: %w", err)
	}
	tunnelCount := int(countBuf[0])
	if tunnelCount > maxTunnelCount {
		return nil, errs.New(errs.CodeInvalidArg, "too many tunnels")
	}

	tunnels := make([]TunnelEntry, 0, tunnelCount)
	for i := 0; i < tunnelCount; i++ {
		labelLenBuf := make([]byte, 1)
		if _, err := io.ReadFull(r, labelLenBuf); err != nil {
			return nil, fmt.Errorf("failed to read label len: %w", err)
		}
		labelLen := int(labelLenBuf[0])
		if labelLen > maxLabelLen {
			return nil, errs.New(errs.CodeInvalidArg, "label too long")
		}

		labelBuf := make([]byte, labelLen)
		if _, err := io.ReadFull(r, labelBuf); err != nil {
			return nil, fmt.Errorf("failed to read label: %w", err)
		}

		addrLenBuf := make([]byte, 2)
		if _, err := io.ReadFull(r, addrLenBuf); err != nil {
			return nil, fmt.Errorf("failed to read local_addr len: %w", err)
		}
		addrLen := int(binary.BigEndian.Uint16(addrLenBuf))
		if addrLen > maxLocalAddrLen {
			return nil, errs.New(errs.CodeInvalidArg, "local_addr too long")
		}

		addrBuf := make([]byte, addrLen)
		if _, err := io.ReadFull(r, addrBuf); err != nil {
			return nil, fmt.Errorf("failed to read local_addr: %w", err)
		}

		tunnels = append(tunnels, TunnelEntry{
			Label:     string(labelBuf),
			LocalAddr: string(addrBuf),
		})
	}

	return &AuthV2Message{Token: string(tokenBuf), Tunnels: tunnels}, nil
}

func EncodeAuthV2Response(w io.Writer, status byte, tunnelID string, tunnels []TunnelHostEntry) error {
	idBytes := []byte(tunnelID)
	if len(idBytes) > maxTunnelIDLen {
		return errs.New(errs.CodeInvalidArg, "tunnel_id too long")
	}
	if len(tunnels) > maxTunnelCount {
		return errs.New(errs.CodeInvalidArg, "too many tunnels in response")
	}

	size := 4 + len(idBytes) + 1
	for _, t := range tunnels {
		size += 1 + len(t.Label) + 2 + len(t.Hostname)
	}

	buf := make([]byte, size)
	off := 0
	buf[off] = MsgAuthRespV2
	off++
	buf[off] = status
	off++
	binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(idBytes)))
	off += 2
	copy(buf[off:], idBytes)
	off += len(idBytes)
	buf[off] = byte(len(tunnels))
	off++

	for _, t := range tunnels {
		buf[off] = byte(len(t.Label))
		off++
		copy(buf[off:], t.Label)
		off += len(t.Label)
		binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(t.Hostname)))
		off += 2
		copy(buf[off:], t.Hostname)
		off += len(t.Hostname)
	}

	_, err := w.Write(buf[:off])
	return err
}

func DecodeAuthV2Response(r io.Reader) (*AuthV2ResponseMessage, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("failed to read auth v2 response header: %w", err)
	}

	if header[0] != MsgAuthRespV2 {
		return nil, errs.New(errs.CodeInvalidArg, fmt.Sprintf("expected auth v2 response type 0x07, got 0x%02x", header[0]))
	}

	status := header[1]
	idLen := binary.BigEndian.Uint16(header[2:4])
	if idLen > maxTunnelIDLen {
		return nil, errs.New(errs.CodeInvalidArg, "tunnel_id too long")
	}

	var tunnelID string
	if idLen > 0 {
		idBuf := make([]byte, idLen)
		if _, err := io.ReadFull(r, idBuf); err != nil {
			return nil, fmt.Errorf("failed to read auth v2 response tunnel_id: %w", err)
		}
		tunnelID = string(idBuf)
	}

	countBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, countBuf); err != nil {
		return nil, fmt.Errorf("failed to read tunnel count in response: %w", err)
	}
	tunnelCount := int(countBuf[0])
	if tunnelCount > maxTunnelCount {
		return nil, errs.New(errs.CodeInvalidArg, "too many tunnels in response")
	}

	tunnels := make([]TunnelHostEntry, 0, tunnelCount)
	for i := 0; i < tunnelCount; i++ {
		labelLenBuf := make([]byte, 1)
		if _, err := io.ReadFull(r, labelLenBuf); err != nil {
			return nil, fmt.Errorf("failed to read response label len: %w", err)
		}
		labelLen := int(labelLenBuf[0])

		labelBuf := make([]byte, labelLen)
		if _, err := io.ReadFull(r, labelBuf); err != nil {
			return nil, fmt.Errorf("failed to read response label: %w", err)
		}

		hostLenBuf := make([]byte, 2)
		if _, err := io.ReadFull(r, hostLenBuf); err != nil {
			return nil, fmt.Errorf("failed to read response hostname len: %w", err)
		}
		hostLen := int(binary.BigEndian.Uint16(hostLenBuf))
		if hostLen > maxPublicURLLen {
			return nil, errs.New(errs.CodeInvalidArg, "hostname too long")
		}

		hostBuf := make([]byte, hostLen)
		if _, err := io.ReadFull(r, hostBuf); err != nil {
			return nil, fmt.Errorf("failed to read response hostname: %w", err)
		}

		tunnels = append(tunnels, TunnelHostEntry{
			Label:    string(labelBuf),
			Hostname: string(hostBuf),
		})
	}

	return &AuthV2ResponseMessage{
		Status:   status,
		TunnelID: tunnelID,
		Tunnels:  tunnels,
	}, nil
}

func EncodeStreamLabel(w io.Writer, label string) error {
	labelBytes := []byte(label)
	if len(labelBytes) > maxLabelLen {
		return errs.New(errs.CodeInvalidArg, "label too long")
	}

	buf := make([]byte, 2+len(labelBytes))
	buf[0] = MsgStreamLabel
	buf[1] = byte(len(labelBytes))
	copy(buf[2:], labelBytes)

	_, err := w.Write(buf)
	return err
}

func DecodeStreamLabel(r io.Reader) (string, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(r, header); err != nil {
		return "", fmt.Errorf("failed to read stream label header: %w", err)
	}

	if header[0] != MsgStreamLabel {
		return "", errs.New(errs.CodeInvalidArg, fmt.Sprintf("expected stream label type 0x08, got 0x%02x", header[0]))
	}

	labelLen := int(header[1])
	if labelLen > maxLabelLen {
		return "", errs.New(errs.CodeInvalidArg, "label too long")
	}

	labelBuf := make([]byte, labelLen)
	if _, err := io.ReadFull(r, labelBuf); err != nil {
		return "", fmt.Errorf("failed to read stream label: %w", err)
	}

	return string(labelBuf), nil
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
