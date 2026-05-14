package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"tunneledge/internal/domain"
	"tunneledge/internal/email"
	"tunneledge/pkg/errs"

	"golang.org/x/crypto/bcrypt"
)

// ── Auth service ──────────────────────────────────────────────────────────────

// AuthService encapsulates all authentication business logic, keeping
// HTTP handlers thin and independently testable.
type AuthService struct {
	users         domain.UserRepository
	verifications domain.EmailVerificationRepository
	emailSvc      *email.Service
	baseURL       string
	jwtCfg        JWTConfig
}

func NewAuthService(
	users domain.UserRepository,
	verifications domain.EmailVerificationRepository,
	emailSvc *email.Service,
	baseURL string,
	jwtCfg JWTConfig,
) *AuthService {
	return &AuthService{
		users:         users,
		verifications: verifications,
		emailSvc:      emailSvc,
		baseURL:       baseURL,
		jwtCfg:        jwtCfg,
	}
}

// RegisterInput carries validated registration data.
type RegisterInput struct {
	Email    string
	Password string
	Name     string
}

// RegisterOutput is the result of a successful registration.
type RegisterOutput struct {
	Message string
}

func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*RegisterOutput, error) {
	if err := domain.ValidateEmail(in.Email); err != nil {
		return nil, err
	}
	if len(in.Password) < 8 {
		return nil, errs.New(errs.CodeInvalidArg, "password must be at least 8 characters")
	}
	if in.Name == "" {
		return nil, errs.New(errs.CodeInvalidArg, "name is required")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	user := &domain.User{
		Email:         in.Email,
		PasswordHash:  string(hash),
		Name:          in.Name,
		EmailVerified: false,
	}

	if err := s.users.Create(ctx, user); err != nil {
		return nil, errs.Wrap(errs.CodeAlreadyExists, "user already exists", err)
	}

	token := generateVerificationToken()
	verification := &domain.EmailVerification{
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := s.verifications.Create(ctx, verification); err != nil {
		return nil, fmt.Errorf("failed to create email verification: %w", err)
	}

	verifyURL := fmt.Sprintf("%s/verify?token=%s", s.baseURL, token)
	if s.emailSvc != nil {
		go func() { _ = s.emailSvc.SendVerification(user.Email, user.Name, verifyURL) }()
	}

	return &RegisterOutput{Message: "registration successful, please check your email to verify your account"}, nil
}

// LoginOutput carries the signed JWT and its expiry.
type LoginOutput struct {
	Token     string
	ExpiresAt time.Time
}

func (s *AuthService) Login(ctx context.Context, email, password string) (*LoginOutput, error) {
	if email == "" || password == "" {
		return nil, errs.New(errs.CodeInvalidArg, "email and password are required")
	}

	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		// Use a constant-time comparison path even for "not found" to avoid user enumeration.
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$12$placeholder"), []byte(password))
		return nil, errs.New(errs.CodeUnauthorized, "invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, errs.New(errs.CodeUnauthorized, "invalid credentials")
	}

	if !user.EmailVerified {
		return nil, errs.New(errs.CodeForbidden, "please verify your email before logging in")
	}

	jwtToken, expiresAt, err := GenerateJWT(s.jwtCfg, user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	return &LoginOutput{Token: jwtToken, ExpiresAt: expiresAt}, nil
}

func (s *AuthService) VerifyEmail(ctx context.Context, token string) (userID uint, jwtToken string, expiresAt time.Time, err error) {
	if token == "" {
		return 0, "", time.Time{}, errs.New(errs.CodeInvalidArg, "missing verification token")
	}

	v, err := s.verifications.GetByToken(ctx, token)
	if err != nil {
		return 0, "", time.Time{}, errs.New(errs.CodeInvalidArg, "invalid or expired verification token")
	}

	if time.Now().After(v.ExpiresAt) {
		return 0, "", time.Time{}, errs.New(errs.CodeInvalidArg, "verification token has expired")
	}

	user, err := s.users.GetByID(ctx, v.UserID)
	if err != nil {
		return 0, "", time.Time{}, fmt.Errorf("user not found: %w", err)
	}

	user.EmailVerified = true
	if err := s.users.Update(ctx, user); err != nil {
		return 0, "", time.Time{}, fmt.Errorf("failed to verify email: %w", err)
	}

	_ = s.verifications.DeleteByUserID(ctx, v.UserID)

	signed, exp, err := GenerateJWT(s.jwtCfg, user.ID)
	if err != nil {
		return user.ID, "", time.Time{}, nil // verified, but JWT failed — caller can redirect to login
	}
	return user.ID, signed, exp, nil
}

func (s *AuthService) ResendVerification(ctx context.Context, emailAddr string) {
	user, err := s.users.GetByEmail(ctx, emailAddr)
	if err != nil || user.EmailVerified {
		return // Silently succeed to avoid email enumeration.
	}

	_ = s.verifications.DeleteByUserID(ctx, user.ID)

	token := generateVerificationToken()
	v := &domain.EmailVerification{
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	_ = s.verifications.Create(ctx, v)

	verifyURL := fmt.Sprintf("%s/verify?token=%s", s.baseURL, token)
	if s.emailSvc != nil {
		go func() { _ = s.emailSvc.SendVerification(user.Email, user.Name, verifyURL) }()
	}
}

func (s *AuthService) Me(ctx context.Context, userID uint) (*domain.User, error) {
	return s.users.GetByID(ctx, userID)
}

// ── Agent service ─────────────────────────────────────────────────────────────

// AgentService encapsulates agent profile and token management logic.
type AgentService struct {
	agents  domain.AgentProfileRepository
	tokens  domain.TokenRepository
	tunnels domain.TunnelConfigRepository
}

func NewAgentService(
	agents domain.AgentProfileRepository,
	tokens domain.TokenRepository,
	tunnels domain.TunnelConfigRepository,
) *AgentService {
	return &AgentService{agents: agents, tokens: tokens, tunnels: tunnels}
}

// CreateAgentOutput carries the new agent's public data and the raw token (shown once).
type CreateAgentOutput struct {
	Agent    *domain.AgentProfile
	RawToken string
}

func (s *AgentService) Create(ctx context.Context, userID uint, name, agentID string) (*CreateAgentOutput, error) {
	if err := domain.ValidateLabel(agentID); err != nil {
		return nil, err
	}

	rawToken := generateRawToken()
	hash, err := bcrypt.GenerateFromPassword([]byte(rawToken), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	agent := &domain.AgentProfile{
		UserID:    userID,
		Name:      name,
		AgentID:   agentID,
		TokenHash: string(hash),
	}

	if err := s.agents.Create(ctx, agent); err != nil {
		return nil, errs.Wrap(errs.CodeAlreadyExists, "agent_id already exists", err)
	}

	if err := s.tokens.Create(ctx, agentID, string(hash)); err != nil {
		return nil, fmt.Errorf("failed to store token: %w", err)
	}

	return &CreateAgentOutput{Agent: agent, RawToken: rawToken}, nil
}

func (s *AgentService) List(ctx context.Context, userID uint) ([]*domain.AgentProfile, error) {
	return s.agents.ListByUserID(ctx, userID)
}

func (s *AgentService) Get(ctx context.Context, userID, agentID uint) (*domain.AgentProfile, error) {
	agent, err := s.agents.GetByID(ctx, agentID)
	if err != nil {
		return nil, errs.New(errs.CodeNotFound, "agent not found")
	}
	if agent.UserID != userID {
		return nil, errs.New(errs.CodeForbidden, "access denied")
	}
	return agent, nil
}

func (s *AgentService) Update(ctx context.Context, userID, agentID uint, name string) (*domain.AgentProfile, error) {
	agent, err := s.Get(ctx, userID, agentID)
	if err != nil {
		return nil, err
	}
	agent.Name = name
	if err := s.agents.Update(ctx, agent); err != nil {
		return nil, fmt.Errorf("failed to update agent: %w", err)
	}
	return agent, nil
}

func (s *AgentService) Delete(ctx context.Context, userID, agentID uint) error {
	if _, err := s.Get(ctx, userID, agentID); err != nil {
		return err
	}
	return s.agents.Delete(ctx, agentID)
}

// RotateTokenOutput carries the new raw token.
type RotateTokenOutput struct {
	AgentID  string
	RawToken string
}

func (s *AgentService) RotateToken(ctx context.Context, userID, agentID uint) (*RotateTokenOutput, error) {
	agent, err := s.Get(ctx, userID, agentID)
	if err != nil {
		return nil, err
	}

	rawToken := generateRawToken()
	hash, err := bcrypt.GenerateFromPassword([]byte(rawToken), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	agent.TokenHash = string(hash)
	if err := s.agents.Update(ctx, agent); err != nil {
		return nil, fmt.Errorf("failed to update agent token: %w", err)
	}

	_ = s.tokens.Delete(ctx, agent.AgentID)
	if err := s.tokens.Create(ctx, agent.AgentID, string(hash)); err != nil {
		return nil, fmt.Errorf("failed to store rotated token: %w", err)
	}

	return &RotateTokenOutput{AgentID: agent.AgentID, RawToken: rawToken}, nil
}

// ── Tunnel service ────────────────────────────────────────────────────────────

// TunnelService manages tunnel definitions (user-configured tunnels).
type TunnelService struct {
	tunnels domain.TunnelConfigRepository
	agents  domain.AgentProfileRepository
}

func NewTunnelService(tunnels domain.TunnelConfigRepository, agents domain.AgentProfileRepository) *TunnelService {
	return &TunnelService{tunnels: tunnels, agents: agents}
}

func (s *TunnelService) Create(ctx context.Context, userID, agentID uint, label, localAddr string) (*domain.TunnelConfig, error) {
	agent, err := s.agents.GetByID(ctx, agentID)
	if err != nil {
		return nil, errs.New(errs.CodeNotFound, "agent not found")
	}
	if agent.UserID != userID {
		return nil, errs.New(errs.CodeForbidden, "access denied")
	}
	if err := domain.ValidateLabel(label); err != nil {
		return nil, err
	}
	if err := domain.ValidateLocalAddr(localAddr); err != nil {
		return nil, err
	}

	t := &domain.TunnelConfig{
		AgentProfileID: agentID,
		Label:          label,
		LocalAddr:      localAddr,
	}
	if err := s.tunnels.Create(ctx, t); err != nil {
		return nil, fmt.Errorf("failed to create tunnel: %w", err)
	}
	return t, nil
}

func (s *TunnelService) List(ctx context.Context, userID, agentID uint) ([]*domain.TunnelConfig, error) {
	agent, err := s.agents.GetByID(ctx, agentID)
	if err != nil {
		return nil, errs.New(errs.CodeNotFound, "agent not found")
	}
	if agent.UserID != userID {
		return nil, errs.New(errs.CodeForbidden, "access denied")
	}
	return s.tunnels.ListByAgentProfileID(ctx, agentID)
}

func (s *TunnelService) Get(ctx context.Context, userID, agentID, tunnelID uint) (*domain.TunnelConfig, error) {
	agent, err := s.agents.GetByID(ctx, agentID)
	if err != nil {
		return nil, errs.New(errs.CodeNotFound, "agent not found")
	}
	if agent.UserID != userID {
		return nil, errs.New(errs.CodeForbidden, "access denied")
	}
	t, err := s.tunnels.GetByID(ctx, tunnelID)
	if err != nil {
		return nil, errs.New(errs.CodeNotFound, "tunnel not found")
	}
	if t.AgentProfileID != agentID {
		return nil, errs.New(errs.CodeForbidden, "access denied")
	}
	return t, nil
}

func (s *TunnelService) Update(ctx context.Context, userID, agentID, tunnelID uint, label, localAddr string) (*domain.TunnelConfig, error) {
	t, err := s.Get(ctx, userID, agentID, tunnelID)
	if err != nil {
		return nil, err
	}
	if label != "" {
		if err := domain.ValidateLabel(label); err != nil {
			return nil, err
		}
		t.Label = label
	}
	if localAddr != "" {
		if err := domain.ValidateLocalAddr(localAddr); err != nil {
			return nil, err
		}
		t.LocalAddr = localAddr
	}
	if err := s.tunnels.Update(ctx, t); err != nil {
		return nil, fmt.Errorf("failed to update tunnel: %w", err)
	}
	return t, nil
}

func (s *TunnelService) Delete(ctx context.Context, userID, agentID, tunnelID uint) error {
	if _, err := s.Get(ctx, userID, agentID, tunnelID); err != nil {
		return err
	}
	return s.tunnels.Delete(ctx, tunnelID)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func generateVerificationToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func generateRawToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}
