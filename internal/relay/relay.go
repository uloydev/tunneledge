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

const (
	defaultRelayBufferSize   = 32 * 1024
	defaultRelayQueueSize    = 16
	defaultRelayEnqueueWait  = 250 * time.Millisecond
	defaultRelayWriteTimeout = 2 * time.Second
)

var ErrBackpressure = errors.New("relay backpressure timeout")

var relayBufferPool = sync.Pool{
	New: func() any {
		return make([]byte, defaultRelayBufferSize)
	},
}

type relayOptions struct {
	bufferSize     int
	queueSize      int
	enqueueTimeout time.Duration
	writeTimeout   time.Duration
}

type relayChunk struct {
	buf []byte
	n   int
}

// deadlineSetter is satisfied by net.Conn, quic.Stream, and any other I/O
// type that supports read/write deadlines.
type deadlineSetter interface {
	SetDeadline(t time.Time) error
}

type writeDeadlineSetter interface {
	SetWriteDeadline(t time.Time) error
}

func setDeadlineIfSupported(v io.ReadWriteCloser, t time.Time) {
	if ds, ok := v.(deadlineSetter); ok {
		_ = ds.SetDeadline(t)
	}
}

func setWriteDeadlineIfSupported(v io.ReadWriteCloser, t time.Time) {
	if ds, ok := v.(writeDeadlineSetter); ok {
		_ = ds.SetWriteDeadline(t)
		return
	}
	setDeadlineIfSupported(v, t)
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
	DroppedFrames int64
	QueueTimeouts int64
	WriteTimeouts int64
	MaxQueueDepth int
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

func (s *Stats) AddDroppedFrames(n int64) {
	s.mu.Lock()
	s.DroppedFrames += n
	s.mu.Unlock()
}

func (s *Stats) AddQueueTimeout() {
	s.mu.Lock()
	s.QueueTimeouts++
	s.mu.Unlock()
}

func (s *Stats) AddWriteTimeout() {
	s.mu.Lock()
	s.WriteTimeouts++
	s.mu.Unlock()
}

func (s *Stats) ObserveQueueDepth(depth int) {
	s.mu.Lock()
	if depth > s.MaxQueueDepth {
		s.MaxQueueDepth = depth
	}
	s.mu.Unlock()
}

func (s *Stats) GetDroppedFrames() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.DroppedFrames
}

func (s *Stats) GetQueueTimeouts() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.QueueTimeouts
}

func (s *Stats) GetWriteTimeouts() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.WriteTimeouts
}

func (s *Stats) GetMaxQueueDepth() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MaxQueueDepth
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
	return bidirectionalWithOptions(ctx, a, b, idleTimeout, onBytes, relayOptions{
		bufferSize:     defaultRelayBufferSize,
		queueSize:      defaultRelayQueueSize,
		enqueueTimeout: defaultRelayEnqueueWait,
		writeTimeout:   defaultRelayWriteTimeout,
	})
}

func bidirectionalWithOptions(ctx context.Context, a, b io.ReadWriteCloser, idleTimeout time.Duration, onBytes func(direction string, n int), opts relayOptions) (*RelayResult, error) {
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

	g, groupCtx := errgroup.WithContext(ctx)
	stopCloser := context.AfterFunc(groupCtx, func() {
		_ = a.Close()
		_ = b.Close()
	})
	defer stopCloser()

	g.Go(func() error {
		return relayOneWay(groupCtx, makeReader(a), b, idleTimeout, opts, stats, stats.AddSent, "sent", onBytes)
	})

	g.Go(func() error {
		return relayOneWay(groupCtx, makeReader(b), a, idleTimeout, opts, stats, stats.AddReceived, "received", onBytes)
	})

	err := g.Wait()

	_ = a.Close()
	_ = b.Close()

	// Idle timeout errors are expected and clean — don't surface as relay errors.
	if isIdleTimeout(err) && stats.GetWriteTimeouts() == 0 {
		err = nil
	}
	if errors.Is(err, net.ErrClosed) && ctx.Err() != nil {
		err = ctx.Err()
	}

	result := &RelayResult{Stats: stats}

	if err != nil {
		log.Debug().Err(err).Int64("sent", stats.GetSent()).Int64("received", stats.GetReceived()).Msg("relay ended")
	}

	return result, err
}

func relayOneWay(ctx context.Context, src io.Reader, dst io.ReadWriteCloser, idleTimeout time.Duration, opts relayOptions, stats *Stats, addBytes func(int64), direction string, onBytes func(direction string, n int)) error {
	chunks := make(chan relayChunk, opts.queueSize)
	group, groupCtx := errgroup.WithContext(ctx)

	group.Go(func() error {
		defer close(chunks)
		for {
			buf := getRelayBuffer(opts.bufferSize)
			n, err := src.Read(buf)
			if n > 0 {
				chunk := relayChunk{buf: buf, n: n}
				stats.ObserveQueueDepth(len(chunks) + 1)

				timer := time.NewTimer(opts.enqueueTimeout)
				select {
				case chunks <- chunk:
					if !timer.Stop() {
						<-timer.C
					}
				case <-groupCtx.Done():
					releaseRelayBuffer(buf)
					if !timer.Stop() {
						<-timer.C
					}
					return groupCtx.Err()
				case <-timer.C:
					releaseRelayBuffer(buf)
					stats.AddDroppedFrames(1)
					stats.AddQueueTimeout()
					return ErrBackpressure
				}
			} else {
				releaseRelayBuffer(buf)
			}

			if err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
		}
	})

	group.Go(func() error {
		defer closeWrite(dst)
		for {
			select {
			case <-groupCtx.Done():
				return groupCtx.Err()
			case chunk, ok := <-chunks:
				if !ok {
					return nil
				}
				err := writeChunk(dst, chunk, opts.writeTimeout, idleTimeout)
				releaseRelayBuffer(chunk.buf)
				if err != nil {
					if isIdleTimeout(err) {
						stats.AddWriteTimeout()
					}
					return err
				}
				addBytes(int64(chunk.n))
				if onBytes != nil {
					onBytes(direction, chunk.n)
				}
			}
		}
	})

	return group.Wait()
}

func getRelayBuffer(size int) []byte {
	if size == defaultRelayBufferSize {
		return relayBufferPool.Get().([]byte)
	}
	return make([]byte, size)
}

func releaseRelayBuffer(buf []byte) {
	if cap(buf) == defaultRelayBufferSize {
		relayBufferPool.Put(buf[:defaultRelayBufferSize])
	}
}

func writeChunk(dst io.ReadWriteCloser, chunk relayChunk, writeTimeout, idleTimeout time.Duration) error {
	timeout := writeTimeout
	if timeout <= 0 {
		timeout = idleTimeout
	}

	written := 0
	for written < chunk.n {
		if timeout > 0 {
			setWriteDeadlineIfSupported(dst, time.Now().Add(timeout))
		}
		n, err := dst.Write(chunk.buf[written:chunk.n])
		written += n
		if err != nil {
			return err
		}
	}
	return nil
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
