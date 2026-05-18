package gateway

import (
	"context"
	"net"
	"sync"
	"time"

	"tunneledge/internal/transport"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog/log"
)

const (
	udpSessionIdleTimeout = 60 * time.Second
	udpMaxPayload         = 65507 // max safe UDP payload (IPv4 w/ header overhead)
)

// udpSession maps a remote UDP address to the QUIC connection (and tunnel info)
// that should receive datagrams for that address.
type udpSession struct {
	tunnelID string
	label    string
	srcAddr  *net.UDPAddr
	quicConn *quic.Conn
	lastSeen time.Time
}

// UDPSessionTable is a concurrency-safe store of active UDP sessions.
// Each entry links a remote UDP address to the agent QUIC connection that
// should forward the datagram.
type UDPSessionTable struct {
	mu       sync.RWMutex
	sessions map[string]*udpSession // key: "label|srcAddr"
}

func newUDPSessionTable() *UDPSessionTable {
	return &UDPSessionTable{
		sessions: make(map[string]*udpSession),
	}
}

func udpSessionKey(label, srcAddr string) string {
	return label + "|" + srcAddr
}

func (t *UDPSessionTable) get(label, srcAddr string) (*udpSession, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	s, ok := t.sessions[udpSessionKey(label, srcAddr)]
	return s, ok
}

func (t *UDPSessionTable) set(label, srcAddr string, s *udpSession) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sessions[udpSessionKey(label, srcAddr)] = s
}

func (t *UDPSessionTable) delete(label, srcAddr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.sessions, udpSessionKey(label, srcAddr))
}

// evictExpired removes sessions that have been idle longer than udpSessionIdleTimeout.
func (t *UDPSessionTable) evictExpired() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	cutoff := time.Now().Add(-udpSessionIdleTimeout)
	count := 0
	for k, s := range t.sessions {
		if s.lastSeen.Before(cutoff) {
			delete(t.sessions, k)
			count++
		}
	}
	return count
}

// udpDatagramListener reads raw UDP packets on conn and forwards each one via
// a QUIC datagram to the agent that owns the matching tunnel/label.
//
// The lookup is: srcAddr → tunnelID+label via g.router.LookupWithLabel(hostname
// extracted from the UDP payload). This function blocks until ctx is cancelled.
func (g *Gateway) udpDatagramListener(ctx context.Context, conn *net.UDPConn) {
	udpSessions := newUDPSessionTable()

	// Eviction ticker.
	evictTicker := time.NewTicker(30 * time.Second)
	defer evictTicker.Stop()

	buf := make([]byte, udpMaxPayload)
	for {
		select {
		case <-ctx.Done():
			return
		case <-evictTicker.C:
			n := udpSessions.evictExpired()
			if n > 0 {
				log.Debug().Int("evicted", n).Msg("UDP session table: evicted idle sessions")
			}
			continue
		default:
		}

		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			log.Warn().Err(err).Msg("UDP datagram listener read error")
			continue
		}

		payload := make([]byte, n)
		copy(payload, buf[:n])
		srcAddr := remoteAddr.String()

		// Determine which tunnel+label this packet belongs to.
		// For UDP, we use a fixed "udp" label convention.
		// Clients must connect to a port that the gateway has mapped to a tunnelID.
		// For now: look up by srcAddr in the session table, or route via SNI header.
		// Phase 4D: simple label = "udp", tunnelID from session table.
		// The initial implementation uses a broadcast-to-all-udp-tunnels approach;
		// a production deployment would use separate ports per tunnel or a
		// routing header. Here we forward to the session table entry if present.
		sess, ok := udpSessions.get("udp", srcAddr)
		if !ok {
			log.Debug().Str("src_addr", srcAddr).Msg("UDP: no session; dropping datagram")
			continue
		}
		sess.lastSeen = time.Now()

		frame, encErr := transport.EncodeUDPDatagram(&transport.UDPDatagramMessage{
			Label:   sess.label,
			SrcAddr: srcAddr,
			Payload: payload,
		})
		if encErr != nil {
			log.Warn().Err(encErr).Msg("UDP: failed to encode datagram frame")
			continue
		}

		if sendErr := sess.quicConn.SendDatagram(frame); sendErr != nil {
			log.Warn().Err(sendErr).Str("tunnel_id", sess.tunnelID).Msg("UDP: failed to send QUIC datagram")
			udpSessions.delete(sess.label, srcAddr)
		}
	}
}

// udpDatagramForwardLoop reads QUIC datagrams from a connected agent's QUIC
// connection and forwards the contained UDP payloads to their src addresses.
// This handles the agent → gateway → internet direction.
func (g *Gateway) udpDatagramForwardLoop(ctx context.Context, tunnelID string, conn *quic.Conn, udpConn *net.UDPConn) {
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
			log.Warn().Err(err).Str("tunnel_id", tunnelID).Msg("UDP: failed to receive QUIC datagram from agent")
			return
		}

		msg, decErr := transport.DecodeUDPDatagram(data)
		if decErr != nil {
			log.Warn().Err(decErr).Str("tunnel_id", tunnelID).Msg("UDP: malformed datagram from agent")
			continue
		}

		// Parse destination address from SrcAddr (agent sends original client addr).
		dstAddr, parseErr := net.ResolveUDPAddr("udp", msg.SrcAddr)
		if parseErr != nil {
			log.Warn().Err(parseErr).Str("src_addr", msg.SrcAddr).Msg("UDP: invalid src addr in datagram")
			continue
		}

		if _, err := udpConn.WriteToUDP(msg.Payload, dstAddr); err != nil {
			log.Warn().Err(err).Str("dst_addr", msg.SrcAddr).Msg("UDP: failed to write reply to client")
		}
	}
}
