package dashboard

import (
	"encoding/json"
	"net/http"
	"time"

	"tunneledge/internal/domain"
)

// AuditHandler serves audit log query requests.
type AuditHandler struct {
	repo domain.AuditRepository
}

func NewAuditHandler(repo domain.AuditRepository) *AuditHandler {
	return &AuditHandler{repo: repo}
}

// List handles GET /api/v1/audit with optional query params:
//
//	actor_id, event_type, since (RFC3339), until (RFC3339), limit (default 100)
func (h *AuditHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := domain.AuditFilter{
		ActorID:   q.Get("actor_id"),
		EventType: domain.AuditEventType(q.Get("event_type")),
		Limit:     100,
	}

	if s := q.Get("since"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since parameter")
			return
		}
		filter.Since = t
	}
	if u := q.Get("until"); u != "" {
		t, err := time.Parse(time.RFC3339, u)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid until parameter")
			return
		}
		filter.Until = t
	}

	events, err := h.repo.List(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query audit log")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(events)
}
