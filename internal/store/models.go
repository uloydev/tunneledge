package store

import (
	"time"

	"gorm.io/gorm"
)

type Token struct {
	gorm.Model
	Token   string `gorm:"uniqueIndex;size:256;not null"`
	AgentID string `gorm:"size:128;not null"`
}

type TunnelSession struct {
	gorm.Model
	TunnelID      string `gorm:"index;size:128;not null"`
	AgentID       string `gorm:"size:128;not null"`
	PublicHost    string `gorm:"size:256"`
	LocalAddr     string `gorm:"size:256"`
	RemoteAddr    string `gorm:"size:256"`
	Status        string `gorm:"size:32;index"`
	ConnectedAt   time.Time
	DisconnectedAt *time.Time
	LastHeartbeat time.Time
}
