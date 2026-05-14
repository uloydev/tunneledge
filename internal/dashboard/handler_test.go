package dashboard_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"tunneledge/internal/dashboard"
	"tunneledge/internal/domain"
	"tunneledge/pkg/errs"

	"golang.org/x/crypto/bcrypt"
)

// ── Stub repositories ─────────────────────────────────────────────────────────

type stubUserRepo struct {
	users  map[string]*domain.User
	nextID uint
}

func newStubUserRepo() *stubUserRepo {
	return &stubUserRepo{users: make(map[string]*domain.User), nextID: 1}
}

func (r *stubUserRepo) Create(ctx context.Context, u *domain.User) error {
	if _, ok := r.users[u.Email]; ok {
		return errs.New(errs.CodeAlreadyExists, "email already exists")
	}
	u.ID = r.nextID
	r.nextID++
	u.CreatedAt = time.Now()
	u.UpdatedAt = time.Now()
	r.users[u.Email] = u
	return nil
}

func (r *stubUserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	u, ok := r.users[email]
	if !ok {
		return nil, errs.New(errs.CodeNotFound, "user not found")
	}
	return u, nil
}

func (r *stubUserRepo) GetByID(ctx context.Context, id uint) (*domain.User, error) {
	for _, u := range r.users {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, errs.New(errs.CodeNotFound, "user not found")
}

func (r *stubUserRepo) Update(ctx context.Context, u *domain.User) error {
	r.users[u.Email] = u
	return nil
}

func (r *stubUserRepo) Delete(ctx context.Context, id uint) error {
	for k, u := range r.users {
		if u.ID == id {
			delete(r.users, k)
		}
	}
	return nil
}

type stubVerificationRepo struct {
	tokens map[string]*domain.EmailVerification
}

func newStubVerificationRepo() *stubVerificationRepo {
	return &stubVerificationRepo{tokens: make(map[string]*domain.EmailVerification)}
}

func (r *stubVerificationRepo) Create(ctx context.Context, v *domain.EmailVerification) error {
	r.tokens[v.Token] = v
	return nil
}

func (r *stubVerificationRepo) GetByToken(ctx context.Context, token string) (*domain.EmailVerification, error) {
	v, ok := r.tokens[token]
	if !ok {
		return nil, errs.New(errs.CodeNotFound, "token not found")
	}
	return v, nil
}

func (r *stubVerificationRepo) DeleteByUserID(ctx context.Context, userID uint) error {
	for k, v := range r.tokens {
		if v.UserID == userID {
			delete(r.tokens, k)
		}
	}
	return nil
}

type stubAgentRepo struct {
	agents map[uint]*domain.AgentProfile
	nextID uint
}

func newStubAgentRepo() *stubAgentRepo {
	return &stubAgentRepo{agents: make(map[uint]*domain.AgentProfile), nextID: 1}
}

func (r *stubAgentRepo) Create(ctx context.Context, a *domain.AgentProfile) error {
	a.ID = r.nextID
	r.nextID++
	a.CreatedAt = time.Now()
	a.UpdatedAt = time.Now()
	r.agents[a.ID] = a
	return nil
}

func (r *stubAgentRepo) GetByID(ctx context.Context, id uint) (*domain.AgentProfile, error) {
	a, ok := r.agents[id]
	if !ok {
		return nil, errs.New(errs.CodeNotFound, "not found")
	}
	return a, nil
}

func (r *stubAgentRepo) GetByAgentID(ctx context.Context, agentID string) (*domain.AgentProfile, error) {
	for _, a := range r.agents {
		if a.AgentID == agentID {
			return a, nil
		}
	}
	return nil, errs.New(errs.CodeNotFound, "not found")
}

func (r *stubAgentRepo) ListByUserID(ctx context.Context, userID uint) ([]*domain.AgentProfile, error) {
	var out []*domain.AgentProfile
	for _, a := range r.agents {
		if a.UserID == userID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (r *stubAgentRepo) Update(ctx context.Context, a *domain.AgentProfile) error {
	r.agents[a.ID] = a
	return nil
}

func (r *stubAgentRepo) Delete(ctx context.Context, id uint) error {
	delete(r.agents, id)
	return nil
}

type stubTokenRepo struct {
	tokens map[string]string // agentID → hash
}

func newStubTokenRepo() *stubTokenRepo {
	return &stubTokenRepo{tokens: make(map[string]string)}
}

func (r *stubTokenRepo) Create(ctx context.Context, agentID, tokenHash string) error {
	r.tokens[agentID] = tokenHash
	return nil
}

func (r *stubTokenRepo) GetByAgentID(ctx context.Context, agentID string) (string, error) {
	h, ok := r.tokens[agentID]
	if !ok {
		return "", errs.New(errs.CodeNotFound, "not found")
	}
	return h, nil
}

func (r *stubTokenRepo) List(ctx context.Context) (map[string]string, error) {
	out := make(map[string]string, len(r.tokens))
	for k, v := range r.tokens {
		out[k] = v
	}
	return out, nil
}

func (r *stubTokenRepo) Delete(ctx context.Context, agentID string) error {
	delete(r.tokens, agentID)
	return nil
}

type stubTunnelRepo struct {
	tunnels map[uint]*domain.TunnelConfig
	nextID  uint
}

func newStubTunnelRepo() *stubTunnelRepo {
	return &stubTunnelRepo{tunnels: make(map[uint]*domain.TunnelConfig), nextID: 1}
}

func (r *stubTunnelRepo) Create(ctx context.Context, t *domain.TunnelConfig) error {
	t.ID = r.nextID
	r.nextID++
	t.CreatedAt = time.Now()
	t.UpdatedAt = time.Now()
	r.tunnels[t.ID] = t
	return nil
}

func (r *stubTunnelRepo) GetByID(ctx context.Context, id uint) (*domain.TunnelConfig, error) {
	t, ok := r.tunnels[id]
	if !ok {
		return nil, errs.New(errs.CodeNotFound, "not found")
	}
	return t, nil
}

func (r *stubTunnelRepo) ListByAgentProfileID(ctx context.Context, agentProfileID uint) ([]*domain.TunnelConfig, error) {
	var out []*domain.TunnelConfig
	for _, t := range r.tunnels {
		if t.AgentProfileID == agentProfileID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (r *stubTunnelRepo) Update(ctx context.Context, t *domain.TunnelConfig) error {
	r.tunnels[t.ID] = t
	return nil
}

func (r *stubTunnelRepo) Delete(ctx context.Context, id uint) error {
	delete(r.tunnels, id)
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func buildAuthService(users *stubUserRepo, verif *stubVerificationRepo) *dashboard.AuthService {
	return dashboard.NewAuthService(users, verif, nil, "http://localhost", dashboard.JWTConfig{
		Secret: "test-secret-key-32bytes-xxxxxxxxx",
		TTL:    1 * time.Hour,
	})
}

func buildAgentService(agents *stubAgentRepo, tokens *stubTokenRepo, tunnels *stubTunnelRepo) *dashboard.AgentService {
	return dashboard.NewAgentService(agents, tokens, tunnels)
}

// ── Auth handler tests ────────────────────────────────────────────────────────

func TestAuthHandler_Register_Success(t *testing.T) {
	users := newStubUserRepo()
	verif := newStubVerificationRepo()
	svc := buildAuthService(users, verif)
	h := dashboard.NewAuthHandler(svc)

	b, _ := json.Marshal(map[string]string{
		"email":    "alice@example.com",
		"password": "supersecret",
		"name":     "Alice",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Register(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	if _, ok := users.users["alice@example.com"]; !ok {
		t.Fatal("user was not persisted")
	}
}

func TestAuthHandler_Register_InvalidEmail(t *testing.T) {
	users := newStubUserRepo()
	verif := newStubVerificationRepo()
	svc := buildAuthService(users, verif)
	h := dashboard.NewAuthHandler(svc)

	b, _ := json.Marshal(map[string]string{
		"email":    "not-an-email",
		"password": "supersecret",
		"name":     "Bob",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Register(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAuthHandler_Register_DuplicateEmail(t *testing.T) {
	users := newStubUserRepo()
	verif := newStubVerificationRepo()
	svc := buildAuthService(users, verif)
	h := dashboard.NewAuthHandler(svc)

	body, _ := json.Marshal(map[string]string{"email": "dup@example.com", "password": "pass1234", "name": "Dup"})

	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	httptest.NewRecorder() // discard
	h.Register(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	h.Register(w2, req2)

	if w2.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestAuthHandler_Login_Success(t *testing.T) {
	users := newStubUserRepo()
	verif := newStubVerificationRepo()
	svc := buildAuthService(users, verif)
	h := dashboard.NewAuthHandler(svc)

	// Pre-create a verified user.
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	_ = users.Create(context.Background(), &domain.User{
		Email:         "user@example.com",
		PasswordHash:  string(hash),
		Name:          "User",
		EmailVerified: true,
	})

	b, _ := json.Marshal(map[string]string{"email": "user@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Login(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp dashboard.AuthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid response body: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestAuthHandler_Login_WrongPassword(t *testing.T) {
	users := newStubUserRepo()
	verif := newStubVerificationRepo()
	svc := buildAuthService(users, verif)
	h := dashboard.NewAuthHandler(svc)

	hash, _ := bcrypt.GenerateFromPassword([]byte("correct"), bcrypt.MinCost)
	_ = users.Create(context.Background(), &domain.User{
		Email:         "user2@example.com",
		PasswordHash:  string(hash),
		Name:          "User2",
		EmailVerified: true,
	})

	b, _ := json.Marshal(map[string]string{"email": "user2@example.com", "password": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Login(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// ── Agent handler tests ───────────────────────────────────────────────────────

func TestAgentHandler_Create_Success(t *testing.T) {
	agents := newStubAgentRepo()
	tokens := newStubTokenRepo()
	tunnels := newStubTunnelRepo()
	svc := buildAgentService(agents, tokens, tunnels)
	h := dashboard.NewAgentHandler(svc)

	b, _ := json.Marshal(map[string]string{"name": "My Agent", "agent_id": "agent-abc"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), dashboard.ExportedUserIDKey, uint(1))
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	if len(agents.agents) != 1 {
		t.Fatal("agent was not created")
	}
}

func TestAgentHandler_List_Empty(t *testing.T) {
	agents := newStubAgentRepo()
	tokens := newStubTokenRepo()
	tunnels := newStubTunnelRepo()
	svc := buildAgentService(agents, tokens, tunnels)
	h := dashboard.NewAgentHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	ctx := context.WithValue(req.Context(), dashboard.ExportedUserIDKey, uint(99))
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp []dashboard.AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad response: %v", err)
	}
	if len(resp) != 0 {
		t.Fatalf("expected empty list, got %d items", len(resp))
	}
}

// ── Stub repositories ─────────────────────────────────────────────────────────
