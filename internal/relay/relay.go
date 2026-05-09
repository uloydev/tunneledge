package relay

import (
	"context"
	"io"
	"sync"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

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
	return BidirectionalWithCallback(ctx, a, b, nil)
}

func BidirectionalWithCallback(ctx context.Context, a, b io.ReadWriteCloser, onBytes func(direction string, n int)) (*RelayResult, error) {
	stats := &Stats{}

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		defer b.Close()
		n, err := io.Copy(b, readerFunc{ctx: ctx, r: a})
		stats.AddSent(n)
		if onBytes != nil && n > 0 {
			onBytes("sent", int(n))
		}
		return err
	})

	g.Go(func() error {
		defer a.Close()
		n, err := io.Copy(a, readerFunc{ctx: ctx, r: b})
		stats.AddReceived(n)
		if onBytes != nil && n > 0 {
			onBytes("received", int(n))
		}
		return err
	})

	err := g.Wait()
	result := &RelayResult{Stats: stats}

	if err != nil {
		log.Debug().Err(err).Int64("sent", stats.GetSent()).Int64("received", stats.GetReceived()).Msg("relay ended")
	}

	return result, nil
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
