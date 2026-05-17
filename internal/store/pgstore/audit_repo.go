package pgstore

import (
	"context"
	"encoding/json"
	"time"

	"tunneledge/internal/domain"

	"gorm.io/gorm"
)

// PGAuditRepository implements domain.AuditRepository backed by Postgres.
type PGAuditRepository struct{ db *gorm.DB }

func NewAuditRepository(db *gorm.DB) *PGAuditRepository {
	return &PGAuditRepository{db: db}
}

func (r *PGAuditRepository) Append(ctx context.Context, event *domain.AuditEvent) error {
	meta := "{}"
	if event.Metadata != nil {
		if b, err := json.Marshal(event.Metadata); err == nil {
			meta = string(b)
		}
	}
	m := &AuditEventModel{
		EventType:  string(event.EventType),
		ActorType:  event.ActorType,
		ActorID:    event.ActorID,
		TargetType: event.TargetType,
		TargetID:   event.TargetID,
		IPAddress:  event.IPAddress,
		UserAgent:  event.UserAgent,
		Metadata:   meta,
		CreatedAt:  event.CreatedAt,
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now()
	}
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *PGAuditRepository) List(ctx context.Context, f domain.AuditFilter) ([]domain.AuditEvent, error) {
	tx := r.db.WithContext(ctx).Model(&AuditEventModel{}).Order("created_at desc")
	if f.ActorID != "" {
		tx = tx.Where("actor_id = ?", f.ActorID)
	}
	if f.EventType != "" {
		tx = tx.Where("event_type = ?", string(f.EventType))
	}
	if !f.Since.IsZero() {
		tx = tx.Where("created_at >= ?", f.Since)
	}
	if !f.Until.IsZero() {
		tx = tx.Where("created_at <= ?", f.Until)
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	tx = tx.Limit(limit).Offset(f.Offset)

	var rows []AuditEventModel
	if err := tx.Find(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]domain.AuditEvent, 0, len(rows))
	for _, row := range rows {
		ev := domain.AuditEvent{
			ID:         row.ID,
			EventType:  domain.AuditEventType(row.EventType),
			ActorType:  row.ActorType,
			ActorID:    row.ActorID,
			TargetType: row.TargetType,
			TargetID:   row.TargetID,
			IPAddress:  row.IPAddress,
			UserAgent:  row.UserAgent,
			CreatedAt:  row.CreatedAt,
		}
		if row.Metadata != "" && row.Metadata != "{}" {
			_ = json.Unmarshal([]byte(row.Metadata), &ev.Metadata)
		}
		out = append(out, ev)
	}
	return out, nil
}

var _ domain.AuditRepository = (*PGAuditRepository)(nil)
