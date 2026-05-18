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
	//
	// v3 adds: TunnelType per TunnelEntry, PreferredRegion in AuthV2,
	// ResumeToken/AssignedRegion/SuggestedGateways in AuthV2Response,
	// MsgSessionResume/Resp, MsgUDPDatagram.
	ProtocolVersion uint16 = 3

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
	// Phase 4 additions.
	MsgSessionResume     byte = 0x09 // agent → gateway: resume existing session
	MsgSessionResumeResp byte = 0x0A // gateway → agent: resume result
	MsgUDPDatagram       byte = 0x0B // bidirectional: UDP payload encapsulated in QUIC datagram

	AuthStatusOK           byte = 0x00
	AuthStatusError        byte = 0x01
	AuthStatusVersionError byte = 0x02 // client protocol version not supported

	// Session resume status codes.
	ResumeStatusResumed  byte = 0x10 // session successfully resumed
	ResumeStatusExpired  byte = 0x11 // resume token expired
	ResumeStatusNotFound byte = 0x12 // session not found

	// TunnelType constants used in TunnelEntry.
	TunnelTypeTCP = "tcp"
	TunnelTypeUDP = "udp"

	maxTokenLen       = 4096
	maxTunnelIDLen    = 256
	maxPublicURLLen   = 512
	maxLabelLen       = 64
	maxLocalAddrLen   = 256
	maxTunnelCount    = 32
	maxVersionLen     = 64
	maxResumeTokenLen = 512
	maxRegionLen      = 64
	maxSrcAddrLen     = 64
	maxUDPPayloadLen  = 65535
	maxSuggestedGWs   = 8
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

// TunnelEntry describes a single tunnel the agent wants the gateway to expose.
type TunnelEntry struct {
	Label      string
	LocalAddr  string
	TunnelType string // "tcp" or "udp"; empty defaults to "tcp"
}

// AuthV2Message is the payload of MsgAuthV2.
type AuthV2Message struct {
	Token           string
	Tunnels         []TunnelEntry
	PreferredRegion string // optional; empty = no preference
}

type TunnelHostEntry struct {
	Label    string
	Hostname string
}

// AuthV2ResponseMessage is the payload of MsgAuthRespV2.
type AuthV2ResponseMessage struct {
	Status            byte
	TunnelID          string
	Tunnels           []TunnelHostEntry
	ResumeToken       string   // HMAC-SHA256 token for session resume; empty if disabled
	AssignedRegion    string   // gateway's configured region
	SuggestedGateways []string // ordered list of gateways for preferred region
}

// SessionResumeMessage is sent by the agent as MsgSessionResume to attempt
// resuming a previously established session.
type SessionResumeMessage struct {
	Token string // resume token issued by the gateway on last auth
}

// SessionResumeRespMessage is the gateway's reply to MsgSessionResume.
type SessionResumeRespMessage struct {
	Status   byte   // ResumeStatusResumed / ResumeStatusExpired / ResumeStatusNotFound
	TunnelID string // filled only when Status == ResumeStatusResumed
	NewToken string // new resume token to use on the next reconnect
}

// UDPDatagramMessage carries a UDP payload between agent and gateway.
type UDPDatagramMessage struct {
	Label   string // tunnel label
	SrcAddr string // source address string (UDP addr)
	Payload []byte
}

// EncodeAuthV2 writes a MsgAuthV2 frame.
//
// Wire format (v3):
//
//	[0x06]
//	[token_len: 2B][token: var]
//	[tunnel_count: 1B]
//	  per tunnel: [label_len: 1B][label: var][local_addr_len: 2B][local_addr: var][type_len: 1B][type: var]
//	[region_len: 1B][region: var]
func EncodeAuthV2(w io.Writer, msg *AuthV2Message) error {
	tokenBytes := []byte(msg.Token)
	if len(tokenBytes) > maxTokenLen {
		return errs.New(errs.CodeInvalidArg, "token too long")
	}
	if len(msg.Tunnels) > maxTunnelCount {
		return errs.New(errs.CodeInvalidArg, "too many tunnels")
	}
	regionBytes := []byte(msg.PreferredRegion)
	if len(regionBytes) > maxRegionLen {
		return errs.New(errs.CodeInvalidArg, "region too long")
	}

	// Pre-calculate buffer size.
	size := 1 + 2 + len(tokenBytes) + 1 + 1 + len(regionBytes)
	for _, t := range msg.Tunnels {
		size += 1 + len(t.Label) + 2 + len(t.LocalAddr) + 1 + len(t.TunnelType)
	}

	buf := make([]byte, size)
	off := 0

	buf[off] = MsgAuthV2
	off++
	binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(tokenBytes)))
	off += 2
	copy(buf[off:], tokenBytes)
	off += len(tokenBytes)
	buf[off] = byte(len(msg.Tunnels))
	off++

	for _, t := range msg.Tunnels {
		if len(t.Label) > maxLabelLen {
			return errs.New(errs.CodeInvalidArg, fmt.Sprintf("label too long: %s", t.Label))
		}
		if len(t.LocalAddr) > maxLocalAddrLen {
			return errs.New(errs.CodeInvalidArg, fmt.Sprintf("local_addr too long: %s", t.LocalAddr))
		}
		if len(t.TunnelType) > maxLabelLen {
			return errs.New(errs.CodeInvalidArg, "tunnel_type too long")
		}
		buf[off] = byte(len(t.Label))
		off++
		copy(buf[off:], t.Label)
		off += len(t.Label)
		binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(t.LocalAddr)))
		off += 2
		copy(buf[off:], t.LocalAddr)
		off += len(t.LocalAddr)
		buf[off] = byte(len(t.TunnelType))
		off++
		copy(buf[off:], t.TunnelType)
		off += len(t.TunnelType)
	}

	buf[off] = byte(len(regionBytes))
	off++
	copy(buf[off:], regionBytes)
	off += len(regionBytes)

	_, err := w.Write(buf[:off])
	return err
}

// DecodeAuthV2 reads a MsgAuthV2 frame from r (message-type byte already consumed).
func DecodeAuthV2(r io.Reader) (*AuthV2Message, error) {
	header := make([]byte, 3)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("failed to read auth v2 header: %w", err)
	}

	if header[0] != MsgAuthV2 {
		return nil, errs.New(errs.CodeInvalidArg, fmt.Sprintf("expected auth v2 type 0x06, got 0x%02x", header[0]))
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

		typeLenBuf := make([]byte, 1)
		if _, err := io.ReadFull(r, typeLenBuf); err != nil {
			return nil, fmt.Errorf("failed to read tunnel_type len: %w", err)
		}
		typeLen := int(typeLenBuf[0])
		if typeLen > maxLabelLen {
			return nil, errs.New(errs.CodeInvalidArg, "tunnel_type too long")
		}
		var tunnelType string
		if typeLen > 0 {
			typeBuf := make([]byte, typeLen)
			if _, err := io.ReadFull(r, typeBuf); err != nil {
				return nil, fmt.Errorf("failed to read tunnel_type: %w", err)
			}
			tunnelType = string(typeBuf)
		}

		tunnels = append(tunnels, TunnelEntry{
			Label:      string(labelBuf),
			LocalAddr:  string(addrBuf),
			TunnelType: tunnelType,
		})
	}

	// Read optional preferred region.
	regionLenBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, regionLenBuf); err != nil {
		return nil, fmt.Errorf("failed to read region len: %w", err)
	}
	regionLen := int(regionLenBuf[0])
	if regionLen > maxRegionLen {
		return nil, errs.New(errs.CodeInvalidArg, "region too long")
	}
	var preferredRegion string
	if regionLen > 0 {
		regionBuf := make([]byte, regionLen)
		if _, err := io.ReadFull(r, regionBuf); err != nil {
			return nil, fmt.Errorf("failed to read region: %w", err)
		}
		preferredRegion = string(regionBuf)
	}

	return &AuthV2Message{
		Token:           string(tokenBuf),
		Tunnels:         tunnels,
		PreferredRegion: preferredRegion,
	}, nil
}

// EncodeAuthV2Response writes a MsgAuthRespV2 frame.
//
// Wire format (v3):
//
//	[0x07][status: 1B][tunnelid_len: 2B][tunnelid: var]
//	[tunnel_count: 1B]
//	  per tunnel: [label_len: 1B][label: var][hostname_len: 2B][hostname: var]
//	[resume_token_len: 2B][resume_token: var]
//	[assigned_region_len: 1B][assigned_region: var]
//	[suggested_count: 1B]
//	  per suggested gateway: [addr_len: 2B][addr: var]
func EncodeAuthV2Response(w io.Writer, msg *AuthV2ResponseMessage) error {
	idBytes := []byte(msg.TunnelID)
	if len(idBytes) > maxTunnelIDLen {
		return errs.New(errs.CodeInvalidArg, "tunnel_id too long")
	}
	if len(msg.Tunnels) > maxTunnelCount {
		return errs.New(errs.CodeInvalidArg, "too many tunnels in response")
	}
	resumeBytes := []byte(msg.ResumeToken)
	if len(resumeBytes) > maxResumeTokenLen {
		return errs.New(errs.CodeInvalidArg, "resume_token too long")
	}
	regionBytes := []byte(msg.AssignedRegion)
	if len(regionBytes) > maxRegionLen {
		return errs.New(errs.CodeInvalidArg, "assigned_region too long")
	}
	if len(msg.SuggestedGateways) > maxSuggestedGWs {
		return errs.New(errs.CodeInvalidArg, "too many suggested gateways")
	}

	size := 1 + 1 + 2 + len(idBytes) + 1 // type + status + id_len + id + count
	for _, t := range msg.Tunnels {
		size += 1 + len(t.Label) + 2 + len(t.Hostname)
	}
	size += 2 + len(resumeBytes) + 1 + len(regionBytes) + 1 // resume_token + region + gw_count
	for _, gw := range msg.SuggestedGateways {
		size += 2 + len(gw)
	}

	buf := make([]byte, size)
	off := 0

	buf[off] = MsgAuthRespV2
	off++
	buf[off] = msg.Status
	off++
	binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(idBytes)))
	off += 2
	copy(buf[off:], idBytes)
	off += len(idBytes)
	buf[off] = byte(len(msg.Tunnels))
	off++

	for _, t := range msg.Tunnels {
		buf[off] = byte(len(t.Label))
		off++
		copy(buf[off:], t.Label)
		off += len(t.Label)
		binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(t.Hostname)))
		off += 2
		copy(buf[off:], t.Hostname)
		off += len(t.Hostname)
	}

	binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(resumeBytes)))
	off += 2
	copy(buf[off:], resumeBytes)
	off += len(resumeBytes)

	buf[off] = byte(len(regionBytes))
	off++
	copy(buf[off:], regionBytes)
	off += len(regionBytes)

	buf[off] = byte(len(msg.SuggestedGateways))
	off++
	for _, gw := range msg.SuggestedGateways {
		gwBytes := []byte(gw)
		binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(gwBytes)))
		off += 2
		copy(buf[off:], gwBytes)
		off += len(gwBytes)
	}

	_, err := w.Write(buf[:off])
	return err
}

// DecodeAuthV2Response reads a MsgAuthRespV2 frame (message-type byte NOT consumed).
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

	// Read resume token.
	resumeLenBuf := make([]byte, 2)
	if _, err := io.ReadFull(r, resumeLenBuf); err != nil {
		return nil, fmt.Errorf("failed to read resume_token len: %w", err)
	}
	resumeLen := int(binary.BigEndian.Uint16(resumeLenBuf))
	if resumeLen > maxResumeTokenLen {
		return nil, errs.New(errs.CodeInvalidArg, "resume_token too long")
	}
	var resumeToken string
	if resumeLen > 0 {
		resumeBuf := make([]byte, resumeLen)
		if _, err := io.ReadFull(r, resumeBuf); err != nil {
			return nil, fmt.Errorf("failed to read resume_token: %w", err)
		}
		resumeToken = string(resumeBuf)
	}

	// Read assigned region.
	regionLenBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, regionLenBuf); err != nil {
		return nil, fmt.Errorf("failed to read assigned_region len: %w", err)
	}
	regionLen := int(regionLenBuf[0])
	if regionLen > maxRegionLen {
		return nil, errs.New(errs.CodeInvalidArg, "assigned_region too long")
	}
	var assignedRegion string
	if regionLen > 0 {
		regionBuf := make([]byte, regionLen)
		if _, err := io.ReadFull(r, regionBuf); err != nil {
			return nil, fmt.Errorf("failed to read assigned_region: %w", err)
		}
		assignedRegion = string(regionBuf)
	}

	// Read suggested gateways.
	gwCountBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, gwCountBuf); err != nil {
		return nil, fmt.Errorf("failed to read suggested gateway count: %w", err)
	}
	gwCount := int(gwCountBuf[0])
	if gwCount > maxSuggestedGWs {
		return nil, errs.New(errs.CodeInvalidArg, "too many suggested gateways")
	}
	suggestedGWs := make([]string, 0, gwCount)
	for i := 0; i < gwCount; i++ {
		addrLenBuf := make([]byte, 2)
		if _, err := io.ReadFull(r, addrLenBuf); err != nil {
			return nil, fmt.Errorf("failed to read suggested gw addr len: %w", err)
		}
		addrLen := int(binary.BigEndian.Uint16(addrLenBuf))
		if addrLen > maxPublicURLLen {
			return nil, errs.New(errs.CodeInvalidArg, "suggested gateway addr too long")
		}
		addrBuf := make([]byte, addrLen)
		if _, err := io.ReadFull(r, addrBuf); err != nil {
			return nil, fmt.Errorf("failed to read suggested gw addr: %w", err)
		}
		suggestedGWs = append(suggestedGWs, string(addrBuf))
	}

	return &AuthV2ResponseMessage{
		Status:            status,
		TunnelID:          tunnelID,
		Tunnels:           tunnels,
		ResumeToken:       resumeToken,
		AssignedRegion:    assignedRegion,
		SuggestedGateways: suggestedGWs,
	}, nil
}

// EncodeSessionResume writes a MsgSessionResume frame.
// Wire: [0x09][token_len: 2B][token: var]
func EncodeSessionResume(w io.Writer, token string) error {
	tb := []byte(token)
	if len(tb) > maxResumeTokenLen {
		return errs.New(errs.CodeInvalidArg, "resume token too long")
	}
	buf := make([]byte, 3+len(tb))
	buf[0] = MsgSessionResume
	binary.BigEndian.PutUint16(buf[1:3], uint16(len(tb)))
	copy(buf[3:], tb)
	_, err := w.Write(buf)
	return err
}

// DecodeSessionResume reads a MsgSessionResume frame (message-type byte NOT consumed).
func DecodeSessionResume(r io.Reader) (*SessionResumeMessage, error) {
	header := make([]byte, 3)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("failed to read session resume header: %w", err)
	}
	if header[0] != MsgSessionResume {
		return nil, errs.New(errs.CodeInvalidArg, fmt.Sprintf("expected session resume type 0x09, got 0x%02x", header[0]))
	}
	tokenLen := int(binary.BigEndian.Uint16(header[1:3]))
	if tokenLen > maxResumeTokenLen {
		return nil, errs.New(errs.CodeInvalidArg, "resume token too long")
	}
	tb := make([]byte, tokenLen)
	if _, err := io.ReadFull(r, tb); err != nil {
		return nil, fmt.Errorf("failed to read resume token: %w", err)
	}
	return &SessionResumeMessage{Token: string(tb)}, nil
}

// EncodeSessionResumeResp writes a MsgSessionResumeResp frame.
// Wire: [0x0A][status: 1B][tunnelid_len: 2B][tunnelid: var][new_token_len: 2B][new_token: var]
func EncodeSessionResumeResp(w io.Writer, msg *SessionResumeRespMessage) error {
	idBytes := []byte(msg.TunnelID)
	ntBytes := []byte(msg.NewToken)
	if len(idBytes) > maxTunnelIDLen {
		return errs.New(errs.CodeInvalidArg, "tunnel_id too long")
	}
	if len(ntBytes) > maxResumeTokenLen {
		return errs.New(errs.CodeInvalidArg, "new resume token too long")
	}
	buf := make([]byte, 1+1+2+len(idBytes)+2+len(ntBytes))
	off := 0
	buf[off] = MsgSessionResumeResp
	off++
	buf[off] = msg.Status
	off++
	binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(idBytes)))
	off += 2
	copy(buf[off:], idBytes)
	off += len(idBytes)
	binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(ntBytes)))
	off += 2
	copy(buf[off:], ntBytes)
	_, err := w.Write(buf)
	return err
}

// DecodeSessionResumeResp reads a MsgSessionResumeResp frame (message-type byte NOT consumed).
func DecodeSessionResumeResp(r io.Reader) (*SessionResumeRespMessage, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("failed to read session resume resp header: %w", err)
	}
	if header[0] != MsgSessionResumeResp {
		return nil, errs.New(errs.CodeInvalidArg, fmt.Sprintf("expected session resume resp type 0x0A, got 0x%02x", header[0]))
	}
	status := header[1]
	idLen := int(binary.BigEndian.Uint16(header[2:4]))
	if idLen > maxTunnelIDLen {
		return nil, errs.New(errs.CodeInvalidArg, "tunnel_id too long")
	}
	var tunnelID string
	if idLen > 0 {
		idBuf := make([]byte, idLen)
		if _, err := io.ReadFull(r, idBuf); err != nil {
			return nil, fmt.Errorf("failed to read resume resp tunnel_id: %w", err)
		}
		tunnelID = string(idBuf)
	}
	ntLenBuf := make([]byte, 2)
	if _, err := io.ReadFull(r, ntLenBuf); err != nil {
		return nil, fmt.Errorf("failed to read new token len: %w", err)
	}
	ntLen := int(binary.BigEndian.Uint16(ntLenBuf))
	if ntLen > maxResumeTokenLen {
		return nil, errs.New(errs.CodeInvalidArg, "new resume token too long")
	}
	var newToken string
	if ntLen > 0 {
		ntBuf := make([]byte, ntLen)
		if _, err := io.ReadFull(r, ntBuf); err != nil {
			return nil, fmt.Errorf("failed to read new resume token: %w", err)
		}
		newToken = string(ntBuf)
	}
	return &SessionResumeRespMessage{Status: status, TunnelID: tunnelID, NewToken: newToken}, nil
}

// EncodeUDPDatagram writes a MsgUDPDatagram frame.
// Wire: [0x0B][label_len: 1B][label: var][src_addr_len: 1B][src_addr: var][payload_len: 2B][payload: var]
func EncodeUDPDatagram(msg *UDPDatagramMessage) ([]byte, error) {
	lb := []byte(msg.Label)
	ab := []byte(msg.SrcAddr)
	if len(lb) > maxLabelLen {
		return nil, errs.New(errs.CodeInvalidArg, "label too long")
	}
	if len(ab) > maxSrcAddrLen {
		return nil, errs.New(errs.CodeInvalidArg, "src_addr too long")
	}
	if len(msg.Payload) > maxUDPPayloadLen {
		return nil, errs.New(errs.CodeInvalidArg, "UDP payload too long")
	}
	buf := make([]byte, 1+1+len(lb)+1+len(ab)+2+len(msg.Payload))
	off := 0
	buf[off] = MsgUDPDatagram
	off++
	buf[off] = byte(len(lb))
	off++
	copy(buf[off:], lb)
	off += len(lb)
	buf[off] = byte(len(ab))
	off++
	copy(buf[off:], ab)
	off += len(ab)
	binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(msg.Payload)))
	off += 2
	copy(buf[off:], msg.Payload)
	return buf, nil
}

// DecodeUDPDatagram decodes a raw datagram byte slice into UDPDatagramMessage.
// The first byte (MsgUDPDatagram) must be present.
func DecodeUDPDatagram(data []byte) (*UDPDatagramMessage, error) {
	if len(data) < 6 {
		return nil, errs.New(errs.CodeInvalidArg, "UDP datagram too short")
	}
	if data[0] != MsgUDPDatagram {
		return nil, errs.New(errs.CodeInvalidArg, fmt.Sprintf("expected UDP datagram type 0x0B, got 0x%02x", data[0]))
	}
	off := 1
	labelLen := int(data[off])
	off++
	if off+labelLen > len(data) {
		return nil, errs.New(errs.CodeInvalidArg, "truncated label")
	}
	label := string(data[off : off+labelLen])
	off += labelLen

	if off >= len(data) {
		return nil, errs.New(errs.CodeInvalidArg, "truncated src_addr len")
	}
	srcAddrLen := int(data[off])
	off++
	if off+srcAddrLen > len(data) {
		return nil, errs.New(errs.CodeInvalidArg, "truncated src_addr")
	}
	srcAddr := string(data[off : off+srcAddrLen])
	off += srcAddrLen

	if off+2 > len(data) {
		return nil, errs.New(errs.CodeInvalidArg, "truncated payload len")
	}
	payloadLen := int(binary.BigEndian.Uint16(data[off : off+2]))
	off += 2
	if off+payloadLen > len(data) {
		return nil, errs.New(errs.CodeInvalidArg, "truncated payload")
	}
	payload := make([]byte, payloadLen)
	copy(payload, data[off:off+payloadLen])
	return &UDPDatagramMessage{Label: label, SrcAddr: srcAddr, Payload: payload}, nil
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
		EnableDatagrams: true,
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
		EnableDatagrams: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to listen QUIC on %s: %w", addr, err)
	}
	return listener, nil
}
