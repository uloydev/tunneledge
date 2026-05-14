package tui

import (
	"fmt"
	"time"
)

// streamItem implements list.DefaultItem so it renders with the default delegate.
type streamItem struct {
	stats StreamStats
}

// Title returns the primary line shown in the stream list.
func (s streamItem) Title() string {
	age := time.Since(s.stats.OpenedAt).Truncate(time.Second)
	return fmt.Sprintf("%-10s  %-14s  %s",
		styleValue.Render(s.stats.StreamID),
		styleMuted.Render(s.stats.Label),
		styleMuted.Render(age.String()),
	)
}

// Description returns the secondary line shown below the title.
func (s streamItem) Description() string {
	return fmt.Sprintf("  → %-22s  ↓ %-8s  ↑ %s",
		s.stats.LocalAddr,
		formatBytes(s.stats.RxBytes),
		formatBytes(s.stats.TxBytes),
	)
}

// FilterValue satisfies list.Item.
func (s streamItem) FilterValue() string { return s.stats.StreamID }

// formatBytes renders a byte count in human-readable form (B / KB / MB / GB).
func formatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
