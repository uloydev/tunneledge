package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"tunneledge/internal/domain"
	"tunneledge/internal/email"

	"github.com/rs/zerolog/log"
)

type Server struct {
	httpServer *http.Server
	jwtCfg     JWTConfig
}

type ServerOptions struct {
	Addr          string
	JWTCfg        JWTConfig
	BaseURL       string
	Users         domain.UserRepository
	Agents        domain.AgentProfileRepository
	Tokens        domain.TokenRepository
	Tunnels       domain.TunnelConfigRepository
	Sessions      domain.SessionRepository
	Verifications domain.EmailVerificationRepository
	EmailService  *email.Service
}

func NewServer(opts ServerOptions) *Server {
	// ── Services ──────────────────────────────────────────────
	authSvc := NewAuthService(opts.Users, opts.Verifications, opts.EmailService, opts.BaseURL, opts.JWTCfg)
	agentSvc := NewAgentService(opts.Agents, opts.Tokens, opts.Tunnels)
	tunnelSvc := NewTunnelService(opts.Tunnels, opts.Agents)

	// ── Handlers ──────────────────────────────────────────────
	authHandler := NewAuthHandler(authSvc)
	agentHandler := NewAgentHandler(agentSvc)
	tunnelHandler := NewTunnelHandler(tunnelSvc)
	statusHandler := NewStatusHandler(opts.Sessions, opts.Agents)
	sseHandler := NewSSEHandler(opts.Sessions, opts.Agents, opts.Tunnels)
	pageHandler := NewPageHandler()

	mux := http.NewServeMux()

	// ── Web pages ─────────────────────────────────────────────
	mux.HandleFunc("GET /login", pageHandler.LoginPage)
	mux.HandleFunc("GET /register", pageHandler.RegisterPage)
	mux.HandleFunc("GET /verify", authHandler.VerifyEmail)
	mux.HandleFunc("GET /dashboard", requireCookie(opts.JWTCfg.Secret, pageHandler.DashboardPage))
	mux.HandleFunc("GET /dashboard/{page...}", requireCookie(opts.JWTCfg.Secret, pageHandler.DashboardPage))

	// HTMX partials (authenticated)
	mux.HandleFunc("GET /partials/overview", requireCookie(opts.JWTCfg.Secret, pageHandler.Partial("overview")))
	mux.HandleFunc("GET /partials/agents", requireCookie(opts.JWTCfg.Secret, pageHandler.Partial("agents")))
	mux.HandleFunc("GET /partials/sessions", requireCookie(opts.JWTCfg.Secret, pageHandler.Partial("sessions")))

	// Root → landing page
	mux.HandleFunc("GET /{$}", pageHandler.LandingPage)

	// ── API routes ────────────────────────────────────────────
	// Public (no auth)
	mux.Handle("POST /api/v1/auth/register", RateLimitMiddleware(http.HandlerFunc(authHandler.Register)))
	mux.Handle("POST /api/v1/auth/login", RateLimitMiddleware(http.HandlerFunc(authHandler.Login)))
	mux.HandleFunc("POST /api/v1/auth/logout", authHandler.Logout)
	mux.HandleFunc("POST /api/v1/auth/resend-verification", authHandler.ResendVerification)

	authMw := JWTAuthMiddleware(opts.JWTCfg.Secret)

	mux.Handle("GET /api/v1/auth/me", authMw(http.HandlerFunc(authHandler.Me)))

	// Agents
	mux.Handle("POST /api/v1/agents", authMw(http.HandlerFunc(agentHandler.Create)))
	mux.Handle("GET /api/v1/agents", authMw(http.HandlerFunc(agentHandler.List)))
	mux.Handle("GET /api/v1/agents/{id}", authMw(http.HandlerFunc(agentHandler.Get)))
	mux.Handle("PUT /api/v1/agents/{id}", authMw(http.HandlerFunc(agentHandler.Update)))
	mux.Handle("DELETE /api/v1/agents/{id}", authMw(http.HandlerFunc(agentHandler.Delete)))
	mux.Handle("POST /api/v1/agents/{id}/rotate-token", authMw(http.HandlerFunc(agentHandler.RotateToken)))

	// Tunnels (sub-resources of agents)
	mux.Handle("POST /api/v1/agents/{id}/tunnels", authMw(http.HandlerFunc(tunnelHandler.Create)))
	mux.Handle("GET /api/v1/agents/{id}/tunnels", authMw(http.HandlerFunc(tunnelHandler.List)))
	mux.Handle("GET /api/v1/agents/{id}/tunnels/{tid}", authMw(http.HandlerFunc(tunnelHandler.Get)))
	mux.Handle("PUT /api/v1/agents/{id}/tunnels/{tid}", authMw(http.HandlerFunc(tunnelHandler.Update)))
	mux.Handle("DELETE /api/v1/agents/{id}/tunnels/{tid}", authMw(http.HandlerFunc(tunnelHandler.Delete)))

	// Status
	mux.Handle("GET /api/v1/agents/{id}/status", authMw(http.HandlerFunc(statusHandler.AgentStatus)))
	mux.Handle("GET /api/v1/sessions", authMw(http.HandlerFunc(statusHandler.ListSessions)))
	mux.Handle("DELETE /api/v1/sessions/{tunnelID}", authMw(http.HandlerFunc(statusHandler.DeleteSession)))

	// SSE — real-time event stream
	mux.Handle("GET /api/v1/events", authMw(http.HandlerFunc(sseHandler.Stream)))

	// Health
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	handler := RequestIDMiddleware(CORSMiddleware(LoggingMiddleware(mux)))

	return &Server{
		httpServer: &http.Server{
			Addr:              opts.Addr,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
			WriteTimeout:      0, // SSE connections are long-lived
			IdleTimeout:       60 * time.Second,
		},
		jwtCfg: opts.JWTCfg,
	}
}

// requireCookie is a lightweight middleware for web pages: checks session cookie and redirects to /login.
func requireCookie(secret string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil || cookie.Value == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		// Validate token (we just need it to be valid, user info is fetched client-side)
		_, err = parseJWT(secret, cookie.Value)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (s *Server) Start() {
	go func() {
		log.Info().Str("addr", s.httpServer.Addr).Msg("starting dashboard server")
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("dashboard server error")
		}
	}()
}

func (s *Server) Stop(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(shutdownCtx)
}
