package dashboard

import (
	"net/http"
	"strings"

	"tunneledge/internal/domain"
)

type StatusHandler struct {
	sessions domain.SessionRepository
	agents   domain.AgentProfileRepository
}

func NewStatusHandler(sessions domain.SessionRepository, agents domain.AgentProfileRepository) *StatusHandler {
	return &StatusHandler{sessions: sessions, agents: agents}
}

func (h *StatusHandler) AgentStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	agentID, err := parseIDFromPath(r.URL.Path, "/api/v1/agents/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent id")
		return
	}

	agent, err := h.agents.GetByID(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	if agent.UserID != userID {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	tunnelID := "t-" + agent.AgentID
	sess, err := h.sessions.Get(r.Context(), tunnelID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "offline"})
		return
	}

	writeJSON(w, http.StatusOK, SessionResponse{
		TunnelID:      sess.TunnelID,
		AgentID:       sess.AgentID,
		PublicAddr:    sess.PublicAddr,
		LocalAddr:     sess.LocalAddr,
		CreatedAt:     sess.CreatedAt.Unix(),
		LastHeartbeat: sess.LastHeartbeat.Unix(),
	})
}

func (h *StatusHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.sessions.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}

	resp := make([]SessionResponse, 0, len(sessions))
	for _, s := range sessions {
		resp = append(resp, SessionResponse{
			TunnelID:      s.TunnelID,
			AgentID:       s.AgentID,
			PublicAddr:    s.PublicAddr,
			LocalAddr:     s.LocalAddr,
			CreatedAt:     s.CreatedAt.Unix(),
			LastHeartbeat: s.LastHeartbeat.Unix(),
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// DeleteSession disconnects a tunnel session. Only the owner of the agent may do this.
func (h *StatusHandler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// TunnelID is everything after /api/v1/sessions/
	tunnelID := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	tunnelID = strings.TrimSuffix(tunnelID, "/")
	if tunnelID == "" {
		writeError(w, http.StatusBadRequest, "tunnel_id required")
		return
	}

	// Verify ownership: derive agentID from tunnelID format "t-{agentID}"
	agentID := tunnelID
	if len(tunnelID) > 2 && tunnelID[:2] == "t-" {
		agentID = tunnelID[2:]
	}

	agent, err := h.agents.GetByAgentID(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if agent.UserID != userID {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	if err := h.sessions.Deregister(r.Context(), tunnelID); err != nil {
		writeError(w, http.StatusNotFound, "session not found or already closed")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
