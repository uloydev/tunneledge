package metrics

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

type Metrics struct {
	registry *prometheus.Registry

	ActiveTunnels   prometheus.Gauge
	ActiveStreams   prometheus.Gauge
	BytesForwarded  *prometheus.CounterVec
	StreamDuration  prometheus.Histogram
	TunnelCreated   *prometheus.CounterVec
	TunnelDestroyed *prometheus.CounterVec
	ReconnectTotal  prometheus.Counter
	RelayDroppedFrames prometheus.Counter
	RelayQueueTimeouts prometheus.Counter
	RelayWriteTimeouts prometheus.Counter
	RelayQueueDepth    prometheus.Histogram
	Errors          *prometheus.CounterVec
}

var (
	globalOnce sync.Once
	global     *Metrics
)

func New(namespace string) *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
	}

	m.ActiveTunnels = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "active_tunnels",
		Help:      "Number of currently active tunnels",
	})

	m.ActiveStreams = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "active_streams",
		Help:      "Number of currently active streams",
	})

	m.BytesForwarded = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "bytes_forwarded_total",
		Help:      "Total bytes forwarded through tunnels",
	}, []string{"direction"})

	m.StreamDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "stream_duration_seconds",
		Help:      "Duration of streams in seconds",
		Buckets:   prometheus.DefBuckets,
	})

	m.TunnelCreated = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "tunnel_created_total",
		Help:      "Total tunnels created",
	}, []string{"status"})

	m.TunnelDestroyed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "tunnel_destroyed_total",
		Help:      "Total tunnels destroyed",
	}, []string{"reason"})

	m.ReconnectTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "reconnect_total",
		Help:      "Total reconnection attempts",
	})

	m.RelayDroppedFrames = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "relay_dropped_frames_total",
		Help:      "Total relay frames dropped during overload protection",
	})

	m.RelayQueueTimeouts = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "relay_queue_timeouts_total",
		Help:      "Total relay queue timeout events",
	})

	m.RelayWriteTimeouts = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "relay_write_timeouts_total",
		Help:      "Total relay write timeout events",
	})

	m.RelayQueueDepth = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "relay_queue_depth",
		Help:      "Observed relay queue depth per completed stream",
		Buckets:   []float64{1, 2, 4, 8, 16, 32},
	})

	m.Errors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "errors_total",
		Help:      "Total errors by type",
	}, []string{"code", "component"})

	m.registry.MustRegister(
		m.ActiveTunnels,
		m.ActiveStreams,
		m.BytesForwarded,
		m.StreamDuration,
		m.TunnelCreated,
		m.TunnelDestroyed,
		m.ReconnectTotal,
		m.RelayDroppedFrames,
		m.RelayQueueTimeouts,
		m.RelayWriteTimeouts,
		m.RelayQueueDepth,
		m.Errors,
	)

	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

func (m *Metrics) Register() error {
	return prometheus.Register(m.registry)
}

type ReadinessCheck func() error

type Server struct {
	server *http.Server
	addr   string
	wg     sync.WaitGroup
}

func NewServer(addr string, m *Metrics, checks ...ReadinessCheck) *Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", m.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		for _, check := range checks {
			if err := check(); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprintf(w, "not ready: %v", err)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ready")
	})

	return &Server{
		server: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
		addr: addr,
	}
}

func (s *Server) Start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		log.Info().Str("addr", s.addr).Msg("starting metrics server")
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("metrics server error")
		}
	}()
}

func (s *Server) Stop(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := s.server.Shutdown(shutdownCtx)
	s.wg.Wait()
	return err
}

func initGlobal() {
	global = New("tunneledge")
}

func Global() *Metrics {
	globalOnce.Do(initGlobal)
	return global
}
