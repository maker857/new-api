package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const diagnosticCaptureCheckpointInterval = 5 * time.Second

var diagnosticCaptureUsageWake = make(chan struct{}, 1)
var diagnosticCaptureScanRequested atomic.Bool
var diagnosticCaptureScanRunning atomic.Bool
var diagnosticCaptureScanRetryAfter atomic.Int64
var diagnosticCaptureUsageStopping atomic.Bool
var diagnosticCaptureUsageMu sync.Mutex // background checkpoint/restore only

// configureDiagnosticCaptureStorage only touches memory. In particular it must
// never resolve symlinks, stat files, scan directories, or query a database.
func configureDiagnosticCaptureStorage(cfg DiagnosticCaptureConfig) {
	diagnosticCaptureStorageState.Lock()
	defer diagnosticCaptureStorageState.Unlock()
	if diagnosticCaptureStorageState.captureDir == cfg.CaptureDir && diagnosticCaptureStorageState.tempDir == cfg.TempDir && diagnosticCaptureStorageState.failureDir == cfg.FailureDir {
		return
	}
	if diagnosticCaptureStorageState.captureDir != "" {
		diagnosticCaptureScanRequested.Store(true)
	}
	diagnosticCaptureStorageState.captureDir = cfg.CaptureDir
	diagnosticCaptureStorageState.tempDir = cfg.TempDir
	diagnosticCaptureStorageState.failureDir = cfg.FailureDir
	diagnosticCaptureStorageState.totalBytes = 0
	diagnosticCaptureStorageState.tempBytes = 0
	diagnosticCaptureStorageState.initialized = false
	diagnosticCaptureStorageState.estimated = false
	diagnosticCaptureStorageState.uncertaintyBytes = 0
	diagnosticCaptureStorageState.activeFiles = nil
	diagnosticCaptureStorageState.scan = nil
	diagnosticCaptureStorageState.lastSaved = time.Time{}
	diagnosticCaptureStorageState.lastSaveError = false
	diagnosticCaptureStorageState.lastScanError = false
	diagnosticCaptureStorageState.restored = false
	diagnosticCaptureStorageState.epoch++
	diagnosticCaptureStorageState.restoreAttempted = false
	diagnosticCaptureStorageState.scope = ""
	diagnosticCaptureStorageState.storageID = ""
	diagnosticCaptureStorageState.usagePath = ""
	diagnosticCaptureStorageState.lastAttempt = time.Time{}
	diagnosticCaptureStorageState.nextEligibleAt = time.Time{}
	if cfg.NextCleanupEligibleAt > time.Now().Unix() {
		diagnosticCaptureStorageState.nextEligibleAt = time.Unix(cfg.NextCleanupEligibleAt, 0)
	}
	diagnosticCaptureStorageState.lastReconciled = time.Time{}
	diagnosticCaptureScanRetryAfter.Store(0)
}

func wakeDiagnosticCaptureUsage() {
	select {
	case diagnosticCaptureUsageWake <- struct{}{}:
	default:
	}
}

// diagnosticCaptureStorageScope is evaluated only by the background worker.
// Statistics belong to the storage directories, not a process or NODE_NAME.
// The optional node name is used only to recognize version-2 checkpoints on
// upgrade; new checkpoints always use the directory-only scope.
func diagnosticCaptureStorageScope(cfg DiagnosticCaptureConfig, legacyNodeName ...string) (string, error) {
	captureDir, err := resolveDiagnosticCaptureDirectory(cfg.CaptureDir)
	if err != nil {
		return "", err
	}
	tempDir, err := filepath.Abs(cfg.TempDir)
	if err != nil {
		return "", err
	}
	failureDir, err := filepath.Abs(cfg.FailureDir)
	if err != nil {
		return "", err
	}
	identity := captureDir + "\x00" + tempDir + "\x00" + failureDir
	if len(legacyNodeName) > 0 {
		identity = legacyNodeName[0] + "\x00" + identity
	}
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:]), nil
}

// restoreDiagnosticCaptureUsage preserves writes that occurred while the file
// read was in flight. It never holds the counter mutex across filesystem I/O.
func restoreDiagnosticCaptureUsage(cfg DiagnosticCaptureConfig, scope string, usage *diagnosticCaptureUsage) {
	diagnosticCaptureStorageState.Lock()
	defer diagnosticCaptureStorageState.Unlock()
	if diagnosticCaptureStorageState.captureDir != cfg.CaptureDir || diagnosticCaptureStorageState.tempDir != cfg.TempDir || diagnosticCaptureStorageState.failureDir != cfg.FailureDir {
		return
	}
	diagnosticCaptureStorageState.scope = scope
	if usage != nil && usage.StorageID != "" {
		diagnosticCaptureStorageState.storageID = usage.StorageID
	} else if diagnosticCaptureStorageState.storageID == "" {
		diagnosticCaptureStorageState.storageID = common.GetUUID()
	}
	diagnosticCaptureStorageState.usagePath = filepath.Join(filepath.Dir(cfg.CaptureDir), diagnosticCaptureUsageFilename)
	diagnosticCaptureStorageState.restoreAttempted = true
	if diagnosticCaptureStorageState.initialized {
		return // a background bootstrap already supplied a newer baseline
	}
	if usage == nil {
		diagnosticCaptureScanRequested.Store(true)
		return
	}
	diagnosticCaptureStorageState.totalBytes = max(0, usage.TotalBytes+diagnosticCaptureStorageState.totalBytes)
	diagnosticCaptureStorageState.tempBytes = min(diagnosticCaptureStorageState.totalBytes, max(0, usage.TemporaryBytes+diagnosticCaptureStorageState.tempBytes))
	diagnosticCaptureStorageState.lastReconciled = time.Unix(usage.ReconciledAt, 0)
	diagnosticCaptureStorageState.initialized = true
	diagnosticCaptureStorageState.estimated = usage.Estimated || !usage.CleanShutdown
	// Even an interrupted restart uses the last valid checkpoint. Scheduled
	// reconciliation corrects unsaved deltas without imposing a startup walk.
	diagnosticCaptureStorageState.lastSaved = time.Unix(usage.SavedAt, 0)
	diagnosticCaptureStorageState.restored = true
	diagnosticCaptureStorageState.uncertaintyBytes = 0

}

func checkpointDiagnosticCaptureUsage(ctx context.Context, clean bool) error {
	diagnosticCaptureStorageState.Lock()
	if !diagnosticCaptureStorageState.initialized || diagnosticCaptureStorageState.scope == "" {
		diagnosticCaptureStorageState.Unlock()
		return nil
	}
	scope := diagnosticCaptureStorageState.scope
	path := diagnosticCaptureStorageState.usagePath
	usage := diagnosticCaptureUsage{
		Scope:     scope,
		StorageID: diagnosticCaptureStorageState.storageID,
		Version:   3, TotalBytes: max(0, diagnosticCaptureStorageState.totalBytes),
		TemporaryBytes: max(0, diagnosticCaptureStorageState.tempBytes),
		SavedAt:        time.Now().Unix(), ReconciledAt: diagnosticCaptureStorageState.lastReconciled.Unix(),
		CleanShutdown:    clean && !diagnosticCaptureScanRunning.Load() && !diagnosticCaptureScanRequested.Load(),
		Estimated:        diagnosticCaptureStorageState.estimated,
		UncertaintyBytes: diagnosticCaptureStorageState.uncertaintyBytes,
	}
	usage.TemporaryBytes = min(usage.TotalBytes, usage.TemporaryBytes)
	diagnosticCaptureStorageState.Unlock()
	err := saveDiagnosticCaptureUsage(ctx, path, usage)
	diagnosticCaptureStorageState.Lock()
	if diagnosticCaptureStorageState.scope == scope && diagnosticCaptureStorageState.usagePath == path {
		diagnosticCaptureStorageState.lastSaveError = err != nil
		if err == nil {
			diagnosticCaptureStorageState.lastSaved = time.Unix(usage.SavedAt, 0)
		}
	}
	diagnosticCaptureStorageState.Unlock()
	return err
}

func runDiagnosticCaptureUsageWorker() {
	ticker := time.NewTicker(diagnosticCaptureCheckpointInterval)
	defer ticker.Stop()
	for !diagnosticCaptureUsageStopping.Load() {
		cfg := DiagnosticCaptureConfigFromOptions()
		configureDiagnosticCaptureStorage(cfg)
		diagnosticCaptureUsageMu.Lock()
		if diagnosticCaptureUsageStopping.Load() {
			diagnosticCaptureUsageMu.Unlock()
			return
		}
		diagnosticCaptureStorageState.Lock()
		loaded := diagnosticCaptureStorageState.restoreAttempted
		diagnosticCaptureStorageState.Unlock()
		if !loaded {
			scope, err := diagnosticCaptureStorageScope(cfg)
			if err == nil {
				legacyScope, legacyErr := diagnosticCaptureStorageScope(cfg, common.NodeName)
				if legacyErr != nil {
					legacyScope = "" // valid version-3 files need no legacy identity
				}
				path := filepath.Join(filepath.Dir(cfg.CaptureDir), diagnosticCaptureUsageFilename)
				usage, loadErr := loadDiagnosticCaptureUsage(path, scope, legacyScope)
				// A corrupt/unavailable checkpoint is not a reason to stop
				// accounting. Bootstrap in the background and retry later saves.
				restoreDiagnosticCaptureUsage(cfg, scope, usage)
				err = loadErr
			}
			if err != nil {
				diagnosticCaptureStorageState.Lock()
				initialized := diagnosticCaptureStorageState.initialized
				diagnosticCaptureStorageState.Unlock()
				if !initialized {
					diagnosticCaptureScanRequested.Store(true)
				}
				logDiagnosticCaptureMaintenanceError(diagnosticCaptureMaintenanceScanError, "failed to restore diagnostic capture usage: "+err.Error())
			}
		}
		err := checkpointDiagnosticCaptureUsage(context.Background(), false)
		diagnosticCaptureUsageMu.Unlock()
		if err != nil {
			logDiagnosticCaptureMaintenanceError(diagnosticCaptureMaintenanceScanError, "failed to checkpoint diagnostic capture usage: "+err.Error())
		}
		if diagnosticCaptureScanRequested.Load() {
			startDiagnosticCaptureUsageScan(cfg)
		}
		cleanupDiagnosticCaptureStorageIfNeeded(cfg)
		select {
		case <-ticker.C:
		case <-diagnosticCaptureUsageWake:
		}
	}
}

// startDiagnosticCaptureUsageScan coalesces all scan triggers. The maintenance
// lock is never acquired by capture writes, and a slow scan cannot hold the
// checkpoint worker or request goroutines hostage.
func startDiagnosticCaptureUsageScan(cfg DiagnosticCaptureConfig) {
	if diagnosticCaptureUsageStopping.Load() || time.Now().UnixNano() < diagnosticCaptureScanRetryAfter.Load() {
		return
	}
	if !diagnosticCaptureScanRunning.CompareAndSwap(false, true) {
		return
	}
	if !diagnosticCaptureCleanupMu.TryLock() {
		diagnosticCaptureScanRunning.Store(false)
		return
	}
	diagnosticCaptureCleanupMu.Unlock()
	diagnosticCaptureScanRequested.Store(false)
	go func() {
		defer diagnosticCaptureScanRunning.Store(false)
		scanDiagnosticCaptureUsage(cfg)
	}()
}

// beginDiagnosticCaptureUsageScan also includes writes already in progress.
// Their committed sizes are authoritative until the write completes.
func beginDiagnosticCaptureUsageScan(cfg DiagnosticCaptureConfig) *diagnosticCaptureScan {
	state := &diagnosticCaptureStorageState
	state.Lock()
	defer state.Unlock()
	if state.captureDir != cfg.CaptureDir || state.tempDir != cfg.TempDir || state.failureDir != cfg.FailureDir {
		return nil
	}
	scan := &diagnosticCaptureScan{epoch: state.epoch, roots: diagnosticCaptureUsageRoots(cfg), changed: make(map[string]int64)}
	for _, change := range state.activeFiles {
		scan.change(change.path, change.before, change.before)
	}
	state.scan = scan
	return scan
}

// observe and file-change commits use the same short memory lock. A file is
// counted either by the walker or by its absolute committed size, never both.
func observeDiagnosticCaptureFile(scan *diagnosticCaptureScan, index int, path string, size int64) bool {
	state := &diagnosticCaptureStorageState
	state.Lock()
	defer state.Unlock()
	if state.scan != scan || state.epoch != scan.epoch {
		return false
	}
	if _, changed := scan.changed[path]; !changed {
		scan.total += size
		if index == 1 {
			scan.temporary += size
		}
	}
	delete(scan.changed, path) // the ordered walk will never observe this path again
	scan.frontier[index] = path
	return true
}

func scanDiagnosticCaptureUsage(cfg DiagnosticCaptureConfig) {
	scan := beginDiagnosticCaptureUsageScan(cfg)
	if scan == nil {
		return
	}
	batch := &diagnosticCaptureMaintenanceBatch{}
	defer batch.release()
	var scanErr error
	for index, root := range scan.roots {
		if root == "" {
			continue
		}
		// Follow a configured root symlink, but keep accounting keys in the
		// configured namespace used by writers. WalkDir itself does not follow it.
		walkRoot, resolveErr := filepath.EvalSymlinks(root)
		if errors.Is(resolveErr, os.ErrNotExist) {
			walkRoot = root
		} else if resolveErr != nil {
			scanErr = resolveErr
			break
		}
		scanErr = filepath.WalkDir(walkRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if err := batch.step(); err != nil {
				return err
			}
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			relative, err := filepath.Rel(walkRoot, path)
			if err != nil {
				return err
			}
			path = filepath.Join(root, relative)
			var size int64
			if !entry.IsDir() {
				info, err := entry.Info()
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				if err != nil {
					return err
				}
				size = info.Size()
			}
			if !observeDiagnosticCaptureFile(scan, index, path, size) {
				return context.Canceled
			}
			return nil
		})
		if scanErr != nil {
			break
		}
		diagnosticCaptureStorageState.Lock()
		scan.finished[index] = true
		diagnosticCaptureStorageState.Unlock()
	}
	batch.release()
	state := &diagnosticCaptureStorageState
	state.Lock()
	if state.scan != scan || state.epoch != scan.epoch {
		state.Unlock()
		return
	}
	state.lastScanError = scanErr != nil
	if scanErr != nil {
		state.scan = nil
	}
	state.Unlock()
	if scanErr == nil {
		completeDiagnosticCaptureUsageScan(scan)
	}
	if scanErr != nil {
		diagnosticCaptureScanRetryAfter.Store(time.Now().Add(time.Minute).UnixNano())
		diagnosticCaptureScanRequested.Store(true)
		if !errors.Is(scanErr, context.Canceled) {
			logDiagnosticCaptureMaintenanceError(diagnosticCaptureMaintenanceScanError, "failed to scan diagnostic capture storage: "+scanErr.Error())
		}
		return
	}
	wakeDiagnosticCaptureUsage()
	cleanupDiagnosticCaptureStorageIfNeeded(cfg)
}

// Publishing is atomic with writer commits. In-flight operations already have
// their previous size in the scan and commit the remaining difference later.
func completeDiagnosticCaptureUsageScan(scan *diagnosticCaptureScan) bool {
	state := &diagnosticCaptureStorageState
	state.Lock()
	defer state.Unlock()
	if state.scan != scan || state.epoch != scan.epoch {
		return false
	}
	state.totalBytes = max(0, scan.total)
	state.tempBytes = min(state.totalBytes, max(0, scan.temporary))
	state.uncertaintyBytes = 0
	state.estimated = scan.uncertain
	state.restored = false
	state.initialized = true
	state.lastReconciled = time.Now()
	state.scan = nil
	return true
}

// StopDiagnosticCaptureUsage bounds the caller's wait even if a filesystem
// operation is stuck. Call after HTTP shutdown so capture sessions can drain.
func StopDiagnosticCaptureUsage(ctx context.Context) error {
	diagnosticCaptureUsageStopping.Store(true)
	wakeDiagnosticCaptureUsage()
	done := make(chan error, 1)
	go func() {
		diagnosticCaptureUsageMu.Lock()
		defer diagnosticCaptureUsageMu.Unlock()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for diagnosticCaptureSessionCount.Load() > 0 {
			select {
			case <-ctx.Done():
				done <- ctx.Err()
				return
			case <-ticker.C:
			}
		}
		locked := diagnosticCaptureCleanupMu.TryLock()
		if locked {
			defer diagnosticCaptureCleanupMu.Unlock()
		}
		clean := locked && !diagnosticCaptureCleanupRunning.Load() && !diagnosticCaptureRetryRunning.Load() && !diagnosticCaptureTempCleanupRunning.Load()
		done <- checkpointDiagnosticCaptureUsage(ctx, clean)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("diagnostic capture shutdown checkpoint: %w", ctx.Err())
	}
}
