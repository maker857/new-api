package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticUsageScanCountsConcurrentWriteExactlyOnce(t *testing.T) {
	for _, order := range []string{"before-observation", "after-observation", "during-stat", "in-flight-at-start", "in-flight-at-publish"} {
		t.Run(order, func(t *testing.T) {
			cfg := captureUsageFixture(t)
			require.NoError(t, os.MkdirAll(cfg.CaptureDir, 0o700))
			path := filepath.Join(cfg.CaptureDir, "request-log.json")
			require.NoError(t, os.WriteFile(path, []byte("12345"), 0o600))
			var change *diagnosticCaptureFileChange
			if order == "in-flight-at-start" {
				change = trackDiagnosticCaptureFile(cfg, path)
			}
			scan := beginDiagnosticCaptureUsageScan(cfg)
			if order == "after-observation" {
				require.True(t, observeDiagnosticCaptureFile(scan, 0, path, 5))
			}
			if change == nil {
				change = trackDiagnosticCaptureFile(cfg, path)
			}
			require.NoError(t, os.WriteFile(path, []byte("1234567890"), 0o600))
			if order != "in-flight-at-publish" {
				change.finishFromDisk()
			}
			if order != "after-observation" {
				size := int64(10)
				if order == "during-stat" {
					size = 5
				} // stat raced the write
				require.True(t, observeDiagnosticCaptureFile(scan, 0, path, size))
			}
			require.True(t, completeDiagnosticCaptureUsageScan(scan))
			if order == "in-flight-at-publish" {
				change.finishFromDisk()
			}
			assert.Equal(t, int64(10), diagnosticCaptureStorageState.totalBytes)
			assert.False(t, diagnosticCaptureStorageState.estimated)
		})
	}
}

func TestDiagnosticUsageScanCountsNewAndDeletedFilesAcrossItsFrontier(t *testing.T) {
	for _, observed := range []bool{false, true} {
		t.Run(map[bool]string{false: "delete-before-scan", true: "delete-after-scan"}[observed], func(t *testing.T) {
			cfg := captureUsageFixture(t)
			require.NoError(t, os.MkdirAll(cfg.CaptureDir, 0o700))
			old := filepath.Join(cfg.CaptureDir, "old.json")
			require.NoError(t, os.WriteFile(old, []byte("12345"), 0o600))
			scan := beginDiagnosticCaptureUsageScan(cfg)
			if observed {
				require.True(t, observeDiagnosticCaptureFile(scan, 0, old, 5))
			}
			require.NoError(t, removeDiagnosticCaptureFile(cfg, old))
			// These names can be omitted from a directory listing already read by
			// WalkDir, whether they sort before or after the current frontier.
			for _, name := range []string{"a.json", "z.json"} {
				path := filepath.Join(cfg.CaptureDir, name)
				change := trackDiagnosticCaptureFile(cfg, path)
				require.NoError(t, os.WriteFile(path, []byte("123"), 0o600))
				change.finishFromDisk()
			}
			require.True(t, completeDiagnosticCaptureUsageScan(scan))
			assert.Equal(t, int64(6), diagnosticCaptureStorageState.totalBytes)
		})
	}
}

func TestDiagnosticUsageScanKeepsMovedFragmentsAndMetadataAccurate(t *testing.T) {
	cfg := captureUsageFixture(t)
	for _, dir := range []string{cfg.CaptureDir, cfg.TempDir, cfg.FailureDir} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}
	source := filepath.Join(cfg.TempDir, "body.part")
	destination := filepath.Join(cfg.FailureDir, "body.part")
	require.NoError(t, os.WriteFile(source, []byte("12345"), 0o600))
	scan := beginDiagnosticCaptureUsageScan(cfg)
	require.True(t, observeDiagnosticCaptureFile(scan, 1, source, 5))
	require.NoError(t, moveDiagnosticCaptureFragment(cfg, source, destination))
	require.True(t, observeDiagnosticCaptureFile(scan, 2, destination, 5))
	require.True(t, completeDiagnosticCaptureUsageScan(scan))
	assert.Equal(t, int64(5), diagnosticCaptureStorageState.totalBytes)
	assert.Zero(t, diagnosticCaptureStorageState.tempBytes)
	flow := &DiagnosticFlow{TraceID: "real-log", Channel: "test", Started: time.Now()}
	require.NoError(t, writeDiagnosticCaptureSession(cfg, flow, nil))
	markDiagnosticCaptureComplete(cfg, flow)
	actual, err := diagnosticCaptureDirectorySize(cfg.CaptureDir)
	require.NoError(t, err)
	assert.Equal(t, actual+5, diagnosticCaptureStorageState.totalBytes, "count staging rename and both marker files once")
}

func TestDiagnosticUsageFrontierMatchesDirectoryWalkOrder(t *testing.T) {
	cfg := captureUsageFixture(t)
	path := filepath.Join(cfg.CaptureDir, "a", "body.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("12345"), 0o600))
	scan := beginDiagnosticCaptureUsageScan(cfg)
	require.True(t, observeDiagnosticCaptureFile(scan, 0, path, 5))
	require.True(t, observeDiagnosticCaptureFile(scan, 0, filepath.Join(cfg.CaptureDir, "a.json"), 0))
	change := trackDiagnosticCaptureFile(cfg, path)
	require.NoError(t, os.WriteFile(path, []byte("12"), 0o600))
	change.finishFromDisk()
	require.True(t, completeDiagnosticCaptureUsageScan(scan))
	assert.Equal(t, int64(2), diagnosticCaptureStorageState.totalBytes)
}

func TestDiagnosticMaintenanceReleasesGateBetweenBatches(t *testing.T) {
	captureUsageFixture(t)
	batch := &diagnosticCaptureMaintenanceBatch{}
	require.NoError(t, batch.step())
	batch.pause(0)
	require.True(t, diagnosticCaptureCleanupMu.TryLock(), "other maintenance must run while this batch pauses")
	diagnosticCaptureCleanupMu.Unlock()
}

func TestDiagnosticUsageScanCountsRepeatedWritesToUnlistedNewFile(t *testing.T) {
	cfg := captureUsageFixture(t)
	path := filepath.Join(cfg.CaptureDir, "a.json")
	scan := beginDiagnosticCaptureUsageScan(cfg)
	beginDiagnosticCaptureFileChange(cfg, path, 0).finish(3)
	// The new file was not in the walker's cached listing. The walk moves
	// past it, and subsequent writes must not reuse an old correction twice.
	require.True(t, observeDiagnosticCaptureFile(scan, 0, filepath.Join(cfg.CaptureDir, "z.json"), 0))
	beginDiagnosticCaptureFileChange(cfg, path, 3).finish(5)
	beginDiagnosticCaptureFileChange(cfg, path, 5).finish(7)
	require.True(t, completeDiagnosticCaptureUsageScan(scan))
	assert.Equal(t, int64(7), diagnosticCaptureStorageState.totalBytes)
}

func TestDiagnosticUsageNormalizesConfiguredPaths(t *testing.T) {
	cfg := captureUsageFixture(t)
	cfg.CaptureDir += string(os.PathSeparator) + "."
	configureDiagnosticCaptureStorage(cfg)
	require.NoError(t, os.MkdirAll(cfg.CaptureDir, 0o700))
	path := filepath.Join(cfg.CaptureDir, "body.json")
	change := trackDiagnosticCaptureFile(cfg, path)
	require.NoError(t, os.WriteFile(path, []byte("12345"), 0o600))
	change.finishFromDisk()
	assert.Equal(t, int64(5), diagnosticCaptureStorageState.totalBytes)
	scanDiagnosticCaptureUsage(cfg)
	assert.Equal(t, int64(5), diagnosticCaptureStorageState.totalBytes)
}

func TestDiagnosticUsageDoesNotClaimExactSizeAfterMetadataReadFailure(t *testing.T) {
	cfg := captureUsageFixture(t)
	scan := beginDiagnosticCaptureUsageScan(cfg)
	// NUL is rejected by both Unix and Windows, unlike permissions that root
	// or administrator test processes may bypass.
	change := trackDiagnosticCaptureFile(cfg, filepath.Join(cfg.CaptureDir, "unreadable\x00.json"))
	require.NotNil(t, change)
	assert.True(t, diagnosticCaptureStorageState.estimated)
	change.finish(5)
	require.True(t, completeDiagnosticCaptureUsageScan(scan))
	assert.True(t, diagnosticCaptureStorageState.estimated, "a successful completion must not hide the unknown original size")
}
