package service

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestDiagnosticCleanupRetriesAfterBodyDeletionFails(t *testing.T) {
	for _, test := range []struct {
		name       string
		lockedFile string
		freed      int64
	}{
		{name: "body", lockedFile: "request-log.json", freed: 3},
		{name: "last-marker", lockedFile: ".capture-created-at", freed: 8},
		{name: "final-directory", lockedFile: ".", freed: 8},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := captureUsageFixture(t)
			day := filepath.Join(cfg.CaptureDir, "channel", "2026-01-01")
			dir := filepath.Join(day, "request")
			require.NoError(t, os.MkdirAll(dir, 0o700))
			created := []byte(strconv.FormatInt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano(), 10))
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".capture-created-at"), created, 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".capture-complete"), []byte("complete"), 0o600))
			// The earlier file is deleted successfully before the locked body fails.
			require.NoError(t, os.WriteFile(filepath.Join(dir, "a.part"), []byte("123"), 0o600))
			body := filepath.Join(dir, "request-log.json")
			require.NoError(t, os.WriteFile(body, []byte("12345"), 0o600))
			scanDiagnosticCaptureUsage(cfg)
			before := diagnosticCaptureStorageState.totalBytes
			path, err := windows.UTF16PtrFromString(filepath.Join(dir, test.lockedFile))
			require.NoError(t, err)
			var flags uint32
			if test.lockedFile == "." {
				flags = windows.FILE_FLAG_BACKUP_SEMANTICS
			}
			handle, err := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, flags, 0)
			require.NoError(t, err)
			t.Cleanup(func() {
				if handle != windows.InvalidHandle {
					_ = windows.CloseHandle(handle)
				}
			})
			freed, err := removeDiagnosticCaptureDirectory(cfg, dir, nil)
			require.Error(t, err)
			assert.Equal(t, test.freed, freed)
			assert.Equal(t, before-test.freed, diagnosticCaptureStorageState.totalBytes)
			if test.lockedFile == "request-log.json" {
				assert.FileExists(t, body)
			}
			marker, err := os.ReadFile(filepath.Join(dir, ".capture-created-at"))
			require.NoError(t, err)
			assert.Equal(t, created, marker, "retry must keep the original retention timestamp")
			assert.True(t, diagnosticCaptureComplete(dir), "failed cleanup must not turn a completed log into an incomplete one")
			batch := &diagnosticCaptureMaintenanceBatch{}
			defer batch.release()
			candidates := diagnosticCaptureOldestCandidates([]diagnosticCaptureDayDir{{path: day}}, "", time.Now(), time.Time{}, batch)
			require.Len(t, candidates, 1, "retry must remain eligible even when incomplete cleanup is disabled")
			batch.release()
			require.NoError(t, windows.CloseHandle(handle))
			handle = windows.InvalidHandle
			freed, err = removeDiagnosticCaptureDirectory(cfg, candidates[0].dir, nil)
			require.NoError(t, err)
			assert.Equal(t, before-test.freed, freed)
			assert.NoDirExists(t, dir)
			assert.Zero(t, diagnosticCaptureStorageState.totalBytes)
		})
	}
}

func TestDiagnosticCleanupStopsFailedFullBatchAndRetriesLater(t *testing.T) {
	cfg := captureUsageFixture(t)
	handles := make([]windows.Handle, 0, diagnosticCaptureCleanupSelectSize)
	t.Cleanup(func() {
		for _, handle := range handles {
			_ = windows.CloseHandle(handle)
		}
	})
	created := []byte(strconv.FormatInt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano(), 10))
	// A complete selection batch is the minimum case that used to reselect
	// the same failures forever. Real deny-delete handles make it deterministic.
	for index := 0; index < diagnosticCaptureCleanupSelectSize; index++ {
		dir := filepath.Join(cfg.CaptureDir, "channel", "2026-01-01", strconv.Itoa(index))
		require.NoError(t, os.MkdirAll(dir, 0o700))
		marker := filepath.Join(dir, ".capture-created-at")
		require.NoError(t, os.WriteFile(marker, created, 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".capture-complete"), []byte("complete"), 0o600))
		path, err := windows.UTF16PtrFromString(marker)
		require.NoError(t, err)
		handle, err := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
		require.NoError(t, err)
		handles = append(handles, handle)
	}
	scanDiagnosticCaptureUsage(cfg)
	before := diagnosticCaptureStorageState.totalBytes
	remaining, deleted, freed, _ := cleanupDiagnosticCaptureStorageByDate(before, cfg, "", 0, time.Now(), time.Time{})
	assert.Equal(t, before, remaining)
	assert.Zero(t, deleted)
	assert.Zero(t, freed, "rolled-back marker removals do not free capacity")
	for _, handle := range handles {
		require.NoError(t, windows.CloseHandle(handle))
	}
	handles = nil
	remaining, deleted, freed, _ = cleanupDiagnosticCaptureStorageByDate(before, cfg, "", 0, time.Now(), time.Time{})
	assert.Zero(t, remaining)
	assert.EqualValues(t, diagnosticCaptureCleanupSelectSize, deleted)
	assert.Equal(t, before, freed)
	assert.Zero(t, diagnosticCaptureStorageState.totalBytes)
}
