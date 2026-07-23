package service

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

type diagnosticResponseWriter struct {
	gin.ResponseWriter
	session     *diagnosticCaptureSession
	partID      string
	captureBody bool
	total       int64
	finished    bool
	writeFailed bool
	mu          sync.Mutex
}

func newDiagnosticResponseWriter(w gin.ResponseWriter, session *diagnosticCaptureSession, partID string, captureBody bool) *diagnosticResponseWriter {
	return &diagnosticResponseWriter{
		ResponseWriter: w,
		session:        session,
		partID:         partID,
		captureBody:    captureBody,
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
	w.session.endPart(w.partID, meta, w.total, !w.writeFailed)
}
