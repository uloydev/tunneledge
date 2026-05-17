package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"tunneledge/internal/domain"
	"tunneledge/pkg/observability"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/time/rate"
)

type contextKey string

const (
	userIDKey    contextKey = "user_id"
	requestIDKey contextKey = "request_id"
	jtiKey       contextKey = "jwt_jti"

	// ExportedUserIDKey is the same key exposed for use in tests.
	ExportedUserIDKey = userIDKey
)

func UserIDFromContext(ctx context.Context) (uint, bool) {
	id, ok := ctx.Value(userIDKey).(uint)
	return id, ok
}

// JTIFromContext returns the JWT jti claim stored by JWTAuthMiddleware.
func JTIFromContext(ctx context.Context) string {
	v, _ := ctx.Value(jtiKey).(string)
	return v
}

// RequestIDFromContext returns the request ID injected by RequestIDMiddleware.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// RequestIDMiddleware generates a unique ID for each request, injects it into
// the context, and echoes it back in the X-Request-ID response header.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			b := make([]byte, 8)
			if _, err := rand.Read(b); err == nil {
				id = hex.EncodeToString(b)
			}
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type JWTConfig struct {
	Secret string
	TTL    time.Duration
}

// GeneratedJWT holds the result of a successful JWT generation.
type GeneratedJWT struct {
	Token     string
	JTI       string
	ExpiresAt time.Time
}

// generateJTI creates a random UUID v4 string for use as a JWT jti claim.
func generateJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func GenerateJWT(cfg JWTConfig, userID uint) (GeneratedJWT, error) {
	jti, err := generateJTI()
	if err != nil {
		return GeneratedJWT{}, fmt.Errorf("failed to generate jti: %w", err)
	}
	expiresAt := time.Now().Add(cfg.TTL)
	claims := jwt.MapClaims{
		"sub": userID,
		"exp": expiresAt.Unix(),
		"iat": time.Now().Unix(),
		"jti": jti,
		"aud": []string{"tunneledge"},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(cfg.Secret))
	if err != nil {
		return GeneratedJWT{}, fmt.Errorf("failed to sign JWT: %w", err)
	}
	return GeneratedJWT{Token: signed, JTI: jti, ExpiresAt: expiresAt}, nil
}

func parseJWT(secret, tokenStr string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims")
	}
	return claims, nil
}

// JWTMiddlewareOptions configures JWTAuthMiddleware.
type JWTMiddlewareOptions struct {
	Secret      string
	RevokedRepo domain.RevokedJTIRepository // optional; nil = no revocation check
}

func JWTAuthMiddleware(opts JWTMiddlewareOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := ""

			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				parts := strings.SplitN(authHeader, " ", 2)
				if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
					tokenStr = parts[1]
				}
			}

			if tokenStr == "" {
				if cookie, err := r.Cookie("session"); err == nil {
					tokenStr = cookie.Value
				}
			}

			if tokenStr == "" {
				if strings.Contains(r.Header.Get("Accept"), "text/html") {
					http.Redirect(w, r, "/login", http.StatusSeeOther)
					return
				}
				writeError(w, http.StatusUnauthorized, "missing authorization")
				return
			}

			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
				}
				return []byte(opts.Secret), nil
			}, jwt.WithAudience("tunneledge"), jwt.WithExpirationRequired())

			if err != nil || !token.Valid {
				writeError(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				writeError(w, http.StatusUnauthorized, "invalid token claims")
				return
			}

			ctx := r.Context()

			// Check JTI revocation if a revocation store is configured.
			if jtiVal, ok := claims["jti"].(string); ok && jtiVal != "" {
				ctx = context.WithValue(ctx, jtiKey, jtiVal)
				if opts.RevokedRepo != nil {
					revoked, rErr := opts.RevokedRepo.IsRevoked(ctx, jtiVal)
					if rErr != nil {
						log.Warn().Err(rErr).Msg("revocation check failed; rejecting token for safety")
						writeError(w, http.StatusUnauthorized, "authorization check failed")
						return
					}
					if revoked {
						writeError(w, http.StatusUnauthorized, "token has been revoked")
						return
					}
				}
			}

			subFloat, ok := claims["sub"].(float64)
			if !ok {
				writeError(w, http.StatusUnauthorized, "invalid token subject")
				return
			}

			ctx = context.WithValue(ctx, userIDKey, uint(subFloat))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		dur := time.Since(start)
		evt := log.Info()
		switch {
		case wrapped.status >= 500:
			evt = log.Error()
		case wrapped.status >= 400:
			evt = log.Warn()
		}

		evt.
			Str("request_id", RequestIDFromContext(r.Context())).
			Str("trace_id", traceIDFromContext(r.Context())).
			Str("span_id", spanIDFromContext(r.Context())).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Str("remote_addr", r.RemoteAddr).
			Int("status", wrapped.status).
			Dur("duration", dur).
			Msg("http request")
	})
}

func TracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := otel.Tracer("tunneledge/dashboard").Start(ctx, r.Method+" "+r.URL.Path,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.path", r.URL.Path),
				attribute.String("request.id", RequestIDFromContext(r.Context())),
			),
		)
		wrapped := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r.WithContext(ctx))
		span.SetAttributes(attribute.Int("http.status_code", wrapped.status))
		if wrapped.status >= http.StatusInternalServerError {
			span.SetStatus(otelcodes.Error, http.StatusText(wrapped.status))
		}
		span.End()
	})
}

func traceIDFromContext(ctx context.Context) string {
	traceID, _ := observability.TraceIDs(ctx)
	return traceID
}

func spanIDFromContext(ctx context.Context) string {
	_, spanID := observability.TraceIDs(ctx)
	return spanID
}

func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// ipRateLimiter enforces a per-IP token-bucket rate limit.
type ipRateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*rate.Limiter
	r        rate.Limit
	b        int
}

func newIPRateLimiter(r rate.Limit, b int) *ipRateLimiter {
	return &ipRateLimiter{
		visitors: make(map[string]*rate.Limiter),
		r:        r,
		b:        b,
	}
}

func (l *ipRateLimiter) getLimiter(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	lim, ok := l.visitors[ip]
	if !ok {
		lim = rate.NewLimiter(l.r, l.b)
		l.visitors[ip] = lim
	}
	return lim
}

// extractClientIP returns the best-effort client IP, respecting X-Forwarded-For
// and X-Real-IP headers set by trusted reverse proxies.
func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.IndexByte(xff, ','); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// authLimiter is the default per-IP rate limiter for auth endpoints (5 rpm).
var authLimiter = newIPRateLimiter(rate.Every(12*time.Second), 5) // 5 rpm, burst 5

// NewRateLimitMiddleware returns a middleware that rejects requests exceeding
// rpm requests-per-minute per source IP with HTTP 429, including rate-limit headers.
func NewRateLimitMiddleware(rpm int) func(http.Handler) http.Handler {
	if rpm <= 0 {
		rpm = 5
	}
	limiter := newIPRateLimiter(rate.Every(time.Minute/time.Duration(rpm)), rpm)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractClientIP(r)
			l := limiter.getLimiter(ip)
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", rpm))
			if !l.Allow() {
				res := l.Reserve()
				delay := int(res.Delay().Seconds()) + 1
				res.Cancel()
				w.Header().Set("Retry-After", fmt.Sprintf("%d", delay))
				w.Header().Set("X-RateLimit-Remaining", "0")
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitMiddleware rejects requests that exceed the per-IP rate limit with
// HTTP 429. Uses the default authLimiter (5 rpm). Prefer NewRateLimitMiddleware
// for configurable limits.
func RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := extractClientIP(r)
		if !authLimiter.getLimiter(ip).Allow() {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Flush forwards the Flush call to the underlying ResponseWriter if it supports it,
// preserving http.Flusher compatibility required for SSE.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
