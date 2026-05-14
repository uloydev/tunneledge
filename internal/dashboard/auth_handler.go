package dashboard

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"tunneledge/internal/domain"
	"tunneledge/internal/email"

	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	users         domain.UserRepository
	verifications domain.EmailVerificationRepository
	emailSvc      *email.Service
	baseURL       string
	jwtCfg        JWTConfig
}

func NewAuthHandler(users domain.UserRepository, verifications domain.EmailVerificationRepository, emailSvc *email.Service, baseURL string, jwtCfg JWTConfig) *AuthHandler {
	return &AuthHandler{
		users:         users,
		verifications: verifications,
		emailSvc:      emailSvc,
		baseURL:       baseURL,
		jwtCfg:        jwtCfg,
	}
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "email, password, and name are required")
		return
	}

	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	user := &domain.User{
		Email:         req.Email,
		PasswordHash:  string(hash),
		Name:          req.Name,
		EmailVerified: false,
	}

	if err := h.users.Create(r.Context(), user); err != nil {
		writeError(w, http.StatusConflict, "user already exists")
		return
	}

	// Generate verification token
	token := generateVerificationToken()
	verification := &domain.EmailVerification{
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := h.verifications.Create(r.Context(), verification); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create verification")
		return
	}

	// Send verification email
	verifyURL := fmt.Sprintf("%s/verify?token=%s", h.baseURL, token)
	go func() {
		_ = h.emailSvc.SendVerification(user.Email, user.Name, verifyURL)
	}()

	writeJSON(w, http.StatusCreated, map[string]string{
		"message": "registration successful, please check your email to verify your account",
	})
}

func (h *AuthHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "missing verification token")
		return
	}

	v, err := h.verifications.GetByToken(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid or expired verification token")
		return
	}

	if time.Now().After(v.ExpiresAt) {
		writeError(w, http.StatusBadRequest, "verification token has expired")
		return
	}

	user, err := h.users.GetByID(r.Context(), v.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "user not found")
		return
	}

	user.EmailVerified = true
	if err := h.users.Update(r.Context(), user); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify email")
		return
	}

	_ = h.verifications.DeleteByUserID(r.Context(), v.UserID)

	// Auto-login after verification: set cookie and redirect
	jwtToken, expiresAt, err := GenerateJWT(h.jwtCfg, user.ID)
	if err != nil {
		// Still verified, just redirect to login
		http.Redirect(w, r, "/login?verified=true", http.StatusSeeOther)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    jwtToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	})
	http.Redirect(w, r, "/dashboard?verified=true", http.StatusSeeOther)
}

func (h *AuthHandler) ResendVerification(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := h.users.GetByEmail(r.Context(), req.Email)
	if err != nil {
		// Don't reveal whether email exists
		writeJSON(w, http.StatusOK, map[string]string{"message": "if that email is registered, a verification link has been sent"})
		return
	}

	if user.EmailVerified {
		writeJSON(w, http.StatusOK, map[string]string{"message": "email already verified"})
		return
	}

	_ = h.verifications.DeleteByUserID(r.Context(), user.ID)

	token := generateVerificationToken()
	verification := &domain.EmailVerification{
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	_ = h.verifications.Create(r.Context(), verification)

	verifyURL := fmt.Sprintf("%s/verify?token=%s", h.baseURL, token)
	go func() {
		_ = h.emailSvc.SendVerification(user.Email, user.Name, verifyURL)
	}()

	writeJSON(w, http.StatusOK, map[string]string{"message": "if that email is registered, a verification link has been sent"})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	user, err := h.users.GetByEmail(r.Context(), req.Email)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if !user.EmailVerified {
		writeError(w, http.StatusForbidden, "please verify your email before logging in")
		return
	}

	token, expiresAt, err := GenerateJWT(h.jwtCfg, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	// Set cookie for web UI
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	})

	writeJSON(w, http.StatusOK, AuthResponse{
		Token:     token,
		ExpiresAt: expiresAt.Unix(),
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.users.GetByID(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	writeJSON(w, http.StatusOK, UserResponse{
		ID:        user.ID,
		Email:     user.Email,
		Name:      user.Name,
		CreatedAt: user.CreatedAt,
	})
}

func generateVerificationToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("failed to generate random token")
	}
	return hex.EncodeToString(b)
}
