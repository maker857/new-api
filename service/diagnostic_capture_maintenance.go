package service

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var diagnosticCaptureCleanupRunning atomic.Bool
var diagnosticCaptureRetryRunning atomic.Bool
var diagnosticCaptureTempCleanupRunning atomic.Bool

var diagnosticCaptureMaintenanceRecords = struct {
	sync.Mutex
	busy map[string]bool
}{busy: make(map[string]bool)}

// Only cleanup and failure recovery claim records. Busy background work is
// skipped, never waited for; normal capture writers do not use this gate.
func claimDiagnosticCaptureMaintenance(traceID string) func() {
	traceID = safeTraceID(strings.TrimPrefix(traceID, ".abandoned-"))
	diagnosticCaptureMaintenanceRecords.Lock()
	defer diagnosticCaptureMaintenanceRecords.Unlock()
	if diagnosticCaptureMaintenanceRecords.busy[traceID] {
		return nil
	}
	diagnosticCaptureMaintenanceRecords.busy[traceID] = true
	return func() {
		diagnosticCaptureMaintenanceRecords.Lock()
		delete(diagnosticCaptureMaintenanceRecords.busy, traceID)
		diagnosticCaptureMaintenanceRecords.Unlock()
	}
}

// Only background maintenance uses this gate. Release it between bounded
// batches and before rate-limit sleeps; capture writers never acquire it.
type diagnosticCaptureMaintenanceBatch struct {
	held       bool
	operations int
	started    time.Time
}

func (batch *diagnosticCaptureMaintenanceBatch) release() {
	if batch.held {
		diagnosticCaptureCleanupMu.Unlock()
		batch.held = false
	}
}

func (batch *diagnosticCaptureMaintenanceBatch) step() error {
	if diagnosticCaptureUsageStopping.Load() {
		batch.release()
		return context.Canceled
	}
	if batch.held && batch.operations < 32 && time.Since(batch.started) < 25*time.Millisecond {
		batch.operations++
		return nil
	}
	if batch.held {
		batch.release()
		// Let other work run between batches without waiting for slow writes
		// or introducing an elapsed-time-dependent scan delay.
		runtime.Gosched()
	}
	diagnosticCaptureCleanupMu.Lock()
	batch.held = true
	batch.operations = 1
	batch.started = time.Now()
	return nil
}

func (batch *diagnosticCaptureMaintenanceBatch) pause(delay time.Duration) {
	batch.release()
	for delay > 0 && !diagnosticCaptureUsageStopping.Load() {
		interval := min(delay, 100*time.Millisecond)
		time.Sleep(interval)
		delay -= interval
	}
}
