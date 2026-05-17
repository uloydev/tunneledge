package dashboard

import (
	"encoding/json"
	"net/http"
	"time"

	"tunneledge/internal/domain"
	"tunneledge/pkg/errs"
)

// AuthHandler handles HTTP requests for authentication.
// revokedRepo and refreshRepo are optional — nil disables the respective feature.
type AuthHandler struct {
	svc         *AuthService
	revokedRepo domain.RevokedJTIRepository   // optional
	refreshRepo domain.RefreshTokenRepository // optional
	auditSvc    *AuditService                 // optional
}

func NewAuthHandler(svc *AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// WithRevocation enables JWT JTI revocation on logout.
func (h *AuthHandler) WithRevocation(revokedRepo domain.RevokedJTIRepository) *AuthHandler {
	h.revokedRepo = revokedRepo
	return h
}

// WithRefreshRepo enables refresh token operations.
func (h *AuthHandler) WithRefreshRepo(repo domain.RefreshTokenRepository) *AuthHandler {
	h.refreshRepo = repo
	return h
}

// WithAuditService enables audit logging for auth events.
func (h *AuthHandler) WithAuditService(svc *AuditService) *AuthHandler {
	h.auditSvc = svc
	return h
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	out, err := h.svc.Register(r.Context(), RegisterInput{
		Email:    req.Email,
		Password: req.Password,
		Name:     req.Name,
	})
	if err != nil {
		writeServiceError(r, w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"message": out.Message})
}

func (h *AuthHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	_, jwtToken, expiresAt, err := h.svc.VerifyEmail(r.Context(), token)
	if err != nil {
		if errs.GetCode(err) == errs.CodeInvalidArg {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "verification failed")
		}
		return
	}

	if jwtToken != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    jwtToken,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Expires:  expiresAt,
		})
		http.Redirect(w, r, "/dashboard?verified=true", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login?verified=true", http.StatusSeeOther)
}

func (h *AuthHandler) ResendVerification(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	h.svc.ResendVerification(r.Context(), req.Email)
	writeJSON(w, http.StatusOK, map[string]string{
		"message": "if that email is registered, a verification link has been sent",
	})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	out, err := h.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		h.auditSvc.Log(domain.AuditAuthFailure, req.Email, "", extractClientIP(r), map[string]any{"reason": err.Error()})
		writeServiceError(r, w, err)
		return
	}

	h.auditSvc.Log(domain.AuditAuthSuccess, req.Email, "", extractClientIP(r), nil)

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    out.Token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  out.ExpiresAt,
	})

	if out.RefreshJTI != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     "tunneledge_refresh",
			Value:    out.RefreshJTI,
			Path:     "/api/v1/auth/refresh",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Expires:  out.RefreshExpiresAt,
		})
	}

	writeJSON(w, http.StatusOK, AuthResponse{
		Token:     out.Token,
		ExpiresAt: out.ExpiresAt.Unix(),
	})
}

// Refresh issues a new short-lived access JWT using a valid refresh token.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("tunneledge_refresh")
	if err != nil || cookie.Value == "" {
		writeError(w, http.StatusUnauthorized, "missing refresh token")
		return
	}

	out, err := h.svc.RefreshAccessToken(r.Context(), cookie.Value)
	if err != nil {
		writeServiceError(r, w, err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    out.Token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  out.ExpiresAt,
	})

	writeJSON(w, http.StatusOK, AuthResponse{Token: out.Token, ExpiresAt: out.ExpiresAt.Unix()})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	// Revoke the current JWT jti if revocation is configured.
	jti := JTIFromContext(r.Context())
	var jtiExpiry time.Time
	if jti != "" {
		// Best-effort expiry from cookie — use TTL as fallback.
		jtiExpiry = time.Now().Add(24 * time.Hour)
	}

	// Revoke refresh token if present.
	var refreshJTI string
	if cookie, err := r.Cookie("tunneledge_refresh"); err == nil {
		refreshJTI = cookie.Value
	}

	if h.revokedRepo != nil || refreshJTI != "" {
		h.svc.RevokeSession(r.Context(), jti, jtiExpiry, refreshJTI, h.revokedRepo)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "tunneledge_refresh",
		Value:    "",
		Path:     "/api/v1/auth/refresh",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
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

	user, err := h.svc.Me(r.Context(), userID)
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

// writeServiceError is defined in errors.go.
