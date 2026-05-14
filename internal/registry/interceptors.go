package registry

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
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

	event.Str("method", info.FullMethod).Str("peer", peerAddr).Dur("duration", duration).Str("code", code.String()).Msg("gRPC call")

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

func NewGRPCServer(authToken string) *grpc.Server {
	interceptors := []grpc.UnaryServerInterceptor{LoggingInterceptor}
	if authToken != "" {
		interceptors = append(interceptors, AuthInterceptor(authToken))
	}

	return grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: 5 * time.Minute,
			Time:              30 * time.Second,
			Timeout:           10 * time.Second,
		}),
		grpc.ChainUnaryInterceptor(interceptors...),
	)
}

func CodeToString(c codes.Code) string {
	return c.String()
}
