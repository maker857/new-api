package service

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type diagnosticResponseWriter struct {
	gin.ResponseWriter
	session     *diagnosticCaptureSession
	flow        *DiagnosticFlow
	partID      string
	captureBody bool
	started     time.Time
	firstWrite  time.Time
	total       int64
	finished    bool
	writeFailed bool
	mu          sync.Mutex
}

func newDiagnosticResponseWriter(w gin.ResponseWriter, session *diagnosticCaptureSession, flow *DiagnosticFlow, partID string, captureBody bool) *diagnosticResponseWriter {
	return &diagnosticResponseWriter{
		ResponseWriter: w,
		session:        session,
		flow:           flow,
		partID:         partID,
		captureBody:    captureBody,
		started:        time.Now(),
	}
}

func (w *diagnosticResponseWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if n > 0 {
		w.capture(data[:n])
	}
	if err != nil {
		w.mu.Lock()
		w.writeFailed = true
		w.mu.Unlock()
	}
	return n, err
}

func (w *diagnosticResponseWriter) WriteString(data string) (int, error) {
	n, err := w.ResponseWriter.WriteString(data)
	if n > 0 {
		w.capture([]byte(data[:n]))
	}
	if err != nil {
		w.mu.Lock()
		w.writeFailed = true
		w.mu.Unlock()
	}
	return n, err
}

func (w *diagnosticResponseWriter) WriteHeaderNow() {
	w.ResponseWriter.WriteHeaderNow()
}

func (w *diagnosticResponseWriter) WriteHeader(code int) {
	w.ResponseWriter.WriteHeader(code)
}

func (w *diagnosticResponseWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

func (w *diagnosticResponseWriter) capture(data []byte) {
	if !w.captureBody || len(data) == 0 {
		return
	}
	if w.firstWrite.IsZero() {
		w.firstWrite = time.Now()
	}
	w.total += int64(len(data))
	w.session.writeChunk(w.partID, data)
}

func (w *diagnosticResponseWriter) finish(meta map[string]any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.finished {
		return
	}
	w.finished = true
	isStream := w.flow != nil && w.flow.Context.StreamStatus != ""
	if isStream && !w.firstWrite.IsZero() && !w.started.IsZero() {
		w.flow.Context.FirstResponseMS = w.firstWrite.Sub(w.started).Milliseconds()
	}
	if isStream && !w.firstWrite.IsZero() && !w.started.IsZero() {
		meta["first_response_ms"] = w.firstWrite.Sub(w.started).Milliseconds()
	}
	w.session.endPart(w.partID, meta, w.total, !w.writeFailed)
}
