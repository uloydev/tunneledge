package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type TunnelHandler struct {
	svc *TunnelService
}

func NewTunnelHandler(svc *TunnelService) *TunnelHandler {
	return &TunnelHandler{svc: svc}
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

	var req CreateTunnelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tunnel, err := h.svc.Create(r.Context(), userID, agentID, req.Label, req.LocalAddr)
	if err != nil {
		writeServiceError(w, err)
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

	tunnels, err := h.svc.List(r.Context(), userID, agentID)
	if err != nil {
		writeServiceError(w, err)
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

	tunnel, err := h.svc.Get(r.Context(), userID, agentID, tunnelID)
	if err != nil {
		writeServiceError(w, err)
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

	var req UpdateTunnelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tunnel, err := h.svc.Update(r.Context(), userID, agentID, tunnelID, req.Label, req.LocalAddr)
	if err != nil {
		writeServiceError(w, err)
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

	if err := h.svc.Delete(r.Context(), userID, agentID, tunnelID); err != nil {
		writeServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *TunnelHandler) parseAgentID(path string) (uint, error) {
	trimmed := strings.TrimPrefix(path, "/api/v1/agents/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 {
		return 0, strconv.ErrSyntax
	}
	id, err := strconv.ParseUint(parts[0], 10, 64)
	return uint(id), err
}

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
