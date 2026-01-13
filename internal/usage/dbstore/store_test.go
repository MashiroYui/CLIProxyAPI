package dbstore

import (
	"context"
	"os"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestNewStore_SQLite(t *testing.T) {
	tmpFile := t.TempDir() + "/test_usage.db"
	defer os.Remove(tmpFile)

	store, err := New(Config{
		Enable: true,
		Driver: "sqlite",
		DSN:    tmpFile,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	if store == nil {
		t.Fatal("store should not be nil")
	}
	defer store.Close()

	// Verify tables were created
	db := store.DB()
	if db == nil {
		t.Fatal("db should not be nil")
	}

	// Check if tables exist
	var count int64
	db.Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='usage_records'").Scan(&count)
	if count != 1 {
		t.Errorf("usage_records table should exist, got count: %d", count)
	}

	db.Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='usage_daily_stats'").Scan(&count)
	if count != 1 {
		t.Errorf("usage_daily_stats table should exist, got count: %d", count)
	}
}

func TestNewStore_Disabled(t *testing.T) {
	store, err := New(Config{
		Enable: false,
	})
	if err != nil {
		t.Fatalf("should not error when disabled: %v", err)
	}
	if store != nil {
		t.Error("store should be nil when disabled")
	}
}

func TestNewStore_InvalidDriver(t *testing.T) {
	_, err := New(Config{
		Enable: true,
		Driver: "invalid",
		DSN:    "test.db",
	})
	if err == nil {
		t.Error("should error with invalid driver")
	}
}

func TestNewStore_EmptyDSN(t *testing.T) {
	_, err := New(Config{
		Enable: true,
		Driver: "sqlite",
		DSN:    "",
	})
	if err == nil {
		t.Error("should error with empty DSN")
	}
}

func TestStore_HandleUsage(t *testing.T) {
	tmpFile := t.TempDir() + "/test_usage.db"
	defer os.Remove(tmpFile)

	store, err := New(Config{
		Enable: true,
		Driver: "sqlite",
		DSN:    tmpFile,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Handle a usage record
	record := coreusage.Record{
		Provider:    "gemini",
		Model:       "gemini-2.5-pro",
		APIKey:      "test-key",
		AuthID:      "auth-123",
		AuthIndex:   "0",
		Source:      "test",
		RequestedAt: time.Now(),
		Failed:      false,
		Detail: coreusage.Detail{
			InputTokens:  100,
			OutputTokens: 50,
			TotalTokens:  150,
		},
	}
	store.HandleUsage(ctx, record)

	// Verify record was saved
	records, err := store.GetRecentRecords(ctx, 10)
	if err != nil {
		t.Fatalf("failed to get recent records: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 record, got %d", len(records))
	}
	if records[0].Provider != "gemini" {
		t.Errorf("expected provider 'gemini', got %s", records[0].Provider)
	}
	if records[0].Model != "gemini-2.5-pro" {
		t.Errorf("expected model 'gemini-2.5-pro', got %s", records[0].Model)
	}
	if records[0].InputTokens != 100 {
		t.Errorf("expected input tokens 100, got %d", records[0].InputTokens)
	}

	// Verify daily stats were updated
	today := time.Now().Format("2006-01-02")
	stats, err := store.GetDailyStats(ctx, today, today)
	if err != nil {
		t.Fatalf("failed to get daily stats: %v", err)
	}
	if len(stats) != 1 {
		t.Errorf("expected 1 daily stat, got %d", len(stats))
	}
	if stats[0].TotalRequests != 1 {
		t.Errorf("expected total requests 1, got %d", stats[0].TotalRequests)
	}
	if stats[0].SuccessCount != 1 {
		t.Errorf("expected success count 1, got %d", stats[0].SuccessCount)
	}
}

func TestStore_HandleUsage_Failed(t *testing.T) {
	tmpFile := t.TempDir() + "/test_usage.db"
	defer os.Remove(tmpFile)

	store, err := New(Config{
		Enable: true,
		Driver: "sqlite",
		DSN:    tmpFile,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Handle a failed usage record
	record := coreusage.Record{
		Provider:    "claude",
		Model:       "claude-3-opus",
		RequestedAt: time.Now(),
		Failed:      true,
		Detail: coreusage.Detail{
			InputTokens:  50,
			OutputTokens: 0,
			TotalTokens:  50,
		},
	}
	store.HandleUsage(ctx, record)

	// Verify daily stats were updated with failure
	today := time.Now().Format("2006-01-02")
	stats, err := store.GetDailyStats(ctx, today, today)
	if err != nil {
		t.Fatalf("failed to get daily stats: %v", err)
	}
	if len(stats) != 1 {
		t.Errorf("expected 1 daily stat, got %d", len(stats))
	}
	if stats[0].FailureCount != 1 {
		t.Errorf("expected failure count 1, got %d", stats[0].FailureCount)
	}
}

func TestStore_GetAggregatedStats(t *testing.T) {
	tmpFile := t.TempDir() + "/test_usage.db"
	defer os.Remove(tmpFile)

	store, err := New(Config{
		Enable: true,
		Driver: "sqlite",
		DSN:    tmpFile,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add some records
	for i := 0; i < 5; i++ {
		record := coreusage.Record{
			Provider:    "gemini",
			Model:       "gemini-2.5-pro",
			RequestedAt: time.Now(),
			Failed:      i == 0, // First one fails
			Detail: coreusage.Detail{
				InputTokens:  100,
				OutputTokens: 50,
				TotalTokens:  150,
			},
		}
		store.HandleUsage(ctx, record)
	}

	// Get aggregated stats
	stats, err := store.GetAggregatedStats(ctx)
	if err != nil {
		t.Fatalf("failed to get aggregated stats: %v", err)
	}

	if stats["total_requests"].(int64) != 5 {
		t.Errorf("expected total requests 5, got %v", stats["total_requests"])
	}
	if stats["success_count"].(int64) != 4 {
		t.Errorf("expected success count 4, got %v", stats["success_count"])
	}
	if stats["failure_count"].(int64) != 1 {
		t.Errorf("expected failure count 1, got %v", stats["failure_count"])
	}
}

func TestStore_GetRecordsByModel(t *testing.T) {
	tmpFile := t.TempDir() + "/test_usage.db"
	defer os.Remove(tmpFile)

	store, err := New(Config{
		Enable: true,
		Driver: "sqlite",
		DSN:    tmpFile,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add records for different models
	models := []string{"gemini-2.5-pro", "claude-3-opus", "gemini-2.5-pro", "gpt-4"}
	for _, model := range models {
		record := coreusage.Record{
			Provider:    "test",
			Model:       model,
			RequestedAt: time.Now(),
			Detail: coreusage.Detail{
				TotalTokens: 100,
			},
		}
		store.HandleUsage(ctx, record)
	}

	// Get records for gemini-2.5-pro
	records, err := store.GetRecordsByModel(ctx, "gemini-2.5-pro", 10)
	if err != nil {
		t.Fatalf("failed to get records by model: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 records for gemini-2.5-pro, got %d", len(records))
	}
}
