package service

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
