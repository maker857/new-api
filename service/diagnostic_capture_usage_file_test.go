package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticUsageFileReplacementKeepsCompleteCheckpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), diagnosticCaptureUsageFilename)
	usage := diagnosticCaptureUsage{Version: 2, StorageID: strings.Repeat("b", 32), Scope: strings.Repeat("a", 64), TotalBytes: 1234, TemporaryBytes: 50, SavedAt: 1700000000, CleanShutdown: true}
	missing, err := loadDiagnosticCaptureUsage(path, usage.Scope)
	require.NoError(t, err)
	assert.Nil(t, missing)
	require.NoError(t, saveDiagnosticCaptureUsage(context.Background(), path, usage))
	usage.TotalBytes = 5678
	usage.CleanShutdown = false
	require.NoError(t, saveDiagnosticCaptureUsage(context.Background(), path, usage))
	loaded, err := loadDiagnosticCaptureUsage(path, usage.Scope)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, usage, *loaded)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	var documented struct {
		Notes map[string]string `json:"_说明"`
	}
	require.NoError(t, common.Unmarshal(before, &documented))
	assert.Contains(t, documented.Notes["temporary_bytes"], "已包含在 total_bytes 内")
	assert.Contains(t, documented.Notes["storage_id"], "正常重启保持不变")
	assert.Contains(t, string(before), "\n  \"", "saved JSON should be indented for reading")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, saveDiagnosticCaptureUsage(cancelled, path, usage), context.Canceled)
	invalid := usage
	invalid.TotalBytes = -1
	assert.Error(t, saveDiagnosticCaptureUsage(context.Background(), path, invalid))
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after, "failed saves must retain the prior checkpoint")
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "successful/cancelled saves must not accumulate temporary files")
}

func TestDiagnosticUsageFailedCheckpointKeepsLastSuccessAndLiveAccounting(t *testing.T) {
	cfg := captureUsageFixture(t)
	scope, err := diagnosticCaptureStorageScope(cfg)
	require.NoError(t, err)
	restoreDiagnosticCaptureUsage(cfg, scope, &diagnosticCaptureUsage{TotalBytes: 5, CleanShutdown: true})
	require.NoError(t, checkpointDiagnosticCaptureUsage(context.Background(), false))
	savedAt := diagnosticCaptureStorageState.lastSaved
	goodPath := diagnosticCaptureStorageState.usagePath
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blocked, []byte("file"), 0o600))
	diagnosticCaptureStorageState.usagePath = filepath.Join(blocked, diagnosticCaptureUsageFilename)
	require.Error(t, checkpointDiagnosticCaptureUsage(context.Background(), false))
	assert.True(t, diagnosticCaptureStorageState.lastSaveError)
	assert.Equal(t, savedAt, diagnosticCaptureStorageState.lastSaved)
	beginDiagnosticCaptureFileChange(cfg, filepath.Join(cfg.CaptureDir, "still-writing.json"), 0).finish(10)
	assert.Equal(t, int64(15), diagnosticCaptureStorageState.totalBytes)
	diagnosticCaptureStorageState.usagePath = goodPath
	require.NoError(t, checkpointDiagnosticCaptureUsage(context.Background(), false))
	assert.False(t, diagnosticCaptureStorageState.lastSaveError)
	saved, err := loadDiagnosticCaptureUsage(goodPath, scope)
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Equal(t, int64(15), saved.TotalBytes)
}

func TestDiagnosticUsageFileRejectsCorruptionAndWrongScope(t *testing.T) {
	valid := diagnosticCaptureUsage{Version: 2, StorageID: strings.Repeat("b", 32), Scope: strings.Repeat("a", 64), TotalBytes: 10, SavedAt: 1700000000}
	validJSON, err := common.Marshal(valid)
	require.NoError(t, err)
	negative := valid
	negative.TotalBytes = -1
	invalidJSON, err := common.Marshal(negative)
	require.NoError(t, err)
	missingID := valid
	missingID.StorageID = ""
	missingIDJSON, err := common.Marshal(missingID)
	require.NoError(t, err)
	for _, tc := range []struct{ name, data, scope string }{
		{"truncated", `{"version":1`, valid.Scope},
		{"negative", string(invalidJSON), valid.Scope},
		{"missing_identity", string(missingIDJSON), valid.Scope},
		{"wrong_storage", string(validJSON), strings.Repeat("b", 64)},
		{"oversized", strings.Repeat(" ", 64*1024+1), valid.Scope},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), diagnosticCaptureUsageFilename)
			require.NoError(t, os.WriteFile(path, []byte(tc.data), 0o600))
			usage, err := loadDiagnosticCaptureUsage(path, tc.scope)
			assert.Error(t, err)
			assert.Nil(t, usage)
		})
	}
}

func TestDiagnosticUsageFileIgnoresInterruptedTemporarySave(t *testing.T) {
	path := filepath.Join(t.TempDir(), diagnosticCaptureUsageFilename)
	usage := diagnosticCaptureUsage{Version: 2, StorageID: strings.Repeat("b", 32), Scope: strings.Repeat("a", 64), TotalBytes: 200, SavedAt: 1700000000}
	require.NoError(t, saveDiagnosticCaptureUsage(context.Background(), path, usage))
	// Simulate process termination after creating the next temp file, before rename.
	require.NoError(t, os.WriteFile(path+".tmp-interrupted", []byte(`{"version":`), 0o600))
	loaded, err := loadDiagnosticCaptureUsage(path, usage.Scope)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, usage, *loaded)
}

func TestDiagnosticUsageFileRemainsOutsideCaptureAccounting(t *testing.T) {
	cfg := captureUsageFixture(t)
	require.NoError(t, os.MkdirAll(cfg.CaptureDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(cfg.CaptureDir, "request-log.json"), []byte("12345"), 0o600))
	scope, err := diagnosticCaptureStorageScope(cfg)
	require.NoError(t, err)
	restoreDiagnosticCaptureUsage(cfg, scope, nil)
	scanDiagnosticCaptureUsage(cfg)
	require.NoError(t, checkpointDiagnosticCaptureUsage(context.Background(), false))
	path := filepath.Join(filepath.Dir(cfg.CaptureDir), diagnosticCaptureUsageFilename)
	require.NoError(t, os.WriteFile(path+".tmp-interrupted", []byte("partial checkpoint"), 0o600))
	scanDiagnosticCaptureUsage(cfg)
	assert.Equal(t, int64(5), diagnosticCaptureStorageState.totalBytes)
	loaded, err := loadDiagnosticCaptureUsage(path, scope)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, int64(5), loaded.TotalBytes)
}

func TestDiagnosticUsageCheckpointsFollowConfiguredStorageRoot(t *testing.T) {
	cfg := captureUsageFixture(t)
	parent := t.TempDir()
	for index, directory := range []string{"custom-logs", "another-tenant"} {
		cfg.CaptureDir = filepath.Join(parent, directory, "captures")
		cfg.TempDir = filepath.Join(parent, directory, "temp")
		cfg.FailureDir = filepath.Join(parent, directory, "failures")
		configureDiagnosticCaptureStorage(cfg)
		scope, err := diagnosticCaptureStorageScope(cfg)
		require.NoError(t, err)
		total := int64(index+1) * 100
		restoreDiagnosticCaptureUsage(cfg, scope, &diagnosticCaptureUsage{TotalBytes: total, CleanShutdown: true})
		require.NoError(t, checkpointDiagnosticCaptureUsage(context.Background(), true))
	}
	// Each configured storage root retains its own checkpoint beside captures.
	for index, directory := range []string{"custom-logs", "another-tenant"} {
		cfg.CaptureDir = filepath.Join(parent, directory, "captures")
		cfg.TempDir = filepath.Join(parent, directory, "temp")
		cfg.FailureDir = filepath.Join(parent, directory, "failures")
		scope, err := diagnosticCaptureStorageScope(cfg)
		require.NoError(t, err)
		loaded, err := loadDiagnosticCaptureUsage(filepath.Join(filepath.Dir(cfg.CaptureDir), diagnosticCaptureUsageFilename), scope)
		require.NoError(t, err)
		require.NotNil(t, loaded)
		assert.Equal(t, int64(index+1)*100, loaded.TotalBytes)
		assert.NoFileExists(t, filepath.Join(cfg.CaptureDir, diagnosticCaptureUsageFilename))
		entries, err := os.ReadDir(filepath.Dir(cfg.CaptureDir))
		require.NoError(t, err)
		assert.Len(t, entries, 1, "checkpoint and storage identity must use a single file")
	}
	assert.NoFileExists(t, filepath.Join(parent, diagnosticCaptureUsageFilename))
	assert.NoFileExists(t, filepath.Join(parent, ".diagnostic-capture-storage-id"))
}
