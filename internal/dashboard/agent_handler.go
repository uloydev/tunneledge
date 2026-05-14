package dashboard

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"tunneledge/internal/domain"

	"golang.org/x/crypto/bcrypt"
)

type AgentHandler struct {
	agents  domain.AgentProfileRepository
	tokens  domain.TokenRepository
	tunnels domain.TunnelDefinitionRepository
}

func NewAgentHandler(agents domain.AgentProfileRepository, tokens domain.TokenRepository, tunnels domain.TunnelDefinitionRepository) *AgentHandler {
	return &AgentHandler{agents: agents, tokens: tokens, tunnels: tunnels}
}

func (h *AgentHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req CreateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.AgentID == "" {
		writeError(w, http.StatusBadRequest, "name and agent_id are required")
		return
	}

	if err := domain.ValidateLabel(req.AgentID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	rawToken := generateRawToken()
	hash, err := bcrypt.GenerateFromPassword([]byte(rawToken), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	agent := &domain.AgentProfile{
		UserID:    userID,
		Name:      req.Name,
		AgentID:   req.AgentID,
		TokenHash: string(hash),
	}

	if err := h.agents.Create(r.Context(), agent); err != nil {
		writeError(w, http.StatusConflict, "agent_id already exists")
		return
	}

	if err := h.tokens.Create(r.Context(), req.AgentID, string(hash)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store token")
		return
	}

	writeJSON(w, http.StatusCreated, AgentTokenResponse{
		AgentID: req.AgentID,
		Token:   rawToken,
	})
}

func (h *AgentHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	agents, err := h.agents.ListByUserID(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}

	resp := make([]AgentResponse, 0, len(agents))
	for _, a := range agents {
		resp = append(resp, AgentResponse{
			ID:        a.ID,
			Name:      a.Name,
			AgentID:   a.AgentID,
			CreatedAt: a.CreatedAt,
			UpdatedAt: a.UpdatedAt,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *AgentHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := parseIDFromPath(r.URL.Path, "/api/v1/agents/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent id")
		return
	}

	agent, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	if agent.UserID != userID {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	writeJSON(w, http.StatusOK, AgentResponse{
		ID:        agent.ID,
		Name:      agent.Name,
		AgentID:   agent.AgentID,
		CreatedAt: agent.CreatedAt,
		UpdatedAt: agent.UpdatedAt,
	})
}

func (h *AgentHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := parseIDFromPath(r.URL.Path, "/api/v1/agents/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent id")
		return
	}

	agent, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	if agent.UserID != userID {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	var req UpdateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != "" {
		agent.Name = req.Name
	}

	if err := h.agents.Update(r.Context(), agent); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update agent")
		return
	}

	writeJSON(w, http.StatusOK, AgentResponse{
		ID:        agent.ID,
		Name:      agent.Name,
		AgentID:   agent.AgentID,
		CreatedAt: agent.CreatedAt,
		UpdatedAt: agent.UpdatedAt,
	})
}

func (h *AgentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := parseIDFromPath(r.URL.Path, "/api/v1/agents/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent id")
		return
	}

	agent, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	if agent.UserID != userID {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	_ = h.tokens.Delete(r.Context(), agent.AgentID)

	if err := h.agents.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete agent")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *AgentHandler) RotateToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse /api/v1/agents/{id}/rotate-token
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/agents/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}

	id, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent id")
		return
	}

	agent, err := h.agents.GetByID(r.Context(), uint(id))
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	if agent.UserID != userID {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	rawToken := generateRawToken()
	hash, err := bcrypt.GenerateFromPassword([]byte(rawToken), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	agent.TokenHash = string(hash)
	if err := h.agents.Update(r.Context(), agent); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update token")
		return
	}

	_ = h.tokens.Delete(r.Context(), agent.AgentID)
	_ = h.tokens.Create(r.Context(), agent.AgentID, string(hash))

	writeJSON(w, http.StatusOK, AgentTokenResponse{
		AgentID: agent.AgentID,
		Token:   rawToken,
	})
}

func generateRawToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("failed to generate random token")
	}
	return "te_" + hex.EncodeToString(b)
}

func parseIDFromPath(path, prefix string) (uint, error) {
	trimmed := strings.TrimPrefix(path, prefix)
	trimmed = strings.TrimSuffix(trimmed, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 {
		return 0, strconv.ErrSyntax
	}
	id, err := strconv.ParseUint(parts[0], 10, 64)
	return uint(id), err
}
