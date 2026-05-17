package registry

import (
	"context"
	"time"

	"tunneledge/internal/auth"
	"tunneledge/pkg/observability"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func LoggingInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	start := time.Now()
	p, _ := peer.FromContext(ctx)
	peerAddr := "unknown"
	if p != nil {
		peerAddr = p.Addr.String()
	}

	resp, err := handler(ctx, req)
	duration := time.Since(start)

	code := status.Code(err)
	event := log.Info()
	if err != nil {
		event = log.Error().Err(err)
	}
	traceID, spanID := observability.TraceIDs(ctx)

	event.Str("method", info.FullMethod).Str("peer", peerAddr).Dur("duration", duration).Str("code", code.String()).Str("trace_id", traceID).Str("span_id", spanID).Msg("gRPC call")

	return resp, err
}

func AuthInterceptor(token string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if token == "" {
			return handler(ctx, req)
		}

		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Errorf(codes.Unauthenticated, "missing metadata")
		}

		authTokens := md.Get("authorization")
		if len(authTokens) == 0 || authTokens[0] != "Bearer "+token {
			return nil, status.Errorf(codes.Unauthenticated, "invalid auth token")
		}

		return handler(ctx, req)
	}
}

func NewGRPCServer(authToken string, rateLimitRPM int) *grpc.Server {
	interceptors := []grpc.UnaryServerInterceptor{LoggingInterceptor}
	if rateLimitRPM > 0 {
		interceptors = append(interceptors, RateLimitInterceptor(rateLimitRPM))
	}
	if authToken != "" {
		interceptors = append(interceptors, AuthInterceptor(authToken))
	}

	return grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: 5 * time.Minute,
			Time:              30 * time.Second,
			Timeout:           10 * time.Second,
		}),
		grpc.ChainUnaryInterceptor(interceptors...),
	)
}

// RateLimitInterceptor returns a gRPC interceptor that rate-limits requests
// per peer IP address to rpm requests per minute.
func RateLimitInterceptor(rpm int) grpc.UnaryServerInterceptor {
	limiter := auth.NewIPRateLimiter(rpm, rpm)
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		p, ok := peer.FromContext(ctx)
		if ok && p.Addr != nil {
			if !limiter.Allow(p.Addr.String()) {
				return nil, status.Errorf(codes.ResourceExhausted, "rate limit exceeded")
			}
		}
		return handler(ctx, req)
	}
}

func CodeToString(c codes.Code) string {
	return c.String()
}
