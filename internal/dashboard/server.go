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
	// Phase 3: token security
	RevokedJTIs         domain.RevokedJTIRepository   // optional
	RefreshTokens       domain.RefreshTokenRepository // optional
	RefreshTokenEnabled bool
	RefreshTokenTTL     time.Duration
	AuthRateLimitRPM    int
	TunnelACLs          domain.TunnelACLRepository // optional
	// Phase 3: audit log
	AuditRepo domain.AuditRepository // optional
}

func NewServer(opts ServerOptions) *Server {
	// ── Services ──────────────────────────────────────────────
	authSvc := NewAuthService(opts.Users, opts.Verifications, opts.EmailService, opts.BaseURL, opts.JWTCfg)
	if opts.RefreshTokenEnabled && opts.RefreshTokens != nil {
		ttl := opts.RefreshTokenTTL
		if ttl <= 0 {
			ttl = 7 * 24 * time.Hour
		}
		authSvc.WithRefreshTokens(opts.RefreshTokens, ttl)
	}
	agentSvc := NewAgentService(opts.Agents, opts.Tokens, opts.Tunnels)
	tunnelSvc := NewTunnelService(opts.Tunnels, opts.Agents)

	// ── Handlers ──────────────────────────────────────────────
	authHandler := NewAuthHandler(authSvc)
	if opts.RevokedJTIs != nil {
		authHandler.WithRevocation(opts.RevokedJTIs)
	}
	if opts.RefreshTokens != nil {
		authHandler.WithRefreshRepo(opts.RefreshTokens)
	}
	if opts.AuditRepo != nil {
		authHandler.WithAuditService(NewAuditService(opts.AuditRepo))
	}
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
	mux.HandleFunc("GET /partials/audit", requireCookie(opts.JWTCfg.Secret, pageHandler.Partial("audit")))

	// Root → landing page
	mux.HandleFunc("GET /{$}", pageHandler.LandingPage)
	mux.HandleFunc("GET /docs", pageHandler.DocsPage)

	// ── API routes ────────────────────────────────────────────
	// Public (no auth)
	authRL := NewRateLimitMiddleware(opts.AuthRateLimitRPM)
	mux.Handle("POST /api/v1/auth/register", authRL(http.HandlerFunc(authHandler.Register)))
	mux.Handle("POST /api/v1/auth/login", authRL(http.HandlerFunc(authHandler.Login)))
	mux.HandleFunc("POST /api/v1/auth/refresh", authHandler.Refresh)
	mux.HandleFunc("POST /api/v1/auth/resend-verification", authHandler.ResendVerification)

	authMw := JWTAuthMiddleware(JWTMiddlewareOptions{
		Secret:      opts.JWTCfg.Secret,
		RevokedRepo: opts.RevokedJTIs,
	})

	mux.Handle("POST /api/v1/auth/logout", authMw(http.HandlerFunc(authHandler.Logout)))
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

	// Tunnel ACLs (Phase 3) — per-agent IP access control rules
	if opts.TunnelACLs != nil {
		aclHandler := NewACLHandler(opts.TunnelACLs, agentSvc)
		mux.Handle("GET /api/v1/agents/{id}/acls", authMw(http.HandlerFunc(aclHandler.List)))
		mux.Handle("POST /api/v1/agents/{id}/acls", authMw(http.HandlerFunc(aclHandler.Create)))
		mux.Handle("DELETE /api/v1/agents/{id}/acls/{aclID}", authMw(http.HandlerFunc(aclHandler.Delete)))
	}

	// Status
	mux.Handle("GET /api/v1/agents/{id}/status", authMw(http.HandlerFunc(statusHandler.AgentStatus)))
	mux.Handle("GET /api/v1/sessions", authMw(http.HandlerFunc(statusHandler.ListSessions)))
	mux.Handle("DELETE /api/v1/sessions/{tunnelID}", authMw(http.HandlerFunc(statusHandler.DeleteSession)))

	// SSE — real-time event stream
	mux.Handle("GET /api/v1/events", authMw(http.HandlerFunc(sseHandler.Stream)))

	// Audit log (Phase 3)
	if opts.AuditRepo != nil {
		auditHandler := NewAuditHandler(opts.AuditRepo)
		mux.Handle("GET /api/v1/audit", authMw(http.HandlerFunc(auditHandler.List)))
	}

	// Health
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	handler := RequestIDMiddleware(TracingMiddleware(CORSMiddleware(LoggingMiddleware(mux))))

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

func (s *Server) Start() error {
	log.Info().Str("addr", s.httpServer.Addr).Msg("starting dashboard server")
	return s.httpServer.ListenAndServe()
}

func (s *Server) Stop(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(shutdownCtx)
}
