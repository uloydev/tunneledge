package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"tunneledge/internal/domain"
)

// SSEHandler streams real-time dashboard events over Server-Sent Events.
// Clients receive:
//
//   - event:stats      — aggregate counts for the current user (every 5 s)
//   - event:sessions   — full session list (every 5 s, same tick)
//   - ": ping"         — keep-alive comment every 20 s
type SSEHandler struct {
	sessions domain.SessionRepository
	agents   domain.AgentProfileRepository
	tunnels  domain.TunnelConfigRepository
}

func NewSSEHandler(sessions domain.SessionRepository, agents domain.AgentProfileRepository, tunnels domain.TunnelConfigRepository) *SSEHandler {
	return &SSEHandler{sessions: sessions, agents: agents, tunnels: tunnels}
}

type sseStatsPayload struct {
	Agents   int `json:"agents"`
	Online   int `json:"online"`
	Tunnels  int `json:"tunnels"`
	Sessions int `json:"sessions"`
}

// Stream is the SSE endpoint: GET /api/v1/events
func (h *SSEHandler) Stream(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported by server", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Prevent nginx / proxies from buffering the stream.
	w.Header().Set("X-Accel-Buffering", "no")

	send := func(event string, data any) {
		b, err := json.Marshal(data)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		flusher.Flush()
	}

	push := func(ctx context.Context) {
		agentList, _ := h.agents.ListByUserID(ctx, userID)
		sessions, _ := h.sessions.List(ctx)

		onlineSet := make(map[string]bool, len(sessions))
		for _, s := range sessions {
			onlineSet[s.AgentID] = true
		}

		online := 0
		tunnelCount := 0
		for _, a := range agentList {
			if onlineSet[a.AgentID] {
				online++
			}
			ts, _ := h.tunnels.ListByAgentProfileID(ctx, a.ID)
			tunnelCount += len(ts)
		}

		send("stats", sseStatsPayload{
			Agents:   len(agentList),
			Online:   online,
			Tunnels:  tunnelCount,
			Sessions: len(sessions),
		})

		sessionResp := make([]SessionResponse, 0, len(sessions))
		for _, s := range sessions {
			sessionResp = append(sessionResp, SessionResponse{
				TunnelID:      s.TunnelID,
				AgentID:       s.AgentID,
				PublicAddr:    s.PublicAddr,
				LocalAddr:     s.LocalAddr,
				CreatedAt:     s.CreatedAt.Unix(),
				LastHeartbeat: s.LastHeartbeat.Unix(),
			})
		}
		send("sessions", sessionResp)
	}

	// Send the initial state immediately so the page renders without waiting.
	push(r.Context())

	statsTicker := time.NewTicker(5 * time.Second)
	pingTicker := time.NewTicker(20 * time.Second)
	defer statsTicker.Stop()
	defer pingTicker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-statsTicker.C:
			push(r.Context())
		case <-pingTicker.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
