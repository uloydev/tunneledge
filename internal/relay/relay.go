package relay

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

// deadlineSetter is satisfied by net.Conn, quic.Stream, and any other I/O
// type that supports read/write deadlines.
type deadlineSetter interface {
	SetDeadline(t time.Time) error
}

func setDeadlineIfSupported(v io.ReadWriteCloser, t time.Time) {
	if ds, ok := v.(deadlineSetter); ok {
		_ = ds.SetDeadline(t)
	}
}

// idleReader wraps a reader and, after every successful read, resets the
// deadline on all relay peers. This keeps both sides alive while traffic
// flows and causes a timeout if the stream goes idle for too long.
type idleReader struct {
	ctx         context.Context
	r           io.Reader
	idleTimeout time.Duration
	peers       []io.ReadWriteCloser
}

func (ir *idleReader) Read(p []byte) (int, error) {
	select {
	case <-ir.ctx.Done():
		return 0, ir.ctx.Err()
	default:
	}
	n, err := ir.r.Read(p)
	if n > 0 && ir.idleTimeout > 0 {
		deadline := time.Now().Add(ir.idleTimeout)
		for _, peer := range ir.peers {
			setDeadlineIfSupported(peer, deadline)
		}
	}
	return n, err
}

// isIdleTimeout returns true when err is a network timeout (deadline exceeded).
// These are expected when a stream goes idle and should not be surfaced as errors.
func isIdleTimeout(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

type Stats struct {
	mu            sync.Mutex
	BytesSent     int64
	BytesReceived int64
}

func (s *Stats) AddSent(n int64) {
	s.mu.Lock()
	s.BytesSent += n
	s.mu.Unlock()
}

func (s *Stats) AddReceived(n int64) {
	s.mu.Lock()
	s.BytesReceived += n
	s.mu.Unlock()
}

func (s *Stats) GetSent() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.BytesSent
}

func (s *Stats) GetReceived() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.BytesReceived
}

type RelayResult struct {
	Stats *Stats
}

func Bidirectional(ctx context.Context, a, b io.ReadWriteCloser) (*RelayResult, error) {
	return BidirectionalWithIdleTimeout(ctx, a, b, 0, nil)
}

func BidirectionalWithCallback(ctx context.Context, a, b io.ReadWriteCloser, onBytes func(direction string, n int)) (*RelayResult, error) {
	return BidirectionalWithIdleTimeout(ctx, a, b, 0, onBytes)
}

// BidirectionalWithIdleTimeout relays data between a and b, closing both sides
// after idleTimeout of inactivity (0 disables the idle timeout). Any I/O
// activity on either side extends the deadline window.
func BidirectionalWithIdleTimeout(ctx context.Context, a, b io.ReadWriteCloser, idleTimeout time.Duration, onBytes func(direction string, n int)) (*RelayResult, error) {
	stats := &Stats{}

	if idleTimeout > 0 {
		deadline := time.Now().Add(idleTimeout)
		setDeadlineIfSupported(a, deadline)
		setDeadlineIfSupported(b, deadline)
	}

	peers := []io.ReadWriteCloser{a, b}

	makeReader := func(r io.ReadWriteCloser) io.Reader {
		if idleTimeout > 0 {
			return &idleReader{ctx: ctx, r: r, idleTimeout: idleTimeout, peers: peers}
		}
		return readerFunc{ctx: ctx, r: r}
	}

	g, _ := errgroup.WithContext(ctx)

	g.Go(func() error {
		defer closeWrite(b)
		n, err := io.Copy(b, makeReader(a))
		stats.AddSent(n)
		if onBytes != nil && n > 0 {
			onBytes("sent", int(n))
		}
		return err
	})

	g.Go(func() error {
		defer closeWrite(a)
		n, err := io.Copy(a, makeReader(b))
		stats.AddReceived(n)
		if onBytes != nil && n > 0 {
			onBytes("received", int(n))
		}
		return err
	})

	err := g.Wait()

	a.Close()
	b.Close()

	// Idle timeout errors are expected and clean — don't surface as relay errors.
	if isIdleTimeout(err) {
		err = nil
	}

	result := &RelayResult{Stats: stats}

	if err != nil {
		log.Debug().Err(err).Int64("sent", stats.GetSent()).Int64("received", stats.GetReceived()).Msg("relay ended")
	}

	return result, nil
}

type closeWriter interface {
	CloseWrite() error
}

func closeWrite(c io.ReadWriteCloser) {
	if cw, ok := c.(closeWriter); ok {
		cw.CloseWrite()
	} else {
		c.Close()
	}
}

type readerFunc struct {
	ctx context.Context
	r   io.Reader
}

func (rf readerFunc) Read(p []byte) (int, error) {
	select {
	case <-rf.ctx.Done():
		return 0, rf.ctx.Err()
	default:
		return rf.r.Read(p)
	}
}
