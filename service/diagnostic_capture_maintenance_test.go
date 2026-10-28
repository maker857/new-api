package service

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func diagnosticCaptureCleanupFixture(t *testing.T) DiagnosticCaptureConfig {
	t.Helper()
	cfg := captureUsageFixture(t)
	// Optional DSNs must point to isolated test databases, never application data.
	var driver gorm.Dialector = sqlite.Open(":memory:")
	if dsn := os.Getenv("TEST_DIAGNOSTIC_MYSQL_DSN"); dsn != "" {
		driver = mysql.Open(dsn)
	} else if dsn := os.Getenv("TEST_DIAGNOSTIC_POSTGRES_DSN"); dsn != "" {
		driver = postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	previousDB := model.DB
	model.DB = db
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = DefaultDiagnosticCaptureOptions()
	common.OptionMap[DiagnosticCaptureDirKey] = cfg.CaptureDir
	common.OptionMap[DiagnosticCaptureAutoCleanupEnabledKey] = "true"
	common.OptionMap[DiagnosticCaptureMaxStorageBytesKey] = "200"
	common.OptionMap[DiagnosticCaptureCleanupPercentKey] = "50"
	common.OptionMap[DiagnosticCaptureMinRetentionMinutesKey] = "900"
	common.OptionMap[DiagnosticCaptureIncompleteTimeoutMinutesKey] = "0"
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		model.DB = previousDB
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
		require.NoError(t, sqlDB.Close())
	})
	cfg = DiagnosticCaptureConfigFromOptions()
	configureDiagnosticCaptureStorage(cfg)
	return cfg
}

func TestDiagnosticCleanupRetriesUsingCaptureTimestamps(t *testing.T) {
	for _, test := range []struct {
		name    string
		elapsed time.Duration
		restart bool
	}{
		{name: "just-expired", elapsed: 10*time.Minute + time.Second},
		{name: "five-hours-later", elapsed: 5 * time.Hour},
		{name: "restart-with-legacy-wait", elapsed: 5 * time.Hour, restart: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := diagnosticCaptureCleanupFixture(t)
			now := time.Now()
			firstAttempt := now.Add(-test.elapsed)
			createdA := firstAttempt.Add(-14*time.Hour - 50*time.Minute)
			paths := make(map[string]string)
			for _, capture := range []struct {
				name    string
				created time.Time
			}{
				{name: "old", created: firstAttempt.Add(-16 * time.Hour)},
				{name: "A", created: createdA},
				{name: "recent", created: firstAttempt.Add(-30 * time.Minute)},
			} {
				dir := filepath.Join(cfg.CaptureDir, "channel", capture.created.Format("2006-01-02"), capture.name)
				require.NoError(t, os.MkdirAll(dir, 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".capture-created-at"), []byte(strconv.FormatInt(capture.created.UnixNano(), 10)), 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".capture-complete"), []byte("complete"), 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "request-log.json"), make([]byte, 64), 0o600))
				paths[capture.name] = dir
			}
			// Establish usage without launching the asynchronous cleanup yet.
			scanCfg := cfg
			scanCfg.AutoCleanupEnabled = false
			scanDiagnosticCaptureUsage(scanCfg)
			cleanupDiagnosticCaptureStorage(cfg, "", diagnosticCaptureStorageState.totalBytes, firstAttempt)
			assert.NoDirExists(t, paths["old"])
			require.DirExists(t, paths["A"], "A is still ten minutes short of its retention period")
			common.OptionMapRWMutex.RLock()
			status := common.OptionMap[DiagnosticCaptureLastCleanupStatusKey]
			common.OptionMapRWMutex.RUnlock()
			assert.Equal(t, "retention_limited", status)
			require.Less(t, diagnosticCaptureStorageState.totalBytes, cfg.MaxStorageBytes)
			require.Greater(t, diagnosticCaptureStorageState.totalBytes, cfg.MaxStorageBytes/2)

			// New writes cross the trigger again before another 15 hours pass.
			newFile := filepath.Join(paths["recent"], "new.part")
			change := beginDiagnosticCaptureFileChange(cfg, newFile, 0)
			require.NoError(t, os.WriteFile(newFile, make([]byte, 64), 0o600))
			change.finish(64)
			require.GreaterOrEqual(t, diagnosticCaptureStorageState.totalBytes, cfg.MaxStorageBytes)
			if test.restart {
				require.NoError(t, model.UpdateOptionsBulk(map[string]string{
					DiagnosticCaptureNextCleanupEligibleAtKey: strconv.FormatInt(firstAttempt.Add(15*time.Hour).Unix(), 10),
				}))
				cfg = DiagnosticCaptureConfigFromOptions()
				diagnosticCaptureStorageState.Lock()
				diagnosticCaptureStorageState.diagnosticCaptureUsageCounters = diagnosticCaptureUsageCounters{}
				diagnosticCaptureStorageState.Unlock()
				configureDiagnosticCaptureStorage(cfg)
				scanDiagnosticCaptureUsage(scanCfg)
				status, err := GetDiagnosticCaptureStorageStatus()
				require.NoError(t, err)
				assert.Zero(t, status.NextCleanupEligibleAt, "obsolete waits must not be reported after an upgrade")
			}
			diagnosticCaptureStorageState.Lock()
			diagnosticCaptureStorageState.lastAttempt = firstAttempt
			diagnosticCaptureStorageState.Unlock()
			cleanupDiagnosticCaptureStorageIfNeeded(cfg)
			require.Eventually(t, func() bool {
				return !diagnosticCaptureCleanupRunning.Load() &&
					diagnosticCaptureOptionInt64(DiagnosticCaptureLastCleanupCutoffKey) == createdA.Unix()
			}, 5*time.Second, 10*time.Millisecond, "the next capacity trigger must delete the now-expired A without a full retention-period wait")
			assert.NoDirExists(t, paths["A"])
			assert.FileExists(t, newFile, "logs still inside their retention period remain protected")
			assert.Zero(t, diagnosticCaptureOptionInt64(DiagnosticCaptureNextCleanupEligibleAtKey))
			var persistedWait model.Option
			require.NoError(t, model.DB.First(&persistedWait, &model.Option{Key: DiagnosticCaptureNextCleanupEligibleAtKey}).Error)
			assert.Equal(t, "0", persistedWait.Value, "the obsolete wait must also be cleared in persistent storage")
		})
	}
}

func TestDiagnosticCleanupPreservesRetentionBoundary(t *testing.T) {
	for _, test := range []struct {
		name               string
		age                time.Duration
		complete           bool
		active             bool
		incompleteDisabled bool
		removed            bool
	}{
		{name: "completed-within-retention", age: 15*time.Hour - time.Nanosecond, complete: true},
		{name: "completed-at-retention-boundary", age: 15 * time.Hour, complete: true},
		{name: "completed-expired", age: 15*time.Hour + time.Nanosecond, complete: true, removed: true},
		{name: "active-completed-expired", age: 16 * time.Hour, complete: true, active: true},
		{name: "incomplete-within-timeout", age: time.Hour - time.Nanosecond},
		{name: "incomplete-at-timeout-boundary", age: time.Hour},
		{name: "incomplete-expired", age: time.Hour + time.Nanosecond, removed: true},
		{name: "incomplete-cleanup-disabled", age: 16 * time.Hour, incompleteDisabled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := diagnosticCaptureCleanupFixture(t)
			cfg.MaxStorageBytes = 1 // Capacity pressure must not bypass retention.
			if !test.incompleteDisabled {
				cfg.IncompleteTimeoutMinutes = 60
			}
			now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
			created := now.Add(-test.age)
			dir := filepath.Join(cfg.CaptureDir, "channel", created.Format("2006-01-02"), "request")
			require.NoError(t, os.MkdirAll(dir, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".capture-created-at"), []byte(strconv.FormatInt(created.UnixNano(), 10)), 0o600))
			body := []byte(`{"message":"protected diagnostic log"}`)
			logPath := filepath.Join(dir, "request-log.json")
			require.NoError(t, os.WriteFile(logPath, body, 0o600))
			if test.complete {
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".capture-complete"), []byte("complete"), 0o600))
			}
			if test.active {
				markDiagnosticCaptureActive("request")
				t.Cleanup(func() { markDiagnosticCaptureInactive("request") })
			}
			scanCfg := cfg
			scanCfg.AutoCleanupEnabled = false
			scanDiagnosticCaptureUsage(scanCfg)
			before := diagnosticCaptureStorageState.totalBytes
			require.Greater(t, before, cfg.MaxStorageBytes)
			for range 2 {
				cleanupDiagnosticCaptureStorage(cfg, "", diagnosticCaptureStorageState.totalBytes, now)
			}
			if test.removed {
				assert.NoDirExists(t, dir)
				assert.Zero(t, diagnosticCaptureStorageState.totalBytes)
				return
			}

			// Even a stale candidate selected as expired/incomplete must be
			// checked against the actual markers again before deleting files.
			var incompleteCutoff time.Time
			if cfg.IncompleteTimeoutMinutes > 0 {
				incompleteCutoff = now.Add(-time.Duration(cfg.IncompleteTimeoutMinutes) * time.Minute)
			}
			candidate := diagnosticCaptureCandidate{dir: dir, traceID: "request", capturedAt: now.Add(-48 * time.Hour)}
			freed, removed, err := removeEligibleDiagnosticCapture(cfg, candidate, now.Add(-15*time.Hour), incompleteCutoff, nil)
			require.NoError(t, err)
			assert.Zero(t, freed)
			assert.False(t, removed)
			retained, err := os.ReadFile(logPath)
			require.NoError(t, err)
			assert.Equal(t, body, retained)
			assert.Equal(t, before, diagnosticCaptureStorageState.totalBytes)
		})
	}
}

func TestDiagnosticMaintenanceDefersConflictingCleanupAndRetry(t *testing.T) {
	cfg := captureUsageFixture(t)
	now := time.Now()
	flow := &DiagnosticFlow{TraceID: "maintenance-retry", Channel: "channel", Started: now.Add(-48 * time.Hour)}
	body := []byte(`{"message":"retained through cleanup"}`)
	require.NoError(t, os.MkdirAll(cfg.TempDir, 0o700))
	partPath := filepath.Join(cfg.TempDir, "request.part")
	require.NoError(t, os.WriteFile(partPath, body, 0o600))
	part := &diagnosticCapturePartState{
		partID: "inbound-request", role: "inbound", part: "request",
		tempPath: partPath, savedSize: int64(len(body)), originalSize: int64(len(body)), complete: true,
	}
	require.NoError(t, persistDiagnosticCaptureFailure(cfg, flow, []*diagnosticCapturePartState{part}, "retry fixture", true))
	failurePath := diagnosticCaptureFailureRecordPath(cfg, flow)
	originalFailure, err := os.ReadFile(failurePath)
	require.NoError(t, err)
	dir := filepath.Join(cfg.CaptureDir, flow.Channel, flow.Started.Format("2006-01-02"), flow.TraceID)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".capture-created-at"), []byte(strconv.FormatInt(flow.Started.UnixNano(), 10)), 0o600))
	candidate := diagnosticCaptureCandidate{dir: dir, traceID: flow.TraceID, capturedAt: flow.Started}
	cutoff, incompleteCutoff := now.Add(-7*24*time.Hour), now.Add(-24*time.Hour)

	// Both background callers must skip a record claimed by the other, without
	// consuming retry data or touching a partially restored formal directory.
	release := claimDiagnosticCaptureMaintenance(flow.TraceID)
	require.NotNil(t, release)
	t.Cleanup(func() {
		if release != nil {
			release()
		}
	})
	retryDiagnosticCaptureFailures(cfg)
	unchanged, err := os.ReadFile(failurePath)
	require.NoError(t, err)
	assert.Equal(t, originalFailure, unchanged)
	assert.NoFileExists(t, filepath.Join(dir, "request-log.json"))
	freed, removed, err := removeEligibleDiagnosticCapture(cfg, candidate, cutoff, incompleteCutoff, nil)
	require.NoError(t, err)
	assert.Zero(t, freed)
	assert.False(t, removed)
	assert.FileExists(t, filepath.Join(dir, ".capture-created-at"))
	// Archival changes the directory name, not the record being protected.
	archivedClaim := claimDiagnosticCaptureMaintenance(".abandoned-" + flow.TraceID)
	if archivedClaim != nil {
		archivedClaim()
		t.Fatal("archival must retain the same maintenance claim")
	}
	release()
	release = nil

	retryDiagnosticCaptureFailures(cfg)
	data, err := os.ReadFile(filepath.Join(dir, "request-log.json"))
	require.NoError(t, err)
	var combined diagnosticCombinedCPAJSON
	require.NoError(t, common.Unmarshal(data, &combined))
	require.NotNil(t, combined.Request)
	assert.Equal(t, map[string]any{"message": "retained through cleanup"}, combined.Request.Body.JSON)
	assert.NoDirExists(t, filepath.Dir(failurePath))
	assert.True(t, diagnosticCaptureComplete(dir))

	// A candidate selected as incomplete before recovery must now respect the
	// longer completed-log retention period, rather than delete the new log.
	freed, removed, err = removeEligibleDiagnosticCapture(cfg, candidate, cutoff, incompleteCutoff, nil)
	require.NoError(t, err)
	assert.Zero(t, freed)
	assert.False(t, removed)
	assert.FileExists(t, filepath.Join(dir, "request-log.json"))
	freed, removed, err = removeEligibleDiagnosticCapture(cfg, candidate, now, incompleteCutoff, nil)
	require.NoError(t, err)
	assert.Positive(t, freed)
	assert.True(t, removed)
	assert.NoDirExists(t, dir)
}
