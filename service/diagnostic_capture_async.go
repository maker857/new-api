package service

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	diagnosticCaptureEventBuffer = 256
	diagnosticCaptureChunkSize   = 64 * 1024
	// Keep diagnostic disk work bounded while allowing independent traces to finish together.
	diagnosticCaptureFinalizeConcurrency = 2
	diagnosticCaptureCleanupInterval     = 10 * time.Minute
	diagnosticCaptureRetryMaxDelay       = 6 * time.Hour
	diagnosticCaptureRetryWindow         = time.Hour
)

type diagnosticCaptureEventKind uint8

const (
	diagnosticCapturePartStart diagnosticCaptureEventKind = iota
	diagnosticCapturePartChunk
	diagnosticCapturePartEnd
)

type diagnosticCaptureEvent struct {
	kind         diagnosticCaptureEventKind
	partID       string
	sequence     int64
	role         string
	part         string
	meta         map[string]any
	data         []byte
	originalSize int64
	complete     bool
}

type diagnosticCaptureSession struct {
	cfg           DiagnosticCaptureConfig
	events        chan diagnosticCaptureEvent
	pendingMu     sync.Mutex
	pending       []diagnosticCaptureEvent
	pendingHead   int
	pendingClosed bool
	pendingWake   chan struct{}
	producers     sync.WaitGroup
	failed        atomic.Bool
	discard       atomic.Bool
	stateMu       sync.RWMutex
	closed        bool
	closing       bool
	failCause     string
	finalFlow     DiagnosticFlow
	activeTraceID string
	spoolDir      string
}

var diagnosticCaptureFinalizeState = struct {
	sync.Mutex
	locks map[string]*diagnosticCaptureFinalizeLock
}{locks: make(map[string]*diagnosticCaptureFinalizeLock)}

var diagnosticCaptureFinalizeSlots = make(chan struct{}, diagnosticCaptureFinalizeConcurrency)

// Limit simultaneous spool writes across requests. Relay handlers only append
// to their asynchronous queue, so this bounds disk contention without making
// upstream or downstream traffic wait for logging I/O.
var diagnosticCaptureSpoolWriteSlots = make(chan struct{}, 8)

type diagnosticCaptureFinalizeLock struct {
	mu   sync.Mutex
	refs int
}

type diagnosticCapturePartState struct {
	partID       string
	sequence     int64
	role         string
	part         string
	meta         map[string]any
	file         *os.File
	tempPath     string
	savedSize    int64
	originalSize int64
	complete     bool
}

type diagnosticCaptureFailureRecord struct {
	TraceID       string                         `json:"newapi_request_id"`
	Channel       string                         `json:"channel"`
	StartedAt     int64                          `json:"started_at"`
	FirstFailedAt int64                          `json:"first_failed_at"`
	AttemptCount  int                            `json:"attempt_count"`
	LastError     string                         `json:"last_error"`
	LastAttemptAt int64                          `json:"last_attempt_at"`
	NextRetryAt   int64                          `json:"next_retry_at,omitempty"`
	Retryable     bool                           `json:"retryable"`
	Parts         []diagnosticCaptureFailurePart `json:"parts"`
}

type diagnosticCaptureFailurePart struct {
	PartID       string         `json:"part_id"`
	Sequence     int64          `json:"sequence"`
	Role         string         `json:"role"`
	Part         string         `json:"part"`
	Meta         map[string]any `json:"meta,omitempty"`
	FileName     string         `json:"file_name,omitempty"`
	TempFileName string         `json:"temp_file_name,omitempty"`
	SavedSize    int64          `json:"saved_size"`
	OriginalSize int64          `json:"original_size"`
	Complete     bool           `json:"complete"`
}

type diagnosticCaptureStream struct {
	reader   io.Reader
	closer   io.Closer
	session  *diagnosticCaptureSession
	partID   string
	total    int64
	finished bool
	mu       sync.Mutex
}

func newDiagnosticCaptureSession(cfg DiagnosticCaptureConfig, flow *DiagnosticFlow) *diagnosticCaptureSession {
	if flow == nil {
		return nil
	}
	session := &diagnosticCaptureSession{
		cfg:           cfg,
		events:        make(chan diagnosticCaptureEvent, diagnosticCaptureEventBuffer),
		pendingWake:   make(chan struct{}, 1),
		activeTraceID: safeTraceID(flow.TraceID),
	}
	tempRoot := cfg.TempDir
	if tempRoot == "" {
		tempRoot = "diagnostic-capture-temp"
	}
	session.spoolDir = filepath.Join(tempRoot, flow.Started.Format("2006-01-02"), session.activeTraceID)
	markDiagnosticCaptureActive(session.activeTraceID)
	go session.dispatch()
	go session.run()
	return session
}

func (s *diagnosticCaptureSession) fail(reason string) {
	if s == nil {
		return
	}
	s.failed.Store(true)
	s.stateMu.Lock()
	if s.failCause == "" {
		s.failCause = reason
	}
	s.stateMu.Unlock()
}

func (s *diagnosticCaptureSession) tryEvent(event diagnosticCaptureEvent) bool {
	if s == nil || s.failed.Load() || s.discard.Load() {
		return false
	}
	s.stateMu.RLock()
	if s.closed {
		s.stateMu.RUnlock()
		return false
	}
	s.pendingMu.Lock()
	s.pending = append(s.pending, event)
	s.pendingMu.Unlock()
	s.stateMu.RUnlock()
	select {
	case s.pendingWake <- struct{}{}:
	default:
	}
	return true
}

// dispatch keeps relay reads and writes non-blocking. Events are copied before
// they arrive here, then emitted to the disk worker in their original order.
// The pending queue intentionally has no drop-on-full branch: a full logging
// queue must never silently turn a successfully proxied request into a partial
// diagnostic record.
func (s *diagnosticCaptureSession) dispatch() {
	defer close(s.events)
	for {
		s.pendingMu.Lock()
		if s.pendingHead >= len(s.pending) {
			if s.pendingClosed {
				s.pendingMu.Unlock()
				return
			}
			s.pendingMu.Unlock()
			<-s.pendingWake
			continue
		}
		event := s.pending[s.pendingHead]
		s.pending[s.pendingHead] = diagnosticCaptureEvent{}
		s.pendingHead++
		if s.pendingHead == len(s.pending) {
			s.pending = nil
			s.pendingHead = 0
		}
		s.pendingMu.Unlock()
		s.events <- event
	}
}

func (s *diagnosticCaptureSession) startPart(partID string, sequence int64, role, part string, meta map[string]any) {
	s.tryEvent(diagnosticCaptureEvent{
		kind:     diagnosticCapturePartStart,
		partID:   partID,
		sequence: sequence,
		role:     role,
		part:     part,
		meta:     meta,
	})
}

func (s *diagnosticCaptureSession) writeChunk(partID string, data []byte) {
	if s == nil || s.failed.Load() || len(data) == 0 {
		return
	}
	for len(data) > 0 {
		chunkSize := len(data)
		if chunkSize > diagnosticCaptureChunkSize {
			chunkSize = diagnosticCaptureChunkSize
		}
		chunk := append([]byte(nil), data[:chunkSize]...)
		if !s.tryEvent(diagnosticCaptureEvent{
			kind:   diagnosticCapturePartChunk,
			partID: partID,
			data:   chunk,
		}) {
			return
		}
		data = data[chunkSize:]
	}
}

func (s *diagnosticCaptureSession) endPart(partID string, meta map[string]any, originalSize int64, complete bool) {
	s.tryEvent(diagnosticCaptureEvent{
		kind:         diagnosticCapturePartEnd,
		partID:       partID,
		meta:         meta,
		originalSize: originalSize,
		complete:     complete,
	})
}

func (s *diagnosticCaptureSession) close(flow *DiagnosticFlow) {
	if s == nil {
		return
	}
	s.stateMu.Lock()
	if s.closed || s.closing {
		s.stateMu.Unlock()
		return
	}
	if flow != nil {
		s.finalFlow = *flow
	}
	s.closing = true
	s.stateMu.Unlock()
	go func() {
		s.producers.Wait()
		s.stateMu.Lock()
		s.closed = true
		s.stateMu.Unlock()
		s.pendingMu.Lock()
		s.pendingClosed = true
		s.pendingMu.Unlock()
		select {
		case s.pendingWake <- struct{}{}:
		default:
		}
	}()
}

func (s *diagnosticCaptureSession) cancel(flow *DiagnosticFlow) {
	if s == nil {
		return
	}
	s.discard.Store(true)
	s.close(flow)
}

func (s *diagnosticCaptureSession) run() {
	defer markDiagnosticCaptureInactive(s.activeTraceID)
	parts := make(map[string]*diagnosticCapturePartState)
	orderedParts := make([]*diagnosticCapturePartState, 0, 4)
	for event := range s.events {
		switch event.kind {
		case diagnosticCapturePartStart:
			if _, exists := parts[event.partID]; exists {
				continue
			}
			state := &diagnosticCapturePartState{
				partID:   event.partID,
				sequence: event.sequence,
				role:     event.role,
				part:     event.part,
				meta:     event.meta,
			}
			parts[event.partID] = state
			orderedParts = append(orderedParts, state)
		case diagnosticCapturePartChunk:
			state := parts[event.partID]
			if state == nil || s.failed.Load() {
				continue
			}
			if state.file == nil {
				spoolDir := s.spoolDir
				prepareDiagnosticCaptureTempDir(s.cfg)
				if err := os.MkdirAll(spoolDir, 0o700); err != nil {
					s.fail("failed to create diagnostic capture temp dir: " + err.Error())
					continue
				}
				prefix := safeCaptureName(state.partID, "part") + "-"
				file, err := os.CreateTemp(spoolDir, prefix+"*.part")
				if err != nil {
					s.fail("failed to create diagnostic temp file: " + err.Error())
					continue
				}
				state.file = file
				state.tempPath = file.Name()
				markDiagnosticTempFileActive(state.tempPath)
			}
			diagnosticCaptureSpoolWriteSlots <- struct{}{}
			_, writeErr := state.file.Write(event.data)
			<-diagnosticCaptureSpoolWriteSlots
			if writeErr != nil {
				s.fail("failed to write diagnostic body spool: " + writeErr.Error())
				continue
			}
			state.savedSize += int64(len(event.data))
			enforceDiagnosticCaptureStorage(s.cfg, "", 0, int64(len(event.data)))
			recordDiagnosticCaptureTempBytesChange(s.cfg, int64(len(event.data)))
		case diagnosticCapturePartEnd:
			state := parts[event.partID]
			if state == nil {
				continue
			}
			if event.meta != nil {
				state.meta = event.meta
			}
			state.originalSize = event.originalSize
			state.complete = event.complete
		}
	}

	s.stateMu.RLock()
	flow := s.finalFlow
	failCause := s.failCause
	s.stateMu.RUnlock()
	if s.discard.Load() {
		for _, state := range orderedParts {
			if state.file != nil {
				_ = state.file.Close()
			}
			if state.tempPath != "" {
				removeDiagnosticCaptureTempFile(s.cfg, state)
			}
		}
		return
	}
	if flow.TraceID == "" {
		flow.TraceID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if flow.Channel == "" {
		flow.Channel = "unknown"
	}
	if flow.Started.IsZero() {
		flow.Started = time.Now()
	}

	base := filepath.Join(
		s.cfg.CaptureDir,
		safeCaptureName(flow.Channel, "unknown"),
		flow.Started.Format("2006-01-02"),
		safeTraceID(flow.TraceID),
	)
	if err := os.MkdirAll(base, 0o755); err != nil {
		s.fail("failed to create diagnostic capture dir: " + err.Error())
	}

	for _, state := range orderedParts {
		if state.file != nil {
			if err := state.file.Close(); err != nil {
				s.fail("failed to close diagnostic temp file: " + err.Error())
			}
		}
	}
	captureFailed := s.failed.Load()
	writeErr := writeDiagnosticCaptureSession(s.cfg, &flow, orderedParts)
	if writeErr != nil {
		s.fail("failed to write diagnostic request-log.json: " + writeErr.Error())
	}
	if captureFailed || writeErr != nil {
		cause := s.failCause
		if cause == "" && writeErr != nil {
			cause = writeErr.Error()
		}
		if persistErr := persistDiagnosticCaptureFailure(s.cfg, &flow, orderedParts, cause, !captureFailed); persistErr != nil {
			logDiagnosticCaptureFailure("failed to retain diagnostic capture retry data: " + persistErr.Error())
		}
	} else {
		for _, state := range orderedParts {
			if state.tempPath != "" {
				removeDiagnosticCaptureTempFile(s.cfg, state)
			}
		}
	}

	if !s.failed.Load() {
		markDiagnosticCaptureComplete(s.cfg, &flow)
	} else {
		s.stateMu.RLock()
		failCause = s.failCause
		s.stateMu.RUnlock()
		if failCause == "" {
			failCause = "diagnostic capture incomplete"
		}
		logDiagnosticCaptureFailure("diagnostic capture incomplete: " + failCause)
	}
}

func newDiagnosticCaptureStream(reader io.Reader, session *diagnosticCaptureSession, partID string) *diagnosticCaptureStream {
	stream := &diagnosticCaptureStream{reader: reader, session: session, partID: partID}
	if closer, ok := reader.(io.Closer); ok {
		stream.closer = closer
	}
	return stream
}

func (s *diagnosticCaptureStream) Read(p []byte) (int, error) {
	n, err := s.reader.Read(p)
	if n > 0 {
		s.total += int64(n)
		s.session.writeChunk(s.partID, p[:n])
	}
	if err == io.EOF {
		s.finish(true)
	}
	return n, err
}

func (s *diagnosticCaptureStream) Close() error {
	s.finish(false)
	if s.closer != nil {
		return s.closer.Close()
	}
	return nil
}

func (s *diagnosticCaptureStream) finish(complete bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return
	}
	s.finished = true
	s.session.endPart(s.partID, nil, s.total, complete)
}

func (s *diagnosticCaptureStream) finishWithMeta(meta map[string]any, complete bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return
	}
	s.finished = true
	s.session.endPart(s.partID, meta, s.total, complete)
}

var diagnosticCaptureTempJanitor struct {
	sync.Mutex
	last    map[string]time.Time
	running map[string]bool
}

var diagnosticCaptureCleanupLoop sync.Once

func prepareDiagnosticCaptureTempDir(cfg DiagnosticCaptureConfig) {
	tempDir := strings.TrimSpace(cfg.TempDir)
	if tempDir == "" {
		return
	}
	diagnosticCaptureTempJanitor.Lock()
	if diagnosticCaptureTempJanitor.last == nil {
		diagnosticCaptureTempJanitor.last = make(map[string]time.Time)
	}
	if diagnosticCaptureTempJanitor.running == nil {
		diagnosticCaptureTempJanitor.running = make(map[string]bool)
	}
	last := diagnosticCaptureTempJanitor.last[tempDir]
	now := time.Now()
	if diagnosticCaptureTempJanitor.running[tempDir] || (!last.IsZero() && now.Sub(last) < 10*time.Minute) {
		diagnosticCaptureTempJanitor.Unlock()
		return
	}
	diagnosticCaptureTempJanitor.last[tempDir] = now
	diagnosticCaptureTempJanitor.running[tempDir] = true
	diagnosticCaptureTempJanitor.Unlock()

	retention := time.Duration(cfg.TempRetentionMinutes) * time.Minute
	if retention <= 0 {
		retention = time.Hour
	}
	cutoff := now.Add(-retention)
	go func() {
		defer func() {
			diagnosticCaptureTempJanitor.Lock()
			diagnosticCaptureTempJanitor.running[tempDir] = false
			diagnosticCaptureTempJanitor.Unlock()
		}()
		deletedCount, freedBytes := cleanupDiagnosticCaptureTempFilesAtRate(tempDir, cutoff, cfg.CleanupRateBytesPerSecond)
		recordDiagnosticCaptureTempCleanup(deletedCount, freedBytes)
		recordDiagnosticCaptureTempStorageDeletion(cfg, freedBytes)
	}()
}

// StartDiagnosticCaptureCleanup performs diagnostic-file maintenance outside
// the relay request path.
func StartDiagnosticCaptureCleanup() {
	diagnosticCaptureCleanupLoop.Do(func() {
		go func() {
			runDiagnosticCaptureCleanup()
			ticker := time.NewTicker(diagnosticCaptureCleanupInterval)
			defer ticker.Stop()
			for range ticker.C {
				runDiagnosticCaptureCleanup()
			}
		}()
	})
}

func runDiagnosticCaptureCleanup() {
	cfg := DiagnosticCaptureConfigFromOptions()
	prepareDiagnosticCaptureTempDir(cfg)
	refreshDiagnosticCaptureStorageState(cfg)
	retryDiagnosticCaptureFailures(cfg)
	cleanupDiagnosticCaptureStorageIfNeeded(cfg)
}

func diagnosticCaptureFailureDir(captureDir string) string {
	captureDir = strings.TrimSpace(captureDir)
	if captureDir == "" {
		captureDir = "captures"
	}
	return filepath.Join(filepath.Dir(filepath.Clean(captureDir)), "diagnostic-capture-failures")
}

func diagnosticCaptureFailureRecordPath(cfg DiagnosticCaptureConfig, flow *DiagnosticFlow) string {
	return filepath.Join(
		cfg.FailureDir,
		safeCaptureName(flow.Channel, "unknown"),
		flow.Started.Format("2006-01-02"),
		safeTraceID(flow.TraceID),
		"capture-failure.json",
	)
}

func persistDiagnosticCaptureFailure(cfg DiagnosticCaptureConfig, flow *DiagnosticFlow, parts []*diagnosticCapturePartState, cause string, retryable bool) error {
	if flow == nil {
		return fmt.Errorf("capture flow is missing")
	}
	path := diagnosticCaptureFailureRecordPath(cfg, flow)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	previousSize, err := diagnosticCaptureDirectorySize(dir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	ensureDiagnosticCaptureTimestamp(dir)
	movedBytes := int64(0)
	defer func() {
		currentSize, sizeErr := diagnosticCaptureDirectorySize(dir)
		if sizeErr == nil {
			// Renames move already-accounted temporary bytes into the failure
			// directory. Only the failure metadata itself is a net addition.
			enforceDiagnosticCaptureStorage(cfg, "", previousSize, currentSize-movedBytes)
		}
	}()
	record := diagnosticCaptureFailureRecord{
		TraceID:       flow.TraceID,
		Channel:       flow.Channel,
		StartedAt:     flow.Started.UnixNano(),
		FirstFailedAt: time.Now().UnixNano(),
		AttemptCount:  1,
		LastError:     cause,
		LastAttemptAt: time.Now().UnixNano(),
		Retryable:     retryable,
		Parts:         make([]diagnosticCaptureFailurePart, 0, len(parts)),
	}
	for index, state := range parts {
		if state == nil {
			continue
		}
		part := diagnosticCaptureFailurePart{
			PartID:       state.partID,
			Sequence:     state.sequence,
			Role:         state.role,
			Part:         state.part,
			Meta:         state.meta,
			SavedSize:    state.savedSize,
			OriginalSize: state.originalSize,
			Complete:     state.complete,
		}
		if state.tempPath != "" {
			part.FileName = fmt.Sprintf("%03d.part", index)
			part.TempFileName = filepath.Base(state.tempPath)
		}
		record.Parts = append(record.Parts, part)
	}
	// Persist the retry index before moving any source data. A crash or I/O
	// error can then leave an incomplete failure directory, but never an
	// unindexed collection of otherwise recoverable body fragments.
	if err := writeDiagnosticCaptureFailureRecord(path, record); err != nil {
		return err
	}
	for _, state := range parts {
		if state == nil || state.tempPath == "" {
			continue
		}
		partName := ""
		for _, part := range record.Parts {
			if part.PartID == state.partID {
				partName = part.FileName
				break
			}
		}
		if partName == "" {
			continue
		}
		destination := filepath.Join(dir, partName)
		if err := os.Rename(state.tempPath, destination); err != nil {
			return err
		}
		movedBytes += state.savedSize
		recordDiagnosticCaptureTempBytesChange(cfg, -state.savedSize)
		markDiagnosticTempFileInactive(state.tempPath)
		state.tempPath = destination
	}
	return nil
}

// writeDiagnosticCaptureFailureRecord protects the retry index independently
// from the formal capture directory. Failure metadata is not synchronized as
// a formal request log, so a same-directory temporary file is safe here.
func writeDiagnosticCaptureFailureRecord(path string, record diagnosticCaptureFailureRecord) error {
	data, err := common.Marshal(record)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".capture-failure-*.tmp")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func updateDiagnosticCaptureFailureRecord(cfg DiagnosticCaptureConfig, path string, record diagnosticCaptureFailureRecord) error {
	previousSize := int64(0)
	if info, err := os.Stat(path); err == nil {
		previousSize = info.Size()
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := writeDiagnosticCaptureFailureRecord(path, record); err != nil {
		return err
	}
	if info, err := os.Stat(path); err == nil {
		enforceDiagnosticCaptureStorage(cfg, "", previousSize, info.Size())
	}
	return nil
}

func retryDiagnosticCaptureFailures(cfg DiagnosticCaptureConfig) {
	if cfg.FailureDir == "" {
		return
	}
	_ = filepath.WalkDir(cfg.FailureDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || entry.Name() != "capture-failure.json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var record diagnosticCaptureFailureRecord
		if common.Unmarshal(data, &record) != nil || !record.Retryable || record.TraceID == "" || record.StartedAt <= 0 {
			return nil
		}
		now := time.Now()
		if record.NextRetryAt > now.UnixNano() {
			return filepath.SkipDir
		}
		firstFailedAt := record.FirstFailedAt
		if firstFailedAt == 0 {
			firstFailedAt = record.LastAttemptAt
			if firstFailedAt == 0 {
				firstFailedAt = record.StartedAt
			}
			record.FirstFailedAt = firstFailedAt
		}
		if firstFailedAt > 0 && now.Sub(time.Unix(0, firstFailedAt)) >= diagnosticCaptureRetryWindow {
			record.Retryable = false
			record.NextRetryAt = 0
			record.LastError = "capture retry stopped: one-hour retry window expired"
			record.LastAttemptAt = now.UnixNano()
			_ = updateDiagnosticCaptureFailureRecord(cfg, path, record)
			return filepath.SkipDir
		}
		flow := &DiagnosticFlow{TraceID: record.TraceID, Channel: record.Channel, Started: time.Unix(0, record.StartedAt)}
		parts := make([]*diagnosticCapturePartState, 0, len(record.Parts))
		for _, part := range record.Parts {
			state := &diagnosticCapturePartState{
				partID:       part.PartID,
				sequence:     part.Sequence,
				role:         part.Role,
				part:         part.Part,
				meta:         part.Meta,
				savedSize:    part.SavedSize,
				originalSize: part.OriginalSize,
				complete:     part.Complete,
			}
			if part.FileName != "" {
				state.tempPath = filepath.Join(filepath.Dir(path), filepath.Base(part.FileName))
			}
			parts = append(parts, state)
		}
		failureDir := filepath.Dir(path)
		for index, part := range record.Parts {
			if index >= len(parts) || part.FileName == "" {
				continue
			}
			if _, statErr := os.Stat(parts[index].tempPath); statErr == nil {
				continue
			}
			tempPath := diagnosticCaptureFailureTempPartPath(cfg, flow, part)
			if tempPath == "" {
				record.Retryable = false
				record.LastError = "capture retry stopped: retained body fragment is missing"
				record.LastAttemptAt = time.Now().UnixNano()
				_ = updateDiagnosticCaptureFailureRecord(cfg, path, record)
				return filepath.SkipDir
			}
			destination := filepath.Join(failureDir, filepath.Base(part.FileName))
			if err := os.Rename(tempPath, destination); err != nil {
				record.Retryable = false
				record.LastError = "capture retry stopped: failed to restore retained body fragment: " + err.Error()
				record.LastAttemptAt = time.Now().UnixNano()
				_ = updateDiagnosticCaptureFailureRecord(cfg, path, record)
				return filepath.SkipDir
			}
			markDiagnosticTempFileInactive(tempPath)
			recordDiagnosticCaptureTempBytesChange(cfg, -parts[index].savedSize)
			parts[index].tempPath = destination
		}
		record.AttemptCount++
		record.LastAttemptAt = now.UnixNano()
		if err := writeDiagnosticCaptureSession(cfg, flow, parts); err != nil {
			record.LastError = err.Error()
			nextRetryAt := now.Add(diagnosticCaptureRetryDelay(record.AttemptCount))
			if nextRetryAt.Sub(time.Unix(0, firstFailedAt)) >= diagnosticCaptureRetryWindow {
				record.Retryable = false
				record.NextRetryAt = 0
				record.LastError = "capture retry stopped: next retry would exceed one-hour retry window: " + err.Error()
			} else {
				record.NextRetryAt = nextRetryAt.UnixNano()
			}
			_ = updateDiagnosticCaptureFailureRecord(cfg, path, record)
			return nil
		}
		markDiagnosticCaptureComplete(cfg, flow)
		if size, err := diagnosticCaptureDirectorySize(failureDir); err == nil {
			if os.RemoveAll(failureDir) == nil {
				enforceDiagnosticCaptureStorage(cfg, "", size, 0)
			}
		}
		return filepath.SkipDir
	})
}

func diagnosticCaptureRetryDelay(attemptCount int) time.Duration {
	if attemptCount <= 1 {
		return diagnosticCaptureCleanupInterval
	}
	delay := diagnosticCaptureCleanupInterval
	for attempt := 1; attempt < attemptCount && delay < diagnosticCaptureRetryMaxDelay; attempt++ {
		delay *= 2
		if delay > diagnosticCaptureRetryMaxDelay {
			return diagnosticCaptureRetryMaxDelay
		}
	}
	return delay
}

// diagnosticCaptureFailureTempPartPath restores a fragment that was still in
// its request-scoped temporary directory when the process stopped between
// writing the retry index and moving the fragment into the failure directory.
func diagnosticCaptureFailureTempPartPath(cfg DiagnosticCaptureConfig, flow *DiagnosticFlow, part diagnosticCaptureFailurePart) string {
	if flow == nil {
		return ""
	}
	tempRoot := cfg.TempDir
	if tempRoot == "" {
		tempRoot = "diagnostic-capture-temp"
	}
	dir := filepath.Join(tempRoot, flow.Started.Format("2006-01-02"), safeTraceID(flow.TraceID))
	if part.TempFileName != "" {
		candidate := filepath.Join(dir, filepath.Base(part.TempFileName))
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	prefix := safeCaptureName(part.PartID, "part") + "-"
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".part") {
			continue
		}
		return filepath.Join(dir, entry.Name())
	}
	return ""
}

func cleanupDiagnosticCaptureTempFiles(tempDir string, cutoff time.Time) (int64, int64) {
	return cleanupDiagnosticCaptureTempFilesAtRate(tempDir, cutoff, 0)
}

func cleanupDiagnosticCaptureTempFilesAtRate(tempDir string, cutoff time.Time, rateBytesPerSecond int64) (int64, int64) {
	var deletedCount int64
	var freedBytes int64
	batchFreedBytes := int64(0)
	batchStartedAt := time.Now()
	_ = filepath.WalkDir(tempDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || isDiagnosticTempFileActive(path) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			return nil
		}
		if os.Remove(path) != nil {
			return nil
		}
		deletedCount++
		freedBytes += info.Size()
		batchFreedBytes += info.Size()
		if batchFreedBytes > 0 && rateBytesPerSecond > 0 {
			expected := diagnosticCaptureRateDuration(batchFreedBytes, rateBytesPerSecond)
			if remaining := expected - time.Since(batchStartedAt); remaining > 0 {
				time.Sleep(remaining)
			}
			batchFreedBytes = 0
			batchStartedAt = time.Now()
		}
		return nil
	})
	directories := make([]string, 0)
	_ = filepath.WalkDir(tempDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr == nil && entry.IsDir() && filepath.Clean(path) != filepath.Clean(tempDir) {
			directories = append(directories, path)
		}
		return nil
	})
	for index := len(directories) - 1; index >= 0; index-- {
		if !hasDiagnosticActiveTempPathUnder(directories[index]) {
			_ = os.Remove(directories[index])
		}
	}
	return deletedCount, freedBytes
}

var diagnosticActiveTempFiles struct {
	sync.RWMutex
	paths map[string]struct{}
}

func markDiagnosticTempFileActive(path string) {
	diagnosticActiveTempFiles.Lock()
	if diagnosticActiveTempFiles.paths == nil {
		diagnosticActiveTempFiles.paths = make(map[string]struct{})
	}
	diagnosticActiveTempFiles.paths[filepath.Clean(path)] = struct{}{}
	diagnosticActiveTempFiles.Unlock()
}

func markDiagnosticTempFileInactive(path string) {
	diagnosticActiveTempFiles.Lock()
	delete(diagnosticActiveTempFiles.paths, filepath.Clean(path))
	diagnosticActiveTempFiles.Unlock()
}

func isDiagnosticTempFileActive(path string) bool {
	diagnosticActiveTempFiles.RLock()
	_, active := diagnosticActiveTempFiles.paths[filepath.Clean(path)]
	diagnosticActiveTempFiles.RUnlock()
	return active
}

func hasDiagnosticActiveTempPathUnder(path string) bool {
	prefix := filepath.Clean(path) + string(os.PathSeparator)
	diagnosticActiveTempFiles.RLock()
	defer diagnosticActiveTempFiles.RUnlock()
	for activePath := range diagnosticActiveTempFiles.paths {
		if strings.HasPrefix(activePath, prefix) {
			return true
		}
	}
	return false
}

func removeDiagnosticCaptureTempFile(cfg DiagnosticCaptureConfig, state *diagnosticCapturePartState) {
	if state == nil || state.tempPath == "" {
		return
	}
	markDiagnosticTempFileInactive(state.tempPath)
	if err := os.Remove(state.tempPath); err == nil {
		enforceDiagnosticCaptureStorage(cfg, "", state.savedSize, 0)
		recordDiagnosticCaptureTempBytesChange(cfg, -state.savedSize)
	}
}

type diagnosticCaptureJSONWriter struct {
	writer *bufio.Writer
	err    error
}

func (w *diagnosticCaptureJSONWriter) raw(value string) {
	if w.err != nil {
		return
	}
	_, w.err = w.writer.WriteString(value)
}

func (w *diagnosticCaptureJSONWriter) bytes(value []byte) {
	if w.err != nil {
		return
	}
	_, w.err = w.writer.Write(value)
}

func (w *diagnosticCaptureJSONWriter) value(value any) {
	if w.err != nil {
		return
	}
	data, err := common.Marshal(value)
	if err != nil {
		w.err = err
		return
	}
	w.bytes(data)
}

func writeDiagnosticCaptureSession(cfg DiagnosticCaptureConfig, flow *DiagnosticFlow, parts []*diagnosticCapturePartState) error {
	if flow == nil {
		return nil
	}
	base := filepath.Join(
		cfg.CaptureDir,
		safeCaptureName(flow.Channel, "unknown"),
		flow.Started.Format("2006-01-02"),
		safeTraceID(flow.TraceID),
	)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return err
	}
	timestampPath := filepath.Join(base, ".capture-created-at")
	previousTimestampSize := int64(0)
	if info, err := os.Stat(timestampPath); err == nil {
		previousTimestampSize = info.Size()
	}
	ensureDiagnosticCaptureTimestamp(base)
	currentTimestampSize := int64(0)
	if info, err := os.Stat(timestampPath); err == nil {
		currentTimestampSize = info.Size()
	}
	path := filepath.Join(base, "request-log.json")
	release := acquireDiagnosticCaptureFinalize(filepath.Clean(base))
	defer release()
	var previousSize int64
	if info, err := os.Stat(path); err == nil {
		previousSize = info.Size()
	}

	var inboundRequest *diagnosticCapturePartState
	var inboundResponse *diagnosticCapturePartState
	apiRequests := make([]*diagnosticCapturePartState, 0)
	apiResponses := make([]*diagnosticCapturePartState, 0)
	for _, state := range parts {
		if state == nil {
			continue
		}
		switch {
		case state.role == "inbound" && state.part == "request":
			inboundRequest = state
		case state.role == "inbound" && state.part == "response":
			inboundResponse = state
		case state.role == "outbound" && state.part == "request":
			apiRequests = append(apiRequests, state)
		case state.role == "outbound" && state.part == "response":
			apiResponses = append(apiResponses, state)
		}
	}
	sort.Slice(apiRequests, func(i, j int) bool { return apiRequests[i].sequence < apiRequests[j].sequence })
	sort.Slice(apiResponses, func(i, j int) bool { return apiResponses[i].sequence < apiResponses[j].sequence })

	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	stream := &diagnosticCaptureJSONWriter{writer: bufio.NewWriterSize(file, diagnosticCaptureChunkSize)}
	stream.raw(`{"format":"cpa-sections-json","version":1`)
	if flow.TraceID != "" {
		stream.raw(`,"newapi_request_id":`)
		stream.value(flow.TraceID)
	}
	if inboundRequest != nil {
		content := buildDiagnosticCPAJSON(cfg, flow, inboundRequest.sequence, inboundRequest.role, inboundRequest.part, inboundRequest.meta, captureBody{})
		if content.RequestInfo != nil {
			stream.raw(`,"request_info":`)
			stream.value(content.RequestInfo)
		}
		if content.Headers != nil {
			stream.raw(`,"headers":`)
			stream.value(content.Headers)
		}
		stream.raw(`,"request_body":`)
		writeDiagnosticCaptureBody(stream, cfg, inboundRequest)
	}
	if len(apiRequests) > 0 {
		stream.raw(`,"api_requests":[`)
		for i, state := range apiRequests {
			if i > 0 {
				stream.raw(",")
			}
			writeDiagnosticAPIRequest(stream, cfg, flow, state)
		}
		stream.raw("]")
	}
	if len(apiResponses) > 0 {
		stream.raw(`,"api_responses":[`)
		for i, state := range apiResponses {
			if i > 0 {
				stream.raw(",")
			}
			writeDiagnosticAPIResponse(stream, cfg, flow, state)
		}
		stream.raw("]")
	}
	if inboundResponse != nil {
		stream.raw(`,"response":`)
		writeDiagnosticInboundResponse(stream, cfg, flow, inboundResponse)
	}
	stream.raw("}\n")
	if stream.err == nil {
		stream.err = stream.writer.Flush()
	}
	closeErr := file.Close()
	if stream.err == nil {
		stream.err = closeErr
	}
	if stream.err != nil {
		return stream.err
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	enforceDiagnosticCaptureStorage(cfg, path, previousSize, info.Size()+currentTimestampSize-previousTimestampSize)
	return nil
}

func acquireDiagnosticCaptureFinalize(key string) func() {
	diagnosticCaptureFinalizeState.Lock()
	lock := diagnosticCaptureFinalizeState.locks[key]
	if lock == nil {
		lock = &diagnosticCaptureFinalizeLock{}
		diagnosticCaptureFinalizeState.locks[key] = lock
	}
	lock.refs++
	diagnosticCaptureFinalizeState.Unlock()

	lock.mu.Lock()
	diagnosticCaptureFinalizeSlots <- struct{}{}
	return func() {
		<-diagnosticCaptureFinalizeSlots
		lock.mu.Unlock()
		diagnosticCaptureFinalizeState.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(diagnosticCaptureFinalizeState.locks, key)
		}
		diagnosticCaptureFinalizeState.Unlock()
	}
}

func writeDiagnosticAPIRequest(w *diagnosticCaptureJSONWriter, cfg DiagnosticCaptureConfig, flow *DiagnosticFlow, state *diagnosticCapturePartState) {
	content := buildDiagnosticCPAJSON(cfg, flow, state.sequence, state.role, state.part, state.meta, captureBody{})
	request := content.APIRequest
	w.raw("{")
	w.raw(`"sequence":`)
	w.value(request.Sequence)
	w.raw(`,"timestamp":`)
	w.value(request.Timestamp)
	w.raw(`,"upstream_url":`)
	w.value(request.UpstreamURL)
	w.raw(`,"http_method":`)
	w.value(request.HTTPMethod)
	if request.Headers != nil {
		w.raw(`,"headers":`)
		w.value(request.Headers)
	}
	w.raw(`,"body":`)
	writeDiagnosticCaptureBody(w, cfg, state)
	w.raw("}")
}

func writeDiagnosticAPIResponse(w *diagnosticCaptureJSONWriter, cfg DiagnosticCaptureConfig, flow *DiagnosticFlow, state *diagnosticCapturePartState) {
	content := buildDiagnosticCPAJSON(cfg, flow, state.sequence, state.role, state.part, state.meta, captureBody{})
	response := content.APIResponse
	w.raw("{")
	w.raw(`"sequence":`)
	w.value(response.Sequence)
	w.raw(`,"timestamp":`)
	w.value(response.Timestamp)
	if response.Status != 0 {
		w.raw(`,"status":`)
		w.value(response.Status)
	}
	if response.Headers != nil {
		w.raw(`,"headers":`)
		w.value(response.Headers)
	}
	w.raw(`,"body":`)
	writeDiagnosticCaptureBody(w, cfg, state)
	if response.Error != "" {
		w.raw(`,"error":`)
		w.value(response.Error)
	}
	w.raw("}")
}

func writeDiagnosticInboundResponse(w *diagnosticCaptureJSONWriter, cfg DiagnosticCaptureConfig, flow *DiagnosticFlow, state *diagnosticCapturePartState) {
	content := buildDiagnosticCPAJSON(cfg, flow, state.sequence, state.role, state.part, state.meta, captureBody{})
	response := content.Response
	w.raw("{")
	first := true
	if response.Status != 0 {
		w.raw(`"status":`)
		w.value(response.Status)
		first = false
	}
	if response.DurationMS != 0 {
		if !first {
			w.raw(",")
		}
		w.raw(`"duration_ms":`)
		w.value(response.DurationMS)
		first = false
	}
	if response.Headers != nil {
		if !first {
			w.raw(",")
		}
		w.raw(`"headers":`)
		w.value(response.Headers)
		first = false
	}
	if !first {
		w.raw(",")
	}
	w.raw(`"body":`)
	writeDiagnosticCaptureBody(w, cfg, state)
	w.raw("}")
}

func writeDiagnosticCaptureBody(w *diagnosticCaptureJSONWriter, cfg DiagnosticCaptureConfig, state *diagnosticCapturePartState) {
	truncated := !state.complete || state.originalSize != state.savedSize
	w.raw(`{"mode":`)
	w.value(cfg.Mode)
	w.raw(`,"body_original_size":`)
	w.value(state.originalSize)
	w.raw(`,"body_saved_size":`)
	w.value(state.savedSize)
	if truncated {
		w.raw(`,"body_truncated":true`)
	}
	if cfg.Mode != "full" {
		w.raw(`,"encoding":"metadata-only"}`)
		return
	}
	if state.tempPath == "" || state.savedSize == 0 {
		w.raw(`,"encoding":"empty"}`)
		return
	}
	jsonBody, err := diagnosticCaptureJSONFile(state.tempPath)
	if err != nil {
		w.err = err
		return
	}
	if jsonBody {
		w.raw(`,"encoding":"json","json":`)
		streamDiagnosticCaptureFile(w, state.tempPath)
	} else {
		w.raw(`,"encoding":"base64","base64":"`)
		streamDiagnosticCaptureFileBase64(w, state.tempPath)
		w.raw(`"`)
	}
	w.raw("}")
}

// diagnosticCaptureJSONFile validates a body without retaining it in memory.
// Formal logs can therefore embed JSON bodies as raw JSON while all other
// payloads are streamed as base64 instead of being materialized as text.
func diagnosticCaptureJSONFile(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	return diagnosticJSONStreamValid(bufio.NewReaderSize(file, diagnosticCaptureChunkSize))
}

func streamDiagnosticCaptureFile(w *diagnosticCaptureJSONWriter, path string) {
	if w.err != nil {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		w.err = err
		return
	}
	defer file.Close()
	_, w.err = io.CopyBuffer(w.writer, file, make([]byte, diagnosticCaptureChunkSize))
}

func streamDiagnosticCaptureFileBase64(w *diagnosticCaptureJSONWriter, path string) {
	if w.err != nil {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		w.err = err
		return
	}
	defer file.Close()
	encoder := base64.NewEncoder(base64.StdEncoding, w.writer)
	_, w.err = io.CopyBuffer(encoder, file, make([]byte, diagnosticCaptureChunkSize))
	if closeErr := encoder.Close(); w.err == nil {
		w.err = closeErr
	}
}

type diagnosticJSONContainer uint8

const (
	diagnosticJSONObject diagnosticJSONContainer = iota
	diagnosticJSONArray
)

type diagnosticJSONState uint8

const (
	diagnosticJSONRootValue diagnosticJSONState = iota
	diagnosticJSONRootDone
	diagnosticJSONObjectKeyOrEnd
	diagnosticJSONObjectColon
	diagnosticJSONObjectValue
	diagnosticJSONObjectCommaOrEnd
	diagnosticJSONArrayValueOrEnd
	diagnosticJSONArrayCommaOrEnd
)

type diagnosticJSONFrame struct {
	kind     diagnosticJSONContainer
	state    diagnosticJSONState
	allowEnd bool
}

// diagnosticJSONStreamValid is a small streaming JSON grammar validator. It
// deliberately stores no token values, including very large string values.
func diagnosticJSONStreamValid(reader *bufio.Reader) (bool, error) {
	state := diagnosticJSONRootValue
	frames := make([]diagnosticJSONFrame, 0, 16)
	inString, stringKey, escaped := false, false, false
	unicodeDigits, literalIndex, numberState := 0, 0, -1
	literal := ""
	seenValue := false

	completeValue := func() bool {
		seenValue = true
		if len(frames) == 0 {
			if state != diagnosticJSONRootValue {
				return false
			}
			state = diagnosticJSONRootDone
			return true
		}
		frame := &frames[len(frames)-1]
		switch frame.state {
		case diagnosticJSONObjectValue:
			frame.state = diagnosticJSONObjectCommaOrEnd
		case diagnosticJSONArrayValueOrEnd:
			frame.state = diagnosticJSONArrayCommaOrEnd
		default:
			return false
		}
		return true
	}

	for {
		value, err := reader.ReadByte()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false, err
		}
		reprocess := true
		for reprocess {
			reprocess = false
			if inString {
				if unicodeDigits > 0 {
					if !((value >= '0' && value <= '9') || (value >= 'a' && value <= 'f') || (value >= 'A' && value <= 'F')) {
						return false, nil
					}
					unicodeDigits--
					continue
				}
				if escaped {
					escaped = false
					if value == 'u' {
						unicodeDigits = 4
					} else if !strings.ContainsRune(`"\\/bfnrt`, rune(value)) {
						return false, nil
					}
					continue
				}
				if value == '\\' {
					escaped = true
					continue
				}
				if value < 0x20 {
					return false, nil
				}
				if value != '"' {
					continue
				}
				inString = false
				if stringKey {
					if len(frames) == 0 || frames[len(frames)-1].state != diagnosticJSONObjectKeyOrEnd {
						return false, nil
					}
					frames[len(frames)-1].state = diagnosticJSONObjectColon
				} else if !completeValue() {
					return false, nil
				}
				continue
			}

			if literal != "" {
				if value != literal[literalIndex] {
					return false, nil
				}
				literalIndex++
				if literalIndex == len(literal) {
					literal = ""
					if !completeValue() {
						return false, nil
					}
				}
				continue
			}

			if numberState >= 0 {
				if value >= '0' && value <= '9' {
					switch numberState {
					case 0:
						if value == '0' {
							numberState = 1
						} else {
							numberState = 2
						}
					case 1:
						return false, nil
					case 2, 4, 6:
					case 3:
						numberState = 4
					case 5:
						numberState = 6
					}
					continue
				}
				if value == '.' && (numberState == 1 || numberState == 2) {
					numberState = 3
					continue
				}
				if (value == 'e' || value == 'E') && (numberState == 1 || numberState == 2 || numberState == 4) {
					numberState = 5
					continue
				}
				if (value == '+' || value == '-') && numberState == 5 {
					numberState = 6
					continue
				}
				if numberState != 1 && numberState != 2 && numberState != 4 && numberState != 6 {
					return false, nil
				}
				numberState = -1
				if !completeValue() {
					return false, nil
				}
				reprocess = true
				continue
			}

			if value == ' ' || value == '\n' || value == '\r' || value == '\t' {
				continue
			}
			if state == diagnosticJSONRootDone {
				return false, nil
			}
			if len(frames) > 0 {
				frame := &frames[len(frames)-1]
				switch frame.state {
				case diagnosticJSONObjectKeyOrEnd:
					if value == '}' {
						if !frame.allowEnd {
							return false, nil
						}
						frames = frames[:len(frames)-1]
						if !completeValue() {
							return false, nil
						}
						continue
					}
					if value != '"' {
						return false, nil
					}
					inString, stringKey = true, true
					continue
				case diagnosticJSONObjectColon:
					if value != ':' {
						return false, nil
					}
					frame.state = diagnosticJSONObjectValue
					continue
				case diagnosticJSONObjectCommaOrEnd:
					if value == ',' {
						frame.state = diagnosticJSONObjectKeyOrEnd
						frame.allowEnd = false
						continue
					}
					if value == '}' {
						frames = frames[:len(frames)-1]
						if !completeValue() {
							return false, nil
						}
						continue
					}
					return false, nil
				case diagnosticJSONArrayValueOrEnd:
					if value == ']' {
						if !frame.allowEnd {
							return false, nil
						}
						frames = frames[:len(frames)-1]
						if !completeValue() {
							return false, nil
						}
						continue
					}
				case diagnosticJSONArrayCommaOrEnd:
					if value == ',' {
						frame.state = diagnosticJSONArrayValueOrEnd
						frame.allowEnd = false
						continue
					}
					if value == ']' {
						frames = frames[:len(frames)-1]
						if !completeValue() {
							return false, nil
						}
						continue
					}
					return false, nil
				}
			}
			switch value {
			case '{':
				frames = append(frames, diagnosticJSONFrame{kind: diagnosticJSONObject, state: diagnosticJSONObjectKeyOrEnd, allowEnd: true})
			case '[':
				frames = append(frames, diagnosticJSONFrame{kind: diagnosticJSONArray, state: diagnosticJSONArrayValueOrEnd, allowEnd: true})
			case '"':
				inString, stringKey = true, false
			case 't':
				literal, literalIndex = "true", 1
			case 'f':
				literal, literalIndex = "false", 1
			case 'n':
				literal, literalIndex = "null", 1
			case '-':
				numberState = 0
			case '0':
				numberState = 1
			default:
				if value >= '1' && value <= '9' {
					numberState = 2
				} else {
					return false, nil
				}
			}
		}
	}
	if inString || escaped || unicodeDigits != 0 || literal != "" || len(frames) != 0 {
		return false, nil
	}
	if numberState >= 0 {
		if numberState != 1 && numberState != 2 && numberState != 4 && numberState != 6 {
			return false, nil
		}
		return completeValue(), nil
	}
	return seenValue && state == diagnosticJSONRootDone, nil
}

func init() {
	diagnosticCaptureTempJanitor.last = make(map[string]time.Time)
}

func logDiagnosticCaptureFailure(message string) {
	common.SysError(message)
}
