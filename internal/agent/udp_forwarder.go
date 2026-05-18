package agent

import (
	"context"
	"net"
	"sync"

	"tunneledge/internal/transport"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog/log"
)

const udpForwarderBufSize = 65507

// UDPForwarder listens on a local UDP address and forwards all received
// datagrams to the gateway over the QUIC datagram channel, tagging them with
// the given tunnel label. Replies from the gateway (datagrams tagged with the
// same label and the original src addr) are written back to the originating
// address.
type UDPForwarder struct {
	label     string
	localAddr string
	conn      *net.UDPConn

	mu       sync.RWMutex
	sessions map[string]*net.UDPAddr // srcKey → remote addr for replies
}

func newUDPForwarder(label, localAddr string) (*UDPForwarder, error) {
	addr, err := net.ResolveUDPAddr("udp", localAddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	return &UDPForwarder{
		label:     label,
		localAddr: localAddr,
		conn:      conn,
		sessions:  make(map[string]*net.UDPAddr),
	}, nil
}

func (f *UDPForwarder) close() {
	f.conn.Close()
}

// runLocalToGateway reads from the local UDP socket and sends each datagram as
// a QUIC datagram to the gateway.
func (f *UDPForwarder) runLocalToGateway(ctx context.Context, quicConn *quic.Conn) {
	buf := make([]byte, udpForwarderBufSize)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, remoteAddr, err := f.conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warn().Err(err).Str("label", f.label).Msg("UDP forwarder: read error")
			continue
		}

		srcKey := remoteAddr.String()
		f.mu.Lock()
		f.sessions[srcKey] = remoteAddr
		f.mu.Unlock()

		payload := make([]byte, n)
		copy(payload, buf[:n])

		frame, encErr := transport.EncodeUDPDatagram(&transport.UDPDatagramMessage{
			Label:   f.label,
			SrcAddr: srcKey,
			Payload: payload,
		})
		if encErr != nil {
			log.Warn().Err(encErr).Str("label", f.label).Msg("UDP forwarder: encode error")
			continue
		}

		if sendErr := quicConn.SendDatagram(frame); sendErr != nil {
			log.Warn().Err(sendErr).Str("label", f.label).Msg("UDP forwarder: send datagram error")
		}
	}
}

// deliverFromGateway receives a decoded UDPDatagramMessage from the gateway
// (routed by the agent's datagram loop) and sends it back to the original
// local client that initiated the UDP session.
func (f *UDPForwarder) deliverFromGateway(msg *transport.UDPDatagramMessage) {
	f.mu.RLock()
	clientAddr, ok := f.sessions[msg.SrcAddr]
	f.mu.RUnlock()
	if !ok {
		log.Debug().Str("src_addr", msg.SrcAddr).Str("label", f.label).Msg("UDP forwarder: no session for reply")
		return
	}
	if _, err := f.conn.WriteToUDP(msg.Payload, clientAddr); err != nil {
		log.Warn().Err(err).Str("label", f.label).Msg("UDP forwarder: reply write error")
	}
}

// udpDatagramLoop reads QUIC datagrams from the gateway and dispatches them to
// the appropriate UDPForwarder by label. This runs as a goroutine alongside the
// TCP stream loop.
func (a *Agent) udpDatagramLoop(ctx context.Context, conn *quic.Conn, forwarders map[string]*UDPForwarder) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		data, err := conn.ReceiveDatagram(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warn().Err(err).Msg("UDP datagram loop: receive error")
			return
		}

		msg, decErr := transport.DecodeUDPDatagram(data)
		if decErr != nil {
			log.Warn().Err(decErr).Msg("UDP datagram loop: decode error")
			continue
		}

		fwd, ok := forwarders[msg.Label]
		if !ok {
			log.Debug().Str("label", msg.Label).Msg("UDP datagram loop: no forwarder for label")
			continue
		}
		fwd.deliverFromGateway(msg)
	}
}
