package service

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// File changes are registered BEFORE disk I/O and committed AFTER it. Only
// small bookkeeping operations hold the counter lock; writers never wait for
// a directory walk, checkpoint, or maintenance lock.
type diagnosticCaptureFileChange struct {
	path      string
	before    int64
	epoch     uint64
	temporary bool
}

type diagnosticCaptureScan struct {
	epoch     uint64
	roots     [3]string
	frontier  [3]string
	finished  [3]bool
	uncertain bool
	// Only files changed during this scan are retained, not the whole archive.
	changed          map[string]int64
	total, temporary int64
}

func diagnosticCaptureRootIndex(roots [3]string, path string) int {
	for index, root := range roots {
		if root != "" && (path == root || strings.HasPrefix(path, root+string(os.PathSeparator))) {
			return index
		}
	}
	return -1
}

func diagnosticCaptureUsageRoots(cfg DiagnosticCaptureConfig) [3]string {
	roots := [3]string{cfg.CaptureDir, cfg.TempDir, cfg.FailureDir}
	for index, root := range roots {
		if root != "" {
			roots[index] = filepath.Clean(root)
		}
	}
	return roots
}

// WalkDir is depth-first and sorts names within each directory. Comparing
// components (not complete path strings) matches that order, including a/b
// versus a.json. The frontier tells us whether an old file was already counted.
func diagnosticCapturePathVisited(path, frontier string) bool {
	if frontier == "" {
		return false
	}
	a, b := strings.Split(path, string(os.PathSeparator)), strings.Split(frontier, string(os.PathSeparator))
	for index := 0; index < len(a) && index < len(b); index++ {
		if a[index] != b[index] {
			return a[index] < b[index]
		}
	}
	return len(a) <= len(b)
}

func (scan *diagnosticCaptureScan) change(path string, previous, current int64) {
	index := diagnosticCaptureRootIndex(scan.roots, path)
	if index < 0 {
		return
	}
	counted, exists := scan.changed[path]
	visited := scan.finished[index] || diagnosticCapturePathVisited(path, scan.frontier[index])
	if !exists {
		if visited {
			counted = previous
		}
	}
	scan.total += current - counted
	if index == 1 {
		scan.temporary += current - counted
	}
	if visited {
		delete(scan.changed, path)
	} else {
		scan.changed[path] = current
	}
}

func beginDiagnosticCaptureFileChange(cfg DiagnosticCaptureConfig, path string, previous int64) *diagnosticCaptureFileChange {
	path = filepath.Clean(path)
	state := &diagnosticCaptureStorageState
	state.Lock()
	defer state.Unlock()
	if state.captureDir != cfg.CaptureDir || state.tempDir != cfg.TempDir || state.failureDir != cfg.FailureDir {
		return nil
	}
	roots := diagnosticCaptureUsageRoots(cfg)
	index := diagnosticCaptureRootIndex(roots, path)
	if index < 0 {
		return nil
	}
	change := &diagnosticCaptureFileChange{path: path, before: previous, epoch: state.epoch, temporary: index == 1}
	if state.activeFiles == nil {
		state.activeFiles = make(map[string]*diagnosticCaptureFileChange)
	}
	state.activeFiles[path] = change
	if state.scan != nil {
		state.scan.change(path, previous, previous)
	}
	return change
}

func (change *diagnosticCaptureFileChange) finish(current int64) {
	if change == nil {
		return
	}
	state := &diagnosticCaptureStorageState
	state.Lock()
	defer state.Unlock()
	if state.epoch != change.epoch {
		return
	}
	delete(state.activeFiles, change.path)
	delta := current - change.before
	state.totalBytes += delta
	if change.temporary {
		state.tempBytes += delta
	}
	if state.initialized && (state.totalBytes < 0 || state.tempBytes < 0) {
		state.totalBytes = max(0, state.totalBytes)
		state.tempBytes = max(0, state.tempBytes)
		state.estimated = true
	}
	if state.scan != nil {
		state.scan.change(change.path, change.before, current)
	}
}

// Use this for infrequent metadata operations. Body chunks already know their
// exact previous size and byte count, so they avoid additional Stat calls.
func trackDiagnosticCaptureFile(cfg DiagnosticCaptureConfig, path string) *diagnosticCaptureFileChange {
	var size int64
	info, err := os.Lstat(path)
	if err == nil {
		size = info.Size()
	}
	change := beginDiagnosticCaptureFileChange(cfg, path, size)
	if change != nil && err != nil && !os.IsNotExist(err) {
		// If the old size was unreadable, even a successful write cannot make
		// its delta exact. Keep writing, but never present that count as exact.
		diagnosticCaptureStorageState.Lock()
		if diagnosticCaptureStorageState.epoch == change.epoch {
			diagnosticCaptureStorageState.estimated = true
			if diagnosticCaptureStorageState.scan != nil {
				diagnosticCaptureStorageState.scan.uncertain = true
			}
		}
		diagnosticCaptureStorageState.Unlock()
	}
	return change
}

func (change *diagnosticCaptureFileChange) finishFromDisk() {
	if change == nil {
		return
	}
	info, err := os.Lstat(change.path)
	if os.IsNotExist(err) {
		change.finish(0)
		return
	}
	if err == nil {
		change.finish(info.Size())
		return
	}
	// An unreadable file is not zero bytes. Keep its previous count and expose
	// that the result is estimated until the scheduled reconciliation succeeds.
	change.finish(change.before)
	diagnosticCaptureStorageState.Lock()
	if diagnosticCaptureStorageState.epoch == change.epoch {
		diagnosticCaptureStorageState.estimated = true
		if diagnosticCaptureStorageState.scan != nil {
			diagnosticCaptureStorageState.scan.uncertain = true
		}
	}
	diagnosticCaptureStorageState.Unlock()
}

type diagnosticCaptureTrackedWriter struct {
	file *os.File
	cfg  DiagnosticCaptureConfig
	size int64
}

func (writer *diagnosticCaptureTrackedWriter) Write(data []byte) (int, error) {
	change := beginDiagnosticCaptureFileChange(writer.cfg, writer.file.Name(), writer.size)
	n, err := writer.file.Write(data)
	writer.size += int64(n)
	change.finish(writer.size)
	return n, err
}

func removeDiagnosticCaptureFile(cfg DiagnosticCaptureConfig, path string) error {
	change := trackDiagnosticCaptureFile(cfg, path)
	defer change.finishFromDisk()
	return os.Remove(path)
}

// Recheck eligibility after claiming the record: recovery may have completed
// since candidate selection, changing which retention period applies.
func removeEligibleDiagnosticCapture(cfg DiagnosticCaptureConfig, candidate diagnosticCaptureCandidate, cutoff, incompleteCutoff time.Time, batch *diagnosticCaptureMaintenanceBatch) (int64, bool, error) {
	release := claimDiagnosticCaptureMaintenance(candidate.traceID)
	if release == nil {
		return 0, false, nil
	}
	defer release()
	if isDiagnosticCaptureActive(candidate.traceID) {
		return 0, false, nil
	}
	capturedAt, exists := diagnosticCaptureCreatedAt(candidate.dir)
	if !exists {
		return 0, false, nil
	}
	if diagnosticCaptureComplete(candidate.dir) {
		if !capturedAt.Before(cutoff) {
			return 0, false, nil
		}
	} else if incompleteCutoff.IsZero() || !capturedAt.Before(incompleteCutoff) {
		return 0, false, nil
	}
	if diagnosticCaptureRootIndex([3]string{cfg.FailureDir}, candidate.dir) >= 0 && diagnosticCaptureFailureRetryable(candidate.dir, time.Now()) {
		return 0, false, nil
	}
	freed, err := removeDiagnosticCaptureDirectory(cfg, candidate.dir, batch)
	return freed, err == nil, err
}

// Count each successfully removed file immediately. A crash in the middle of
// a large cleanup no longer leaves the entire batch awaiting one subtraction.
func removeDiagnosticCaptureDirectory(cfg DiagnosticCaptureConfig, dir string, batch *diagnosticCaptureMaintenanceBatch) (freed int64, err error) {
	dir = filepath.Clean(dir)
	dirs := []string{}
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if os.IsNotExist(walkErr) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if batch != nil {
			if err := batch.step(); err != nil {
				return err
			}
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		// Retain both markers until every body file and child directory is
		// gone. Otherwise a failed deletion can become invisible to retention
		// cleanup, or a completed log can be misclassified as incomplete.
		if filepath.Dir(path) == dir && (entry.Name() == ".capture-created-at" || entry.Name() == ".capture-complete") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if err := removeDiagnosticCaptureFile(cfg, path); err != nil {
			return err
		}
		freed += info.Size()
		return nil
	})
	if err != nil {
		return freed, err
	}
	for index := len(dirs) - 1; index >= 0; index-- {
		if dirs[index] == dir {
			continue
		}
		if batch != nil {
			if err := batch.step(); err != nil {
				return freed, err
			}
		}
		if err := os.Remove(dirs[index]); err != nil && !os.IsNotExist(err) {
			return freed, err
		}
	}
	// Keep the small marker contents for rollback if final removal fails.
	// Restoring the original timestamp preserves the existing retention rule.
	type cleanupMarker struct {
		path string
		data []byte
	}
	markers := make([]cleanupMarker, 0, 2)
	for _, name := range []string{".capture-complete", ".capture-created-at"} {
		path := filepath.Join(dir, name)
		data, readErr := os.ReadFile(path)
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			return freed, readErr
		}
		markers = append(markers, cleanupMarker{path: path, data: data})
	}
	removed := 0
	defer func() {
		if err == nil {
			return
		}
		for _, marker := range markers[:removed] {
			change := trackDiagnosticCaptureFile(cfg, marker.path)
			restoreErr := os.WriteFile(marker.path, marker.data, 0o600)
			change.finishFromDisk()
			if info, statErr := os.Lstat(marker.path); statErr == nil {
				freed -= info.Size()
			}
			err = errors.Join(err, restoreErr)
		}
	}()
	for _, marker := range markers {
		if err := removeDiagnosticCaptureFile(cfg, marker.path); err != nil {
			return freed, err
		}
		removed++
		freed += int64(len(marker.data))
	}
	if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
		return freed, err
	}
	return freed, nil
}

func renameDiagnosticCaptureDirectory(cfg DiagnosticCaptureConfig, source, destination string) error {
	changes := []*diagnosticCaptureFileChange{}
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		changes = append(changes, trackDiagnosticCaptureFile(cfg, path), trackDiagnosticCaptureFile(cfg, filepath.Join(destination, relative)))
		return nil
	})
	if err == nil {
		err = os.Rename(source, destination)
	}
	for _, change := range changes {
		change.finishFromDisk()
	}
	return err
}
