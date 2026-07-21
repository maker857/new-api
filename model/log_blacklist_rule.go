package model

import "time"

type LogBlacklistRule struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	StatusCode      int       `gorm:"index" json:"status_code"`
	ChannelID       int       `gorm:"index" json:"channel_id"`
	ChannelName     string    `gorm:"size:255" json:"channel_name"`
	ChannelGroup    string    `gorm:"size:255;index" json:"channel_group"`
	GroupScope      string    `gorm:"size:255;index" json:"group_scope"`
	ModelName       string    `gorm:"size:255;index" json:"model_name"`
	ContentContains string    `gorm:"type:text" json:"content_contains"`
	Enabled         bool      `gorm:"index;default:true" json:"enabled"`
	CreatedBy       string    `gorm:"size:64" json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (LogBlacklistRule) TableName() string {
	return "log_blacklist_rules"
}
