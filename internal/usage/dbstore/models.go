// Package dbstore provides database-backed storage for usage statistics.
package dbstore

import (
	"time"
)

// UsageRecord represents a single usage record stored in the database.
type UsageRecord struct {
	ID              uint      `gorm:"primaryKey;autoIncrement"`
	Provider        string    `gorm:"size:64;index"`
	Model           string    `gorm:"size:128;index"`
	APIKey          string    `gorm:"size:64;index"`
	AuthID          string    `gorm:"size:128"`
	AuthIndex       string    `gorm:"size:64"`
	Source          string    `gorm:"size:64"`
	RequestedAt     time.Time `gorm:"index"`
	Failed          bool      `gorm:"index"`
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	CachedTokens    int64
	TotalTokens     int64
	CreatedAt       time.Time `gorm:"autoCreateTime"`
}

// TableName returns the table name for UsageRecord.
// This method allows customization via table prefix.
func (UsageRecord) TableName() string {
	return "usage_records"
}

// DailyStats represents aggregated daily statistics.
type DailyStats struct {
	ID            uint   `gorm:"primaryKey;autoIncrement"`
	Date          string `gorm:"size:10;uniqueIndex:idx_daily_stats_unique"`
	Provider      string `gorm:"size:64;uniqueIndex:idx_daily_stats_unique"`
	Model         string `gorm:"size:128;uniqueIndex:idx_daily_stats_unique"`
	TotalRequests int64
	SuccessCount  int64
	FailureCount  int64
	TotalTokens   int64
	InputTokens   int64
	OutputTokens  int64
	UpdatedAt     time.Time `gorm:"autoUpdateTime"`
}

// TableName returns the table name for DailyStats.
func (DailyStats) TableName() string {
	return "usage_daily_stats"
}
