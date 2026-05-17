package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"tunneledge/internal/domain"
)

// ACLHandler handles CRUD for per-agent tunnel ACL rules.
// ACLs are keyed by the agent's string TunnelID (e.g. "t-my-server") and
// are evaluated by the gateway on every public connection.
type ACLHandler struct {
	repo     domain.TunnelACLRepository
	agentSvc *AgentService
}

func NewACLHandler(repo domain.TunnelACLRepository, agentSvc *AgentService) *ACLHandler {
	return &ACLHandler{repo: repo, agentSvc: agentSvc}
}

// List handles GET /api/v1/agents/{id}/acls
func (h *ACLHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	agentDBID, err := parseIDFromPath(r.URL.Path, "/api/v1/agents/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent id")
		return
	}

	agent, err := h.agentSvc.Get(r.Context(), userID, agentDBID)
	if err != nil {
		writeServiceError(r, w, err)
		return
	}

	tunnelID := domain.NewTunnelID(agent.AgentID).String()
	acls, err := h.repo.List(r.Context(), tunnelID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list ACLs")
		return
	}

	resp := make([]TunnelACLResponse, 0, len(acls))
	for _, a := range acls {
		resp = append(resp, TunnelACLResponse{
			ID:        a.ID,
			TunnelID:  a.TunnelID,
			ACLType:   a.ACLType,
			CIDR:      a.CIDR,
			CreatedAt: a.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// Create handles POST /api/v1/agents/{id}/acls
func (h *ACLHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	agentDBID, err := parseIDFromPath(r.URL.Path, "/api/v1/agents/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent id")
		return
	}

	agent, err := h.agentSvc.Get(r.Context(), userID, agentDBID)
	if err != nil {
		writeServiceError(r, w, err)
		return
	}

	var req CreateTunnelACLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.CIDR == "" || req.ACLType == "" {
		writeError(w, http.StatusBadRequest, "cidr and acl_type are required")
		return
	}
	if req.ACLType != "allow" && req.ACLType != "deny" {
		writeError(w, http.StatusBadRequest, "acl_type must be 'allow' or 'deny'")
		return
	}

	acl := &domain.TunnelACL{
		TunnelID: domain.NewTunnelID(agent.AgentID).String(),
		ACLType:  req.ACLType,
		CIDR:     req.CIDR,
	}
	if err := h.repo.Create(r.Context(), acl); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create ACL rule")
		return
	}

	writeJSON(w, http.StatusCreated, TunnelACLResponse{
		ID:        acl.ID,
		TunnelID:  acl.TunnelID,
		ACLType:   acl.ACLType,
		CIDR:      acl.CIDR,
		CreatedAt: acl.CreatedAt,
	})
}

// Delete handles DELETE /api/v1/agents/{id}/acls/{aclID}
func (h *ACLHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse /api/v1/agents/{id}/acls/{aclID}
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/agents/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 3 {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	agentDBID, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent id")
		return
	}
	aclID, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid acl id")
		return
	}

	// Verify agent belongs to this user before deleting.
	if _, err := h.agentSvc.Get(r.Context(), userID, uint(agentDBID)); err != nil {
		writeServiceError(r, w, err)
		return
	}

	if err := h.repo.Delete(r.Context(), uint(aclID)); err != nil {
		writeError(w, http.StatusNotFound, "ACL rule not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
