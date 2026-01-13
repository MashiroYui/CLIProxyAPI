// Package dbstore provides database-backed storage for usage statistics.
package dbstore

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

// Config holds the database configuration for usage storage.
type Config struct {
	Enable      bool
	Driver      string
	DSN         string
	TablePrefix string
}

// Store provides database-backed storage for usage statistics.
type Store struct {
	db          *gorm.DB
	tablePrefix string
	mu          sync.Mutex
}

// New creates a new database store with the given configuration.
func New(cfg Config) (*Store, error) {
	if !cfg.Enable {
		return nil, nil
	}

	driver := strings.ToLower(strings.TrimSpace(cfg.Driver))
	dsn := strings.TrimSpace(cfg.DSN)
	if dsn == "" {
		return nil, fmt.Errorf("dbstore: DSN is required")
	}

	var dialector gorm.Dialector
	switch driver {
	case "sqlite", "sqlite3":
		dialector = sqlite.Open(dsn)
	case "postgres", "postgresql", "pg":
		dialector = postgres.Open(dsn)
	default:
		return nil, fmt.Errorf("dbstore: unsupported driver %q (supported: sqlite, postgres)", driver)
	}

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
		NamingStrategy: schema.NamingStrategy{
			TablePrefix: cfg.TablePrefix,
		},
	}

	db, err := gorm.Open(dialector, gormConfig)
	if err != nil {
		return nil, fmt.Errorf("dbstore: failed to open database: %w", err)
	}

	store := &Store{
		db:          db,
		tablePrefix: cfg.TablePrefix,
	}

	if err := store.migrate(); err != nil {
		return nil, fmt.Errorf("dbstore: failed to migrate: %w", err)
	}

	return store, nil
}

// migrate runs database migrations.
func (s *Store) migrate() error {
	return s.db.AutoMigrate(&UsageRecord{}, &DailyStats{})
}

// Close closes the database connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// HandleUsage implements coreusage.Plugin interface.
// It saves usage records to the database.
func (s *Store) HandleUsage(ctx context.Context, record coreusage.Record) {
	if s == nil || s.db == nil {
		return
	}

	timestamp := record.RequestedAt
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	totalTokens := record.Detail.TotalTokens
	if totalTokens == 0 {
		totalTokens = record.Detail.InputTokens + record.Detail.OutputTokens + record.Detail.ReasoningTokens
	}

	dbRecord := UsageRecord{
		Provider:        record.Provider,
		Model:           record.Model,
		APIKey:          record.APIKey,
		AuthID:          record.AuthID,
		AuthIndex:       record.AuthIndex,
		Source:          record.Source,
		RequestedAt:     timestamp,
		Failed:          record.Failed,
		InputTokens:     record.Detail.InputTokens,
		OutputTokens:    record.Detail.OutputTokens,
		ReasoningTokens: record.Detail.ReasoningTokens,
		CachedTokens:    record.Detail.CachedTokens,
		TotalTokens:     totalTokens,
	}

	if err := s.db.WithContext(ctx).Create(&dbRecord).Error; err != nil {
		log.WithError(err).Warn("dbstore: failed to save usage record")
		return
	}

	s.updateDailyStats(ctx, record, timestamp, totalTokens)
}

// updateDailyStats updates the aggregated daily statistics.
func (s *Store) updateDailyStats(ctx context.Context, record coreusage.Record, timestamp time.Time, totalTokens int64) {
	dateStr := timestamp.Format("2006-01-02")
	provider := record.Provider
	if provider == "" {
		provider = "unknown"
	}
	model := record.Model
	if model == "" {
		model = "unknown"
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var stats DailyStats
	result := s.db.WithContext(ctx).Where(
		"date = ? AND provider = ? AND model = ?",
		dateStr, provider, model,
	).First(&stats)

	successInc := int64(0)
	failureInc := int64(0)
	if record.Failed {
		failureInc = 1
	} else {
		successInc = 1
	}

	if result.Error == gorm.ErrRecordNotFound {
		stats = DailyStats{
			Date:          dateStr,
			Provider:      provider,
			Model:         model,
			TotalRequests: 1,
			SuccessCount:  successInc,
			FailureCount:  failureInc,
			TotalTokens:   totalTokens,
			InputTokens:   record.Detail.InputTokens,
			OutputTokens:  record.Detail.OutputTokens,
		}
		if err := s.db.WithContext(ctx).Create(&stats).Error; err != nil {
			log.WithError(err).Warn("dbstore: failed to create daily stats")
		}
		return
	}

	if result.Error != nil {
		log.WithError(result.Error).Warn("dbstore: failed to query daily stats")
		return
	}

	updates := map[string]interface{}{
		"total_requests": gorm.Expr("total_requests + ?", 1),
		"success_count":  gorm.Expr("success_count + ?", successInc),
		"failure_count":  gorm.Expr("failure_count + ?", failureInc),
		"total_tokens":   gorm.Expr("total_tokens + ?", totalTokens),
		"input_tokens":   gorm.Expr("input_tokens + ?", record.Detail.InputTokens),
		"output_tokens":  gorm.Expr("output_tokens + ?", record.Detail.OutputTokens),
	}

	if err := s.db.WithContext(ctx).Model(&stats).Updates(updates).Error; err != nil {
		log.WithError(err).Warn("dbstore: failed to update daily stats")
	}
}

// GetDailyStats retrieves daily statistics for a given date range.
func (s *Store) GetDailyStats(ctx context.Context, startDate, endDate string) ([]DailyStats, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("dbstore: not initialized")
	}

	var stats []DailyStats
	query := s.db.WithContext(ctx)

	if startDate != "" && endDate != "" {
		query = query.Where("date >= ? AND date <= ?", startDate, endDate)
	} else if startDate != "" {
		query = query.Where("date >= ?", startDate)
	} else if endDate != "" {
		query = query.Where("date <= ?", endDate)
	}

	if err := query.Order("date DESC").Find(&stats).Error; err != nil {
		return nil, fmt.Errorf("dbstore: failed to get daily stats: %w", err)
	}

	return stats, nil
}

// GetRecentRecords retrieves the most recent usage records.
func (s *Store) GetRecentRecords(ctx context.Context, limit int) ([]UsageRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("dbstore: not initialized")
	}

	if limit <= 0 {
		limit = 100
	}

	var records []UsageRecord
	if err := s.db.WithContext(ctx).Order("requested_at DESC").Limit(limit).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("dbstore: failed to get recent records: %w", err)
	}

	return records, nil
}

// GetRecordsByModel retrieves usage records for a specific model.
func (s *Store) GetRecordsByModel(ctx context.Context, model string, limit int) ([]UsageRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("dbstore: not initialized")
	}

	if limit <= 0 {
		limit = 100
	}

	var records []UsageRecord
	if err := s.db.WithContext(ctx).Where("model = ?", model).Order("requested_at DESC").Limit(limit).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("dbstore: failed to get records by model: %w", err)
	}

	return records, nil
}

// GetAggregatedStats returns aggregated token statistics.
func (s *Store) GetAggregatedStats(ctx context.Context) (map[string]interface{}, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("dbstore: not initialized")
	}

	var result struct {
		TotalRequests int64
		SuccessCount  int64
		FailureCount  int64
		TotalTokens   int64
		InputTokens   int64
		OutputTokens  int64
	}

	if err := s.db.WithContext(ctx).Model(&UsageRecord{}).Select(
		"COUNT(*) as total_requests",
		"SUM(CASE WHEN failed = false THEN 1 ELSE 0 END) as success_count",
		"SUM(CASE WHEN failed = true THEN 1 ELSE 0 END) as failure_count",
		"COALESCE(SUM(total_tokens), 0) as total_tokens",
		"COALESCE(SUM(input_tokens), 0) as input_tokens",
		"COALESCE(SUM(output_tokens), 0) as output_tokens",
	).Scan(&result).Error; err != nil {
		return nil, fmt.Errorf("dbstore: failed to get aggregated stats: %w", err)
	}

	return map[string]interface{}{
		"total_requests": result.TotalRequests,
		"success_count":  result.SuccessCount,
		"failure_count":  result.FailureCount,
		"total_tokens":   result.TotalTokens,
		"input_tokens":   result.InputTokens,
		"output_tokens":  result.OutputTokens,
	}, nil
}

// DB returns the underlying GORM database instance.
func (s *Store) DB() *gorm.DB {
	if s == nil {
		return nil
	}
	return s.db
}
