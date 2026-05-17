package dashboard

import (
	"context"
	"time"

	"tunneledge/internal/domain"

	"github.com/rs/zerolog/log"
)

// AuditService logs audit events asynchronously (fire-and-forget) to avoid
// blocking request paths.
type AuditService struct {
	repo domain.AuditRepository
}

func NewAuditService(repo domain.AuditRepository) *AuditService {
	return &AuditService{repo: repo}
}

// Log records an audit event. It is non-blocking; failures are logged but not propagated.
func (s *AuditService) Log(eventType domain.AuditEventType, actorID, targetID, ipAddress string, metadata map[string]any) {
	if s == nil || s.repo == nil {
		return
	}
	event := &domain.AuditEvent{
		EventType: eventType,
		ActorID:   actorID,
		TargetID:  targetID,
		IPAddress: ipAddress,
		Metadata:  metadata,
		CreatedAt: time.Now(),
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.repo.Append(ctx, event); err != nil {
			log.Error().Err(err).Str("event_type", string(eventType)).Msg("failed to write audit event")
		}
	}()
}
