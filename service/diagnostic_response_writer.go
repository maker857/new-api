package service

import (
	"bytes"
	"net/http"

	"github.com/gin-gonic/gin"
)

type diagnosticResponseWriter struct {
	gin.ResponseWriter
	buf      bytes.Buffer
	maxBytes int64
	total    int64
}

func newDiagnosticResponseWriter(w gin.ResponseWriter, cfg DiagnosticCaptureConfig) *diagnosticResponseWriter {
	return &diagnosticResponseWriter{
		ResponseWriter: w,
		maxBytes:       cfg.MaxBodyBytes,
	}
}

func (w *diagnosticResponseWriter) Write(data []byte) (int, error) {
	w.capture(data)
	return w.ResponseWriter.Write(data)
}

func (w *diagnosticResponseWriter) WriteString(data string) (int, error) {
	w.capture([]byte(data))
	return w.ResponseWriter.WriteString(data)
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
	if len(data) == 0 {
		return
	}
	w.total += int64(len(data))
	remaining := w.maxBytes - int64(w.buf.Len())
	if remaining <= 0 {
		return
	}
	if int64(len(data)) > remaining {
		w.buf.Write(data[:remaining])
		return
	}
	w.buf.Write(data)
}

func (w *diagnosticResponseWriter) body() captureBody {
	return captureBody{
		Data:         w.buf.Bytes(),
		OriginalSize: w.total,
		SavedSize:    int64(w.buf.Len()),
		Truncated:    w.total > int64(w.buf.Len()),
	}
}
