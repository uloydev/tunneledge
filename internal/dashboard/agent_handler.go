package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"tunneledge/internal/domain"
)

type AgentHandler struct {
	svc *AgentService
}

func NewAgentHandler(svc *AgentService) *AgentHandler {
	return &AgentHandler{svc: svc}
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

	out, err := h.svc.Create(r.Context(), userID, req.Name, req.AgentID)
	if err != nil {
		writeServiceError(r, w, err)
		return
	}

	writeJSON(w, http.StatusCreated, AgentTokenResponse{
		AgentID: out.Agent.AgentID,
		Token:   out.RawToken,
	})
}

func (h *AgentHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	agents, err := h.svc.List(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}

	resp := make([]AgentResponse, 0, len(agents))
	for _, a := range agents {
		resp = append(resp, agentToResponse(a))
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

	agent, err := h.svc.Get(r.Context(), userID, id)
	if err != nil {
		writeServiceError(r, w, err)
		return
	}

	writeJSON(w, http.StatusOK, agentToResponse(agent))
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

	var req UpdateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	agent, err := h.svc.Update(r.Context(), userID, id, req.Name)
	if err != nil {
		writeServiceError(r, w, err)
		return
	}

	writeJSON(w, http.StatusOK, agentToResponse(agent))
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

	if err := h.svc.Delete(r.Context(), userID, id); err != nil {
		writeServiceError(r, w, err)
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

	rawID, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent id")
		return
	}

	out, err := h.svc.RotateToken(r.Context(), userID, uint(rawID))
	if err != nil {
		writeServiceError(r, w, err)
		return
	}

	writeJSON(w, http.StatusOK, AgentTokenResponse{
		AgentID: out.AgentID,
		Token:   out.RawToken,
	})
}

func agentToResponse(a *domain.AgentProfile) AgentResponse {
	return AgentResponse{
		ID:              a.ID,
		Name:            a.Name,
		AgentID:         a.AgentID,
		TokenExpiresAt:  a.TokenExpiresAt,
		LastUsedAt:      a.LastUsedAt,
		FailedAuthCount: a.FailedAuthCount,
		LockedUntil:     a.LockedUntil,
		CreatedAt:       a.CreatedAt,
		UpdatedAt:       a.UpdatedAt,
	}
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
