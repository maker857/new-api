package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticCaptureRetains30MiBBacklog(t *testing.T) {
	for _, transport := range []string{"HTTP", "WebSocket"} {
		t.Run(transport, func(t *testing.T) {
			cfg := captureUsageFixture(t)
			diagnosticCaptureCleanupMu.Lock()
			defer diagnosticCaptureCleanupMu.Unlock()
			flow := &DiagnosticFlow{TraceID: "large-body", Channel: "channel", Started: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)}
			s := &diagnosticCaptureSession{cfg: cfg, events: make(chan diagnosticCaptureEvent, diagnosticCaptureEventBuffer), pendingWake: make(chan struct{}, 1), spoolDir: filepath.Join(cfg.TempDir, flow.TraceID)}
			payload := bytes.Repeat([]byte{'x'}, 30*1024*1024)
			expectedDigest := sha256.Sum256(payload)

			// Delay the disk consumer deliberately. The whole request/response
			// must queue without dropping bytes or making forwarding wait for it.
			if transport == "HTTP" {
				s.startPart("request", 1, "inbound", "request", nil)
				s.startPart("response", 2, "inbound", "response", nil)
				reader := newDiagnosticCaptureStream(bytes.NewReader(payload), s, "request", false)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				writer := newDiagnosticResponseWriter(c.Writer, s, flow, "response", true)
				forwarded, err := io.Copy(writer, reader)
				require.NoError(t, err)
				assert.Equal(t, int64(len(payload)), forwarded)
				assert.Equal(t, expectedDigest, sha256.Sum256(recorder.Body.Bytes()))
				writer.finish(nil)
			} else {
				s.writeWebSocketFrame(1, "wss://upstream.example/realtime", "request", payload)
				s.writeWebSocketFrame(2, "wss://upstream.example/realtime", "response", payload)
			}
			require.False(t, s.failed.Load(), "large queued bodies must not disable capture")
			// Forwarding buffers may be reused before asynchronous capture runs.
			payload[0] = 'y'
			s.close(flow)
			go s.dispatch()
			s.run()
			require.False(t, s.failed.Load(), "capture must finish after the disk consumer resumes")

			path := filepath.Join(cfg.CaptureDir, flow.Channel, "2026-10-10", flow.TraceID, "request-log.json")
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			var log diagnosticCombinedCPAJSON
			require.NoError(t, common.Unmarshal(data, &log))
			var bodies []diagnosticBodyJSON
			if transport == "HTTP" {
				require.NotNil(t, log.Request)
				require.NotNil(t, log.Response)
				bodies = []diagnosticBodyJSON{log.Request.Body, log.Response.Response.Body}
			} else {
				require.Len(t, log.APIRequests, 1)
				require.Len(t, log.APIResponses, 1)
				require.Len(t, log.APIRequests[0].WebSocketFrames, 1)
				require.Len(t, log.APIResponses[0].WebSocketFrames, 1)
				bodies = []diagnosticBodyJSON{log.APIRequests[0].WebSocketFrames[0].Body, log.APIResponses[0].WebSocketFrames[0].Body}
			}
			for _, body := range bodies {
				assert.False(t, body.Truncated)
				assert.Equal(t, int64(len(payload)), body.OriginalSize)
				assert.Equal(t, int64(len(payload)), body.SavedSize)
				digest := sha256.New()
				decoded, err := io.Copy(digest, base64.NewDecoder(base64.StdEncoding, strings.NewReader(body.Base64)))
				require.NoError(t, err)
				assert.Equal(t, int64(len(payload)), decoded)
				assert.Equal(t, expectedDigest[:], digest.Sum(nil), "saved body must match every forwarded byte")
			}
			assert.FileExists(t, filepath.Join(filepath.Dir(path), ".capture-complete"))
			actualBytes, err := diagnosticCaptureDirectorySize(cfg.CaptureDir)
			require.NoError(t, err)
			assert.Equal(t, actualBytes, diagnosticCaptureStorageState.totalBytes, "large captures must leave exact accounting after staging files are removed")
			assert.Zero(t, diagnosticCaptureStorageState.tempBytes)
		})
	}
}
