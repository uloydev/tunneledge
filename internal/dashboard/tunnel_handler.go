package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"tunneledge/internal/domain"
)

type TunnelHandler struct {
	tunnels domain.TunnelDefinitionRepository
	agents  domain.AgentProfileRepository
}

func NewTunnelHandler(tunnels domain.TunnelDefinitionRepository, agents domain.AgentProfileRepository) *TunnelHandler {
	return &TunnelHandler{tunnels: tunnels, agents: agents}
}

func (h *TunnelHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	agentID, err := h.parseAgentID(r.URL.Path)
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

	var req CreateTunnelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := domain.ValidateLabel(req.Label); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := domain.ValidateLocalAddr(req.LocalAddr); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tunnel := &domain.TunnelDefinition{
		AgentProfileID: agentID,
		Label:          req.Label,
		LocalAddr:      req.LocalAddr,
	}

	if err := h.tunnels.Create(r.Context(), tunnel); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create tunnel")
		return
	}

	writeJSON(w, http.StatusCreated, TunnelResponse{
		ID:        tunnel.ID,
		Label:     tunnel.Label,
		LocalAddr: tunnel.LocalAddr,
		CreatedAt: tunnel.CreatedAt,
		UpdatedAt: tunnel.UpdatedAt,
	})
}

func (h *TunnelHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	agentID, err := h.parseAgentID(r.URL.Path)
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

	tunnels, err := h.tunnels.ListByAgentProfileID(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tunnels")
		return
	}

	resp := make([]TunnelResponse, 0, len(tunnels))
	for _, t := range tunnels {
		resp = append(resp, TunnelResponse{
			ID:        t.ID,
			Label:     t.Label,
			LocalAddr: t.LocalAddr,
			CreatedAt: t.CreatedAt,
			UpdatedAt: t.UpdatedAt,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *TunnelHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	agentID, tunnelID, err := h.parseAgentAndTunnelID(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid path")
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

	tunnel, err := h.tunnels.GetByID(r.Context(), tunnelID)
	if err != nil {
		writeError(w, http.StatusNotFound, "tunnel not found")
		return
	}

	if tunnel.AgentProfileID != agentID {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	writeJSON(w, http.StatusOK, TunnelResponse{
		ID:        tunnel.ID,
		Label:     tunnel.Label,
		LocalAddr: tunnel.LocalAddr,
		CreatedAt: tunnel.CreatedAt,
		UpdatedAt: tunnel.UpdatedAt,
	})
}

func (h *TunnelHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	agentID, tunnelID, err := h.parseAgentAndTunnelID(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid path")
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

	tunnel, err := h.tunnels.GetByID(r.Context(), tunnelID)
	if err != nil {
		writeError(w, http.StatusNotFound, "tunnel not found")
		return
	}

	if tunnel.AgentProfileID != agentID {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	var req UpdateTunnelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Label != "" {
		if err := domain.ValidateLabel(req.Label); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		tunnel.Label = req.Label
	}

	if req.LocalAddr != "" {
		if err := domain.ValidateLocalAddr(req.LocalAddr); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		tunnel.LocalAddr = req.LocalAddr
	}

	if err := h.tunnels.Update(r.Context(), tunnel); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update tunnel")
		return
	}

	writeJSON(w, http.StatusOK, TunnelResponse{
		ID:        tunnel.ID,
		Label:     tunnel.Label,
		LocalAddr: tunnel.LocalAddr,
		CreatedAt: tunnel.CreatedAt,
		UpdatedAt: tunnel.UpdatedAt,
	})
}

func (h *TunnelHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	agentID, tunnelID, err := h.parseAgentAndTunnelID(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid path")
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

	tunnel, err := h.tunnels.GetByID(r.Context(), tunnelID)
	if err != nil {
		writeError(w, http.StatusNotFound, "tunnel not found")
		return
	}

	if tunnel.AgentProfileID != agentID {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	if err := h.tunnels.Delete(r.Context(), tunnelID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete tunnel")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// parseAgentID extracts agent ID from paths like /api/v1/agents/{id}/tunnels
func (h *TunnelHandler) parseAgentID(path string) (uint, error) {
	trimmed := strings.TrimPrefix(path, "/api/v1/agents/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 {
		return 0, strconv.ErrSyntax
	}
	id, err := strconv.ParseUint(parts[0], 10, 64)
	return uint(id), err
}

// parseAgentAndTunnelID extracts IDs from paths like /api/v1/agents/{id}/tunnels/{tid}
func (h *TunnelHandler) parseAgentAndTunnelID(path string) (uint, uint, error) {
	trimmed := strings.TrimPrefix(path, "/api/v1/agents/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 3 {
		return 0, 0, strconv.ErrSyntax
	}
	agentID, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	tunnelID, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return uint(agentID), uint(tunnelID), nil
}
