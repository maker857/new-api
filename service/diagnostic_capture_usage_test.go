package service

import (
	"context"

	"os"
	"path/filepath"

	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureUsageFixture(t *testing.T) DiagnosticCaptureConfig {
	t.Helper()
	diagnosticCaptureStorageState.Lock()
	previous := diagnosticCaptureStorageState.diagnosticCaptureUsageCounters
	diagnosticCaptureStorageState.diagnosticCaptureUsageCounters = diagnosticCaptureUsageCounters{}
	diagnosticCaptureStorageState.Unlock()
	requested := diagnosticCaptureScanRequested.Swap(false)
	running := diagnosticCaptureScanRunning.Swap(false)
	stopping := diagnosticCaptureUsageStopping.Swap(false)
	retryAfter := diagnosticCaptureScanRetryAfter.Swap(0)
	t.Cleanup(func() {
		diagnosticCaptureStorageState.Lock()
		diagnosticCaptureStorageState.diagnosticCaptureUsageCounters = previous
		diagnosticCaptureStorageState.Unlock()
		diagnosticCaptureScanRequested.Store(requested)
		diagnosticCaptureScanRunning.Store(running)
		diagnosticCaptureUsageStopping.Store(stopping)
		diagnosticCaptureScanRetryAfter.Store(retryAfter)
	})
	root := t.TempDir()
	cfg := DiagnosticCaptureConfig{Enabled: true, Mode: "full", CaptureDir: filepath.Join(root, "captures"), TempDir: filepath.Join(root, "temp"), FailureDir: filepath.Join(root, "failures")}
	configureDiagnosticCaptureStorage(cfg)
	return cfg
}

func TestDiagnosticUsageRestoreKeepsWritesAndDeletesDuringFileRead(t *testing.T) {
	cfg := captureUsageFixture(t)
	// These locks stand for an ongoing slow scan and checkpoint. Accounting
	// must complete before they are released, including before initialization.
	diagnosticCaptureCleanupMu.Lock()
	defer diagnosticCaptureCleanupMu.Unlock()
	diagnosticCaptureUsageMu.Lock()
	defer diagnosticCaptureUsageMu.Unlock()
	beginDiagnosticCaptureFileChange(cfg, filepath.Join(cfg.TempDir, "added.part"), 0).finish(30)
	beginDiagnosticCaptureFileChange(cfg, filepath.Join(cfg.TempDir, "removed.part"), 50).finish(0)
	usage := &diagnosticCaptureUsage{Version: 1, TotalBytes: 1000, TemporaryBytes: 100, CleanShutdown: true}
	restoreDiagnosticCaptureUsage(cfg, "scope", usage)
	assert.Equal(t, int64(980), diagnosticCaptureStorageState.totalBytes)
	assert.Equal(t, int64(80), diagnosticCaptureStorageState.tempBytes)
	assert.True(t, diagnosticCaptureStorageState.initialized)
	assert.False(t, diagnosticCaptureScanRequested.Load(), "clean restart must not schedule a directory scan")
}

func TestDiagnosticUsageCheckpointSurvivesCleanAndInterruptedRestart(t *testing.T) {
	cfg := captureUsageFixture(t)
	scope, err := diagnosticCaptureStorageScope(cfg)
	require.NoError(t, err)
	restoreDiagnosticCaptureUsage(cfg, scope, &diagnosticCaptureUsage{Version: 1, TotalBytes: 1000, TemporaryBytes: 10, CleanShutdown: true})
	beginDiagnosticCaptureFileChange(cfg, filepath.Join(cfg.CaptureDir, "new.json"), 0).finish(250)
	require.NoError(t, checkpointDiagnosticCaptureUsage(context.Background(), true))
	for _, clean := range []bool{true, false} {
		saved, err := loadDiagnosticCaptureUsage(filepath.Join(filepath.Dir(cfg.CaptureDir), diagnosticCaptureUsageFilename), scope)
		require.NoError(t, err)
		require.NotNil(t, saved)
		assert.Equal(t, int64(1250), saved.TotalBytes)
		storageID := saved.StorageID
		saved.CleanShutdown = clean
		diagnosticCaptureStorageState.Lock()
		diagnosticCaptureStorageState.diagnosticCaptureUsageCounters = diagnosticCaptureUsageCounters{}
		diagnosticCaptureStorageState.Unlock()
		configureDiagnosticCaptureStorage(cfg)
		diagnosticCaptureScanRequested.Store(false)
		restoreDiagnosticCaptureUsage(cfg, scope, saved)
		assert.Equal(t, storageID, diagnosticCaptureStorageState.storageID, "restart retains the identity stored in the same file")
		assert.Equal(t, int64(1250), diagnosticCaptureStorageState.totalBytes)
		assert.False(t, diagnosticCaptureScanRequested.Load(), "valid checkpoints must not trigger a startup scan even after a crash")
		assert.Equal(t, !clean, diagnosticCaptureStorageState.estimated)
	}
}

func TestDiagnosticUsageScanRejectsOldDirectory(t *testing.T) {
	cfg := captureUsageFixture(t)
	scan := beginDiagnosticCaptureUsageScan(cfg)
	other := cfg
	other.CaptureDir += "-new"
	configureDiagnosticCaptureStorage(other)
	beginDiagnosticCaptureFileChange(cfg, filepath.Join(cfg.CaptureDir, "obsolete.json"), 0).finish(999)
	assert.Zero(t, diagnosticCaptureStorageState.totalBytes)
	configureDiagnosticCaptureStorage(cfg)
	assert.False(t, completeDiagnosticCaptureUsageScan(scan), "returning to the same path must not accept an obsolete scan")
	assert.False(t, diagnosticCaptureStorageState.initialized)
}

func TestDiagnosticUsageStorageScopeSeparatesNodesAndDirectories(t *testing.T) {
	cfg := captureUsageFixture(t)
	previous := common.NodeName
	common.NodeName = "node-one"
	t.Cleanup(func() { common.NodeName = previous })
	first, err := diagnosticCaptureStorageScope(cfg)
	require.NoError(t, err)
	again, err := diagnosticCaptureStorageScope(cfg)
	require.NoError(t, err)
	assert.Equal(t, first, again)
	common.NodeName = "node-two"
	other, err := diagnosticCaptureStorageScope(cfg)
	require.NoError(t, err)
	assert.NotEqual(t, first, other)
	common.NodeName = "node-one"
	cfg.CaptureDir = filepath.Join(t.TempDir(), "captures")
	replaced, err := diagnosticCaptureStorageScope(cfg)
	require.NoError(t, err)
	assert.NotEqual(t, first, replaced)
}

func TestDiagnosticCaptureWritesNewFileWhileMaintenanceIsBusy(t *testing.T) {
	cfg := captureUsageFixture(t)
	release := claimDiagnosticCaptureMaintenance("old-record-being-cleaned")
	require.NotNil(t, release)
	defer release()
	diagnosticCaptureCleanupMu.Lock()
	defer diagnosticCaptureCleanupMu.Unlock()
	diagnosticCaptureUsageMu.Lock()
	defer diagnosticCaptureUsageMu.Unlock()
	flow := &DiagnosticFlow{TraceID: "nonblocking-write", Channel: "channel", Started: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)}
	s := &diagnosticCaptureSession{cfg: cfg, events: make(chan diagnosticCaptureEvent, 4), pendingWake: make(chan struct{}, 1), spoolDir: filepath.Join(cfg.TempDir, "request")}
	go s.dispatch()
	done := make(chan struct{})
	go func() { s.run(); close(done) }()
	s.startPart("inbound-request", 1, "inbound", "request", nil)
	body := []byte(`{"message":"saved while scanning"}`)
	s.writeChunk("inbound-request", body)
	s.endPart("inbound-request", nil, int64(len(body)), true)
	s.close(flow)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("capture waited for maintenance/checkpoint lock")
	}
	data, err := os.ReadFile(filepath.Join(cfg.CaptureDir, "channel", "2026-10-10", flow.TraceID, "request-log.json"))
	require.NoError(t, err)
	var combined diagnosticCombinedCPAJSON
	require.NoError(t, common.Unmarshal(data, &combined))
	require.NotNil(t, combined.Request)
	assert.Equal(t, map[string]any{"message": "saved while scanning"}, combined.Request.Body.JSON)
	assert.False(t, combined.Request.Body.Truncated)
	assert.False(t, s.failed.Load())

}

func TestDiagnosticUsageBusyMaintenanceDefersSingleScan(t *testing.T) {
	cfg := captureUsageFixture(t)
	diagnosticCaptureCleanupMu.Lock()
	defer diagnosticCaptureCleanupMu.Unlock()
	diagnosticCaptureScanRequested.Store(true)
	startDiagnosticCaptureUsageScan(cfg)
	assert.True(t, diagnosticCaptureScanRequested.Load())
	assert.False(t, diagnosticCaptureScanRunning.Load())
	diagnosticCaptureScanRunning.Store(true)
	startDiagnosticCaptureUsageScan(cfg)
	assert.True(t, diagnosticCaptureScanRunning.Load())
}

func TestDiagnosticUsageBootstrapCountsAllRootsAndKeepsBaselineOnScanError(t *testing.T) {
	cfg := captureUsageFixture(t)
	for _, root := range []string{cfg.CaptureDir, cfg.TempDir, cfg.FailureDir} {
		require.NoError(t, os.MkdirAll(root, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(root, "fragment"), []byte("12345"), 0o600))
	}
	scanDiagnosticCaptureUsage(cfg)
	assert.True(t, diagnosticCaptureStorageState.initialized)
	assert.Equal(t, int64(15), diagnosticCaptureStorageState.totalBytes)
	assert.Equal(t, int64(5), diagnosticCaptureStorageState.tempBytes)
	// A NUL path fails rather than being treated as a not-yet-created root.
	cfg.CaptureDir += "\x00"
	configureDiagnosticCaptureStorage(cfg)
	restoreDiagnosticCaptureUsage(cfg, "scope", &diagnosticCaptureUsage{Version: 1, TotalBytes: 123, TemporaryBytes: 5, CleanShutdown: true})
	scanDiagnosticCaptureUsage(cfg)
	assert.Equal(t, int64(123), diagnosticCaptureStorageState.totalBytes)
	assert.True(t, diagnosticCaptureScanRequested.Load())
}

func TestDiagnosticUsageShutdownSavesAndReleasesMaintenanceGate(t *testing.T) {
	for _, maintenanceRunning := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "maintenance-paused-between-batches"}[maintenanceRunning], func(t *testing.T) {
			cfg := captureUsageFixture(t)
			scope, err := diagnosticCaptureStorageScope(cfg)
			require.NoError(t, err)
			restoreDiagnosticCaptureUsage(cfg, scope, &diagnosticCaptureUsage{TotalBytes: 5, CleanShutdown: true})
			previous := diagnosticCaptureCleanupRunning.Swap(maintenanceRunning)
			t.Cleanup(func() { diagnosticCaptureCleanupRunning.Store(previous) })
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			require.NoError(t, StopDiagnosticCaptureUsage(ctx))
			require.True(t, diagnosticCaptureCleanupMu.TryLock(), "shutdown must not leak the maintenance gate")
			diagnosticCaptureCleanupMu.Unlock()
			usage, err := loadDiagnosticCaptureUsage(diagnosticCaptureStorageState.usagePath, scope)
			require.NoError(t, err)
			require.NotNil(t, usage)
			assert.Equal(t, !maintenanceRunning, usage.CleanShutdown)
		})
	}
}
