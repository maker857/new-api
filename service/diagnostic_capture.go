package service

import (
	"bytes"
	"container/heap"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

const defaultDiagnosticCapturePaths = "/v1/*,/v1beta/*,/pg/*,/mj/*,*/mj/*,/suno/*,/kling/v1/*,/jimeng/*"

const (
	DiagnosticCaptureEnabledKey                  = "DiagnosticCaptureEnabled"
	DiagnosticCaptureModeKey                     = "DiagnosticCaptureMode"
	DiagnosticCaptureDirKey                      = "DiagnosticCaptureDir"
	DiagnosticCaptureTempDirKey                  = "DiagnosticCaptureTempDir"
	DiagnosticCaptureTempRetentionMinutesKey     = "DiagnosticCaptureTempRetentionMinutes"
	DiagnosticCaptureMaxBodyMBKey                = "DiagnosticCaptureMaxBodyMB"
	DiagnosticCaptureAutoCleanupEnabledKey       = "DiagnosticCaptureAutoCleanupEnabled"
	DiagnosticCaptureMaxStorageBytesKey          = "DiagnosticCaptureMaxStorageBytes"
	DiagnosticCaptureCleanupPercentKey           = "DiagnosticCaptureCleanupPercent"
	DiagnosticCaptureCleanupRateMBKey            = "DiagnosticCaptureCleanupRateMB"
	DiagnosticCaptureMinRetentionMinutesKey      = "DiagnosticCaptureMinRetentionMinutes"
	DiagnosticCaptureIncompleteTimeoutMinutesKey = "DiagnosticCaptureIncompleteTimeoutMinutes"
	DiagnosticCaptureMinRetentionHoursKey        = "DiagnosticCaptureMinRetentionHours"
	DiagnosticCaptureIncompleteTimeoutHoursKey   = "DiagnosticCaptureIncompleteTimeoutHours"
	DiagnosticCapturePathsKey                    = "DiagnosticCapturePaths"

	DiagnosticCaptureLastCleanupAtKey           = "DiagnosticCaptureLastCleanupAt"
	DiagnosticCaptureLastCleanupCutoffKey       = "DiagnosticCaptureLastCleanupCutoff"
	DiagnosticCaptureLastCleanupDeletedCountKey = "DiagnosticCaptureLastCleanupDeletedCount"
	DiagnosticCaptureLastCleanupFreedBytesKey   = "DiagnosticCaptureLastCleanupFreedBytes"
	DiagnosticCaptureLastCleanupStatusKey       = "DiagnosticCaptureLastCleanupStatus"
	DiagnosticCaptureNextCleanupEligibleAtKey   = "DiagnosticCaptureNextCleanupEligibleAt"
	DiagnosticCaptureLastTempCleanupAtKey       = "DiagnosticCaptureLastTempCleanupAt"
	DiagnosticCaptureLastTempDeletedCountKey    = "DiagnosticCaptureLastTempDeletedCount"
	DiagnosticCaptureLastTempFreedBytesKey      = "DiagnosticCaptureLastTempFreedBytes"

	DiagnosticTraceHeader   = "X-Diagnostic-Trace-Id"
	DiagnosticChannelHeader = "X-Diagnostic-Channel"
)

type DiagnosticCaptureConfig struct {
	Enabled                   bool
	Mode                      string
	CaptureDir                string
	MaxBodyBytes              int64
	AutoCleanupEnabled        bool
	MaxStorageBytes           int64
	CleanupPercent            int64
	CleanupRateBytesPerSecond int64
	MinRetentionMinutes       int64
	IncompleteTimeoutMinutes  int64
	TempDir                   string
	FailureDir                string
	TempRetentionMinutes      int64
	NextCleanupEligibleAt     int64
	PathRules                 []string
}

type DiagnosticFlow struct {
	TraceID      string
	ProxyTraceID string
	Channel      string
	Started      time.Time
	session      *diagnosticCaptureSession
	writer       *diagnosticResponseWriter
}

type captureBody struct {
	Data         []byte `json:"-"`
	OriginalSize int64  `json:"body_original_size,omitempty"`
	SavedSize    int64  `json:"body_saved_size,omitempty"`
	Truncated    bool   `json:"body_truncated,omitempty"`
}

type diagnosticCPAJSON struct {
	Format      string                     `json:"format"`
	Version     int                        `json:"version"`
	CapturedAt  string                     `json:"captured_at"`
	Direction   string                     `json:"direction"`
	Section     string                     `json:"section"`
	Meta        map[string]any             `json:"meta,omitempty"`
	RequestInfo *diagnosticRequestInfoJSON `json:"request_info,omitempty"`
	Headers     map[string][]string        `json:"headers,omitempty"`
	RequestBody *diagnosticBodyJSON        `json:"request_body,omitempty"`
	APIRequest  *diagnosticAPIRequestJSON  `json:"api_request,omitempty"`
	APIResponse *diagnosticAPIResponseJSON `json:"api_response,omitempty"`
	Response    *diagnosticResponseJSON    `json:"response,omitempty"`
}

type diagnosticCombinedCPAJSON struct {
	Format          string                      `json:"format"`
	Version         int                         `json:"version"`
	ProxyTraceID    string                      `json:"proxy_trace_id,omitempty"`
	NewAPIRequestID string                      `json:"newapi_request_id,omitempty"`
	RequestInfo     *diagnosticRequestInfoJSON  `json:"request_info,omitempty"`
	Headers         map[string][]string         `json:"headers,omitempty"`
	RequestBody     *diagnosticBodyJSON         `json:"request_body,omitempty"`
	APIRequests     []diagnosticAPIRequestJSON  `json:"api_requests,omitempty"`
	APIResponses    []diagnosticAPIResponseJSON `json:"api_responses,omitempty"`
	Response        *diagnosticResponseJSON     `json:"response,omitempty"`
}

type diagnosticRequestInfoJSON struct {
	AppVersion          string `json:"version"`
	URL                 string `json:"url"`
	Method              string `json:"method"`
	DownstreamTransport string `json:"downstream_transport,omitempty"`
	UpstreamTransport   string `json:"upstream_transport,omitempty"`
	Timestamp           string `json:"timestamp"`
	RemoteAddr          string `json:"remote_addr,omitempty"`
}

type diagnosticAPIRequestJSON struct {
	Sequence        int64                          `json:"sequence"`
	Timestamp       string                         `json:"timestamp"`
	UpstreamURL     string                         `json:"upstream_url"`
	HTTPMethod      string                         `json:"http_method"`
	Headers         map[string][]string            `json:"headers,omitempty"`
	Body            diagnosticBodyJSON             `json:"body"`
	WebSocketFrames []diagnosticWebSocketFrameJSON `json:"websocket_frames,omitempty"`
}

type diagnosticAPIResponseJSON struct {
	Sequence        int64                          `json:"sequence"`
	Timestamp       string                         `json:"timestamp"`
	Status          int                            `json:"status,omitempty"`
	Headers         map[string][]string            `json:"headers,omitempty"`
	Body            diagnosticBodyJSON             `json:"body"`
	Error           string                         `json:"error,omitempty"`
	WebSocketFrames []diagnosticWebSocketFrameJSON `json:"websocket_frames,omitempty"`
}

type diagnosticWebSocketFrameJSON struct {
	Sequence  int64              `json:"sequence"`
	Timestamp string             `json:"timestamp"`
	Body      diagnosticBodyJSON `json:"body"`
}

type diagnosticResponseJSON struct {
	Status     int                 `json:"status,omitempty"`
	DurationMS int64               `json:"duration_ms,omitempty"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Body       diagnosticBodyJSON  `json:"body"`
}

type diagnosticBodyJSON struct {
	Mode         string `json:"mode"`
	Encoding     string `json:"encoding"`
	OriginalSize int64  `json:"original_size"`
	SavedSize    int64  `json:"saved_size"`
	Truncated    bool   `json:"truncated"`
	Text         string `json:"text,omitempty"`
	JSON         any    `json:"json,omitempty"`
	Base64       string `json:"base64,omitempty"`
}

type captureReadCloser struct {
	io.ReadCloser
	buf      bytes.Buffer
	maxBytes int64
	onClose  func([]byte, int64, bool)
	total    int64
	closed   bool
}

type DiagnosticExchange struct {
	Flow      *DiagnosticFlow
	Sequence  int64
	Started   time.Time
	ChannelID int
}

var diagnosticSequence = struct {
	sync.Mutex
	value int64
}{}

var diagnosticCaptureWriteMu sync.Mutex

var diagnosticActiveCaptures = struct {
	sync.RWMutex
	traceIDs map[string]int
}{traceIDs: make(map[string]int)}

var diagnosticCaptureStorageState struct {
	sync.Mutex
	captureDir     string
	tempDir        string
	failureDir     string
	totalBytes     int64
	tempBytes      int64
	initialized    bool
	lastAttempt    time.Time
	nextEligibleAt time.Time
	lastReconciled time.Time
	generation     uint64
}

var diagnosticCaptureCleanupMu sync.Mutex

const (
	maxDiagnosticCaptureStorageBytes   = int64(10) << 40
	maxDiagnosticCaptureRetentionHours = 24 * 365 * 10
	diagnosticCaptureCleanupBatchSize  = 500
	diagnosticCaptureDirectoryReadSize = 500
	diagnosticCaptureCleanupSelectSize = 500
)

func DefaultDiagnosticCaptureOptions() map[string]string {
	return map[string]string{
		DiagnosticCaptureEnabledKey:                  "false",
		DiagnosticCaptureModeKey:                     "full",
		DiagnosticCaptureDirKey:                      "captures",
		DiagnosticCaptureTempDirKey:                  "diagnostic-capture-temp",
		DiagnosticCaptureTempRetentionMinutesKey:     "60",
		DiagnosticCaptureMaxBodyMBKey:                "10",
		DiagnosticCaptureAutoCleanupEnabledKey:       "false",
		DiagnosticCaptureMaxStorageBytesKey:          "0",
		DiagnosticCaptureCleanupPercentKey:           "0",
		DiagnosticCaptureCleanupRateMBKey:            "0",
		DiagnosticCaptureMinRetentionMinutesKey:      "0",
		DiagnosticCaptureIncompleteTimeoutMinutesKey: "1440",
		DiagnosticCaptureMinRetentionHoursKey:        "0",
		DiagnosticCaptureIncompleteTimeoutHoursKey:   "24",
		DiagnosticCapturePathsKey:                    defaultDiagnosticCapturePaths,
		DiagnosticCaptureNextCleanupEligibleAtKey:    "0",
	}
}

func DiagnosticCaptureConfigFromOptions() DiagnosticCaptureConfig {
	options := DefaultDiagnosticCaptureOptions()
	common.OptionMapRWMutex.RLock()
	for key := range options {
		if value, ok := common.OptionMap[key]; ok {
			options[key] = value
		}
	}
	common.OptionMapRWMutex.RUnlock()

	maxBodyMB, _ := strconv.ParseInt(strings.TrimSpace(options[DiagnosticCaptureMaxBodyMBKey]), 10, 64)
	if maxBodyMB <= 0 {
		maxBodyMB = 10
	}
	maxStorageBytes, _ := strconv.ParseInt(strings.TrimSpace(options[DiagnosticCaptureMaxStorageBytesKey]), 10, 64)
	if maxStorageBytes < 0 || maxStorageBytes > maxDiagnosticCaptureStorageBytes {
		maxStorageBytes = 0
	}
	cleanupPercent, _ := strconv.ParseInt(strings.TrimSpace(options[DiagnosticCaptureCleanupPercentKey]), 10, 64)
	if cleanupPercent < 0 || cleanupPercent > 90 {
		cleanupPercent = 0
	}
	cleanupRateMB, _ := strconv.ParseInt(strings.TrimSpace(options[DiagnosticCaptureCleanupRateMBKey]), 10, 64)
	if cleanupRateMB < 0 || cleanupRateMB > 10240 {
		cleanupRateMB = 0
	}
	minRetentionMinutes, _ := strconv.ParseInt(strings.TrimSpace(options[DiagnosticCaptureMinRetentionMinutesKey]), 10, 64)
	if minRetentionMinutes < 0 || minRetentionMinutes > maxDiagnosticCaptureRetentionHours*60 {
		minRetentionMinutes = 0
	}
	incompleteTimeoutMinutes, _ := strconv.ParseInt(strings.TrimSpace(options[DiagnosticCaptureIncompleteTimeoutMinutesKey]), 10, 64)
	if incompleteTimeoutMinutes < 0 || incompleteTimeoutMinutes > maxDiagnosticCaptureRetentionHours*60 {
		incompleteTimeoutMinutes = 24 * 60
	}
	// Older installations persist these two settings in hours. Prefer their
	// non-default values until the minute-based setting is saved from the UI.
	legacyMinRetentionHours, _ := strconv.ParseInt(strings.TrimSpace(options[DiagnosticCaptureMinRetentionHoursKey]), 10, 64)
	if minRetentionMinutes == 0 && legacyMinRetentionHours > 0 && legacyMinRetentionHours <= maxDiagnosticCaptureRetentionHours {
		minRetentionMinutes = legacyMinRetentionHours * 60
	}
	legacyIncompleteTimeoutHours, _ := strconv.ParseInt(strings.TrimSpace(options[DiagnosticCaptureIncompleteTimeoutHoursKey]), 10, 64)
	if incompleteTimeoutMinutes == 24*60 && legacyIncompleteTimeoutHours >= 0 && legacyIncompleteTimeoutHours <= maxDiagnosticCaptureRetentionHours && legacyIncompleteTimeoutHours != 24 {
		incompleteTimeoutMinutes = legacyIncompleteTimeoutHours * 60
	}
	tempRetentionMinutes, _ := strconv.ParseInt(strings.TrimSpace(options[DiagnosticCaptureTempRetentionMinutesKey]), 10, 64)
	if tempRetentionMinutes < 1 || tempRetentionMinutes > maxDiagnosticCaptureRetentionHours*60 {
		tempRetentionMinutes = 60
	}
	mode := strings.ToLower(strings.TrimSpace(options[DiagnosticCaptureModeKey]))
	if mode != "metadata" && mode != "full" {
		mode = "full"
	}
	return DiagnosticCaptureConfig{
		Enabled:                   options[DiagnosticCaptureEnabledKey] == "true",
		Mode:                      mode,
		CaptureDir:                strings.TrimSpace(options[DiagnosticCaptureDirKey]),
		TempDir:                   strings.TrimSpace(options[DiagnosticCaptureTempDirKey]),
		FailureDir:                diagnosticCaptureFailureDir(strings.TrimSpace(options[DiagnosticCaptureDirKey])),
		TempRetentionMinutes:      tempRetentionMinutes,
		NextCleanupEligibleAt:     diagnosticCaptureOptionInt64(DiagnosticCaptureNextCleanupEligibleAtKey),
		MaxBodyBytes:              maxBodyMB * 1024 * 1024,
		AutoCleanupEnabled:        options[DiagnosticCaptureAutoCleanupEnabledKey] == "true",
		MaxStorageBytes:           maxStorageBytes,
		CleanupPercent:            cleanupPercent,
		CleanupRateBytesPerSecond: cleanupRateMB * 1024 * 1024,
		MinRetentionMinutes:       minRetentionMinutes,
		IncompleteTimeoutMinutes:  incompleteTimeoutMinutes,
		PathRules:                 parseDiagnosticPathRules(options[DiagnosticCapturePathsKey]),
	}
}

func DiagnosticCaptureModeValid(mode string) bool {
	mode = strings.ToLower(strings.TrimSpace(mode))
	return mode == "metadata" || mode == "full"
}

func DiagnosticCaptureMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		flow, _ := c.Get("diagnostic_flow")
		diagnosticFlow, ok := flow.(*DiagnosticFlow)
		if !ok || diagnosticFlow == nil || diagnosticFlow.session == nil || diagnosticFlow.writer == nil {
			return
		}
		diagnosticFlow.writer.finish(map[string]any{
			"captured_at":  time.Now().UTC().Format(time.RFC3339Nano),
			"role":         "inbound",
			"status_code":  c.Writer.Status(),
			"duration_ms":  time.Since(diagnosticFlow.Started).Milliseconds(),
			"headers":      redactHeaders(c.Writer.Header()),
			"body_capture": diagnosticFlow.session.cfg.Mode,
		})
		diagnosticFlow.session.close(diagnosticFlow)
	}
}

// StartDiagnosticCapture starts capture only after a concrete channel has been
// selected. This is deliberately later than the global middleware: a disabled
// channel must not cause any request/response byte copying or spool I/O.
func StartDiagnosticCapture(c *gin.Context) {
	if c == nil {
		return
	}
	channelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	channelName := common.GetContextKeyString(c, constant.ContextKeyChannelName)
	startDiagnosticCaptureForChannel(c, channelID, channelName)
}

// StartDiagnosticCaptureForChannel starts a capture for a request path that
// resolves its channel outside the normal distributor flow, such as a task
// status fetch. It honors the same global and per-channel switches.
func StartDiagnosticCaptureForChannel(c *gin.Context, channelID int, channelName string) {
	startDiagnosticCaptureForChannel(c, channelID, channelName)
}

func startDiagnosticCaptureForChannel(c *gin.Context, channelID int, channelName string) {
	if c == nil || c.Request == nil {
		return
	}
	cfg := DiagnosticCaptureConfigFromOptions()
	if !cfg.Enabled || !cfg.shouldCapturePath(c.Request.URL.Path) || !diagnosticCaptureChannelEnabled(c, channelID) {
		return
	}
	if existing, ok := c.Get("diagnostic_flow"); ok {
		if flow, ok := existing.(*DiagnosticFlow); ok && flow != nil && flow.session != nil {
			return
		}
	}
	flow := &DiagnosticFlow{
		TraceID:      diagnosticTraceIDFromContext(c),
		ProxyTraceID: strings.TrimSpace(c.GetHeader(DiagnosticTraceHeader)),
		Channel:      safeCaptureName(channelName, "unknown"),
		Started:      time.Now(),
	}
	flow.session = newDiagnosticCaptureSession(cfg, flow)
	if flow.session == nil {
		return
	}
	c.Set("diagnostic_flow", flow)
	sequence := nextDiagnosticSequence()
	flow.session.startPart("inbound-request", sequence, "inbound", "request", map[string]any{
		"captured_at": time.Now().UTC().Format(time.RFC3339Nano),
		"role":        "inbound",
		"method":      c.Request.Method,
		"protocol":    c.Request.Proto,
		"path":        c.Request.URL.RequestURI(),
		"remote_addr": c.ClientIP(),
		"headers":     redactHeaders(c.Request.Header),
	})
	if cfg.Mode == "full" {
		if storage, err := common.GetBodyStorage(c); err == nil {
			reader, readerErr := common.OpenBodyStorageReader(storage)
			if readerErr != nil {
				flow.session.endPart("inbound-request", nil, 0, false)
			} else {
				flow.session.producers.Add(1)
				go func() {
					defer flow.session.producers.Done()
					defer reader.Close()
					buffer := make([]byte, diagnosticCaptureChunkSize)
					var total int64
					complete := true
					for {
						n, readErr := reader.Read(buffer)
						if n > 0 {
							flow.session.writeChunk("inbound-request", buffer[:n])
							total += int64(n)
						}
						if readErr == io.EOF {
							break
						}
						if readErr != nil {
							complete = false
							break
						}
					}
					flow.session.endPart("inbound-request", nil, total, complete)
				}()
			}
		} else {
			flow.session.endPart("inbound-request", nil, 0, false)
		}
	} else {
		flow.session.endPart("inbound-request", nil, 0, true)
	}
	flow.session.startPart("inbound-response", sequence, "inbound", "response", nil)
	flow.writer = newDiagnosticResponseWriter(c.Writer, flow.session, "inbound-response", cfg.Mode == "full")
	c.Writer = flow.writer
}

func PrepareDiagnosticOutboundRequest(c *gin.Context, info *relaycommon.RelayInfo, method, url string, headers http.Header, body io.Reader) (io.Reader, *DiagnosticExchange) {
	if c == nil || c.Request == nil {
		return body, nil
	}
	flowValue, _ := c.Get("diagnostic_flow")
	flow, _ := flowValue.(*DiagnosticFlow)
	if flow == nil || flow.session == nil {
		cfg := DiagnosticCaptureConfigFromOptions()
		if !cfg.Enabled || !cfg.shouldCapturePath(c.Request.URL.Path) {
			return body, nil
		}
		channelID := 0
		if info != nil && info.ChannelMeta != nil {
			channelID = info.ChannelMeta.ChannelId
		}
		if !diagnosticCaptureChannelEnabled(c, channelID) {
			return body, nil
		}
		channelName := ""
		if info != nil && info.ChannelMeta != nil {
			channelName = info.ChannelMeta.ChannelName
		}
		StartDiagnosticCaptureForChannel(c, channelID, channelName)
		flow = getOrCreateDiagnosticFlow(c)
		if flow.session == nil {
			return body, nil
		}
	}
	channelID := 0
	if info != nil && info.ChannelMeta != nil {
		channelID = info.ChannelMeta.ChannelId
	}
	channel := ""
	if info != nil && info.ChannelMeta != nil {
		channel = info.ChannelMeta.ChannelName
	}
	if channel == "" {
		channel = c.GetString("channel_name")
	}
	if channel != "" {
		flow.Channel = safeCaptureName(channel, "unknown")
	}
	sequence := nextDiagnosticSequence()
	partID := fmt.Sprintf("outbound-%06d-request", sequence)
	flow.session.startPart(partID, sequence, "outbound", "request", map[string]any{
		"captured_at": time.Now().UTC().Format(time.RFC3339Nano),
		"role":        "outbound",
		"method":      method,
		"url":         url,
		"headers":     redactHeaders(headers),
	})
	if flow.session.cfg.Mode == "full" && body != nil {
		body = newDiagnosticCaptureStream(body, flow.session, partID)
	} else {
		flow.session.endPart(partID, nil, 0, true)
	}
	return body, &DiagnosticExchange{Flow: flow, Sequence: sequence, Started: time.Now(), ChannelID: channelID}
}

// PrepareDiagnosticHTTPOutboundRequest applies the same sidecar capture to a
// channel's auxiliary HTTP request (uploads, polling, and similar calls) as
// it does to its primary relay request. It only wraps the body when capture is
// enabled for the already-selected channel, so it never changes the request
// bytes or makes the request wait for diagnostic I/O.
func PrepareDiagnosticHTTPOutboundRequest(c *gin.Context, info *relaycommon.RelayInfo, req *http.Request) *DiagnosticExchange {
	if req == nil {
		return nil
	}
	body, exchange := PrepareDiagnosticOutboundRequest(c, info, req.Method, req.URL.String(), req.Header, req.Body)
	if body == nil {
		return exchange
	}
	if closer, ok := body.(io.ReadCloser); ok {
		req.Body = closer
	} else {
		req.Body = io.NopCloser(body)
	}
	return exchange
}

func WrapDiagnosticOutboundResponse(c *gin.Context, resp *http.Response, exchange *DiagnosticExchange) {
	if resp == nil || exchange == nil || exchange.Flow == nil {
		return
	}
	flow := exchange.Flow
	if flow.session == nil {
		return
	}
	partID := fmt.Sprintf("outbound-%06d-response", exchange.Sequence)
	flow.session.startPart(partID, exchange.Sequence, "outbound", "response", map[string]any{
		"captured_at": time.Now().UTC().Format(time.RFC3339Nano),
		"role":        "outbound",
		"status_code": resp.StatusCode,
		"duration_ms": time.Since(exchange.Started).Milliseconds(),
		"headers":     redactHeaders(resp.Header),
	})
	if flow.session.cfg.Mode != "full" || resp.Body == nil {
		flow.session.endPart(partID, nil, 0, true)
		return
	}
	resp.Body = newDiagnosticCaptureStream(resp.Body, flow.session, partID)
}

// WrapDiagnosticOutboundNDJSONResponse captures newline-delimited response
// records after redacting configured secrets while leaving the response bytes
// consumed by the relay unchanged.
func WrapDiagnosticOutboundNDJSONResponse(resp *http.Response, exchange *DiagnosticExchange, maxLineSize int, secrets ...string) {
	if resp == nil || exchange == nil || exchange.Flow == nil || exchange.Flow.session == nil {
		return
	}
	flow := exchange.Flow
	partID := fmt.Sprintf("outbound-%06d-response", exchange.Sequence)
	flow.session.startPart(partID, exchange.Sequence, "outbound", "response", map[string]any{
		"captured_at": time.Now().UTC().Format(time.RFC3339Nano),
		"role":        "outbound",
		"status_code": resp.StatusCode,
		"duration_ms": time.Since(exchange.Started).Milliseconds(),
		"headers":     redactHeaders(resp.Header),
	})
	if flow.session.cfg.Mode != "full" || resp.Body == nil {
		flow.session.endPart(partID, nil, 0, true)
		return
	}
	resp.Body = newDiagnosticNDJSONCaptureStream(resp.Body, flow.session, partID, maxLineSize, secrets...)
}

// RecordDiagnosticOutboundFailure records transport failures for an exchange
// that never produced an HTTP response, such as DNS, TLS, or connection errors.
func RecordDiagnosticOutboundFailure(exchange *DiagnosticExchange, requestErr error) {
	if exchange == nil || exchange.Flow == nil || exchange.Flow.session == nil || requestErr == nil {
		return
	}
	flow := exchange.Flow
	partID := fmt.Sprintf("outbound-%06d-response", exchange.Sequence)
	flow.session.startPart(partID, exchange.Sequence, "outbound", "response", map[string]any{
		"captured_at": time.Now().UTC().Format(time.RFC3339Nano),
		"role":        "outbound",
		"duration_ms": time.Since(exchange.Started).Milliseconds(),
		"error":       requestErr.Error(),
	})
	flow.session.endPart(partID, nil, 0, true)
}

// RecordDiagnosticOutboundResponseMetadata completes an exchange that has a
// protocol-level response but no HTTP body to read, such as a WebSocket 101
// handshake.
func RecordDiagnosticOutboundResponseMetadata(exchange *DiagnosticExchange, status int, headers http.Header) {
	if exchange == nil || exchange.Flow == nil || exchange.Flow.session == nil {
		return
	}
	flow := exchange.Flow
	partID := fmt.Sprintf("outbound-%06d-response", exchange.Sequence)
	flow.session.startPart(partID, exchange.Sequence, "outbound", "response", map[string]any{
		"captured_at": time.Now().UTC().Format(time.RFC3339Nano),
		"role":        "outbound",
		"status_code": status,
		"duration_ms": time.Since(exchange.Started).Milliseconds(),
		"headers":     redactHeaders(headers),
	})
	flow.session.endPart(partID, nil, 0, true)
}

// RecordDiagnosticWebSocketFrame stores one already-read WebSocket message as
// an auxiliary upstream exchange. The caller records only byte slices it has
// already received or is about to send, keeping this sidecar operation out of
// the WebSocket forwarding path.
func RecordDiagnosticWebSocketFrame(c *gin.Context, upstreamURL, direction string, payload []byte) {
	if c == nil {
		return
	}
	flowValue, _ := c.Get("diagnostic_flow")
	flow, _ := flowValue.(*DiagnosticFlow)
	if flow == nil || flow.session == nil {
		return
	}
	part := "response"
	if direction == "request" {
		part = "request"
	}
	sequence := nextDiagnosticSequence()
	flow.session.writeWebSocketFrame(sequence, upstreamURL, part, payload)
}

// RecordDiagnosticWebSocketFailure stores a transport error that happened
// after a WebSocket connection was established. It is intentionally sidecar
// only: callers record the original error before returning it unchanged.
func RecordDiagnosticWebSocketFailure(c *gin.Context, upstreamURL string, transportErr error) {
	if c == nil || transportErr == nil {
		return
	}
	flowValue, _ := c.Get("diagnostic_flow")
	flow, _ := flowValue.(*DiagnosticFlow)
	if flow == nil || flow.session == nil {
		return
	}
	sequence := nextDiagnosticSequence()
	partID := fmt.Sprintf("outbound-%06d-response", sequence)
	flow.session.startPart(partID, sequence, "outbound", "response", map[string]any{
		"captured_at":  time.Now().UTC().Format(time.RFC3339Nano),
		"role":         "outbound",
		"transport":    "websocket",
		"upstream_url": upstreamURL,
		"error":        transportErr.Error(),
	})
	flow.session.endPart(partID, nil, 0, true)
}

func getOrCreateDiagnosticFlow(c *gin.Context) *DiagnosticFlow {
	if value, ok := c.Get("diagnostic_flow"); ok {
		if flow, ok := value.(*DiagnosticFlow); ok && flow != nil {
			return flow
		}
	}
	flow := &DiagnosticFlow{
		TraceID: diagnosticTraceIDFromContext(c),
		Channel: "unknown",
		Started: time.Now(),
	}
	c.Set("diagnostic_flow", flow)
	return flow
}

func diagnosticCaptureChannelEnabled(c *gin.Context, fallbackChannelID int) bool {
	channelID := fallbackChannelID
	if c != nil {
		if contextChannelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId); contextChannelID > 0 {
			channelID = contextChannelID
		}
	}
	if channelID <= 0 {
		return false
	}
	channelInfo, err := model.CacheGetChannelInfo(channelID)
	if err != nil || channelInfo == nil {
		return false
	}
	return channelInfo.IsDiagnosticCaptureEnabled()
}

func ensureDiagnosticTraceID(req *http.Request) string {
	if req != nil {
		if traceID := strings.TrimSpace(req.Header.Get(common.RequestIdKey)); traceID != "" {
			return safeTraceID(traceID)
		}
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func diagnosticTraceIDFromContext(c *gin.Context) string {
	if c != nil {
		if traceID := strings.TrimSpace(c.GetString(common.RequestIdKey)); traceID != "" {
			return safeTraceID(traceID)
		}
	}
	if c != nil {
		return ensureDiagnosticTraceID(c.Request)
	}
	return ensureDiagnosticTraceID(nil)
}

func nextDiagnosticSequence() int64 {
	diagnosticSequence.Lock()
	defer diagnosticSequence.Unlock()
	diagnosticSequence.value++
	return diagnosticSequence.value
}

func writeCapture(cfg DiagnosticCaptureConfig, flow *DiagnosticFlow, sequence int64, role, part string, meta map[string]any, body captureBody) {
	if flow == nil || !cfg.Enabled {
		return
	}
	channel := safeCaptureName(flow.Channel, "unknown")
	traceID := safeTraceID(flow.TraceID)
	day := flow.Started.Format("2006-01-02")
	base := filepath.Join(cfg.CaptureDir, channel, day, traceID)
	if err := os.MkdirAll(base, 0o755); err != nil {
		common.SysError("failed to create diagnostic capture dir: " + err.Error())
		return
	}
	ensureDiagnosticCaptureTimestamp(base)
	content := buildDiagnosticCPAJSON(cfg, flow, sequence, role, part, meta, body)
	writeCombinedCapture(filepath.Join(base, "request-log.json"), flow, content, cfg)
}

func ensureDiagnosticCaptureTimestamp(dir string) {
	file, err := os.OpenFile(filepath.Join(dir, ".capture-created-at"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if !os.IsExist(err) {
			common.SysError("failed to create diagnostic capture timestamp: " + err.Error())
		}
		return
	}
	_, _ = file.WriteString(strconv.FormatInt(time.Now().UnixNano(), 10))
	_ = file.Close()
}

func markDiagnosticCaptureComplete(cfg DiagnosticCaptureConfig, flow *DiagnosticFlow) {
	if flow == nil {
		return
	}
	channel := safeCaptureName(flow.Channel, "unknown")
	traceID := safeTraceID(flow.TraceID)
	day := flow.Started.Format("2006-01-02")
	dir := filepath.Join(cfg.CaptureDir, channel, day, traceID)
	if _, err := os.Stat(filepath.Join(dir, "request-log.json")); err != nil {
		return
	}
	if err := os.WriteFile(
		filepath.Join(dir, ".capture-complete"),
		[]byte(strconv.FormatInt(time.Now().UnixNano(), 10)),
		0o600,
	); err != nil {
		common.SysError("failed to mark diagnostic capture complete: " + err.Error())
	}
}

func diagnosticCaptureComplete(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".capture-complete"))
	return err == nil && !info.IsDir()
}

func markDiagnosticCaptureActive(traceID string) {
	traceID = safeTraceID(traceID)
	diagnosticActiveCaptures.Lock()
	diagnosticActiveCaptures.traceIDs[traceID]++
	diagnosticActiveCaptures.Unlock()
}

func markDiagnosticCaptureInactive(traceID string) {
	traceID = safeTraceID(traceID)
	diagnosticActiveCaptures.Lock()
	if diagnosticActiveCaptures.traceIDs[traceID] <= 1 {
		delete(diagnosticActiveCaptures.traceIDs, traceID)
	} else {
		diagnosticActiveCaptures.traceIDs[traceID]--
	}
	diagnosticActiveCaptures.Unlock()
}

func isDiagnosticCaptureActive(traceID string) bool {
	traceID = safeTraceID(traceID)
	diagnosticActiveCaptures.RLock()
	active := diagnosticActiveCaptures.traceIDs[traceID] > 0
	diagnosticActiveCaptures.RUnlock()
	return active
}

func writeCombinedCapture(path string, flow *DiagnosticFlow, content diagnosticCPAJSON, cfg DiagnosticCaptureConfig) {
	diagnosticCaptureWriteMu.Lock()
	defer diagnosticCaptureWriteMu.Unlock()

	combined := diagnosticCombinedCPAJSON{
		Format:  "cpa-sections-json",
		Version: 1,
	}
	var previousSize int64
	if data, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(data)) > 0 {
		previousSize = int64(len(data))
		_ = json.Unmarshal(data, &combined)
	}
	if combined.Format == "" {
		combined.Format = "cpa-sections-json"
	}
	if combined.Version == 0 {
		combined.Version = 1
	}
	if flow != nil {
		if combined.NewAPIRequestID == "" {
			combined.NewAPIRequestID = flow.TraceID
		}
	}
	switch {
	case content.RequestInfo != nil:
		combined.RequestInfo = content.RequestInfo
		combined.Headers = content.Headers
		combined.RequestBody = content.RequestBody
	case content.APIRequest != nil:
		upsertAPIRequest(&combined, *content.APIRequest)
	case content.APIResponse != nil:
		upsertAPIResponse(&combined, *content.APIResponse)
	case content.Response != nil:
		combined.Response = content.Response
	}

	data, err := json.MarshalIndent(combined, "", "  ")
	if err != nil {
		common.SysError("failed to encode diagnostic capture: " + err.Error())
		return
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		common.SysError("failed to write diagnostic capture: " + err.Error())
		return
	}
	enforceDiagnosticCaptureStorage(cfg, path, previousSize, int64(len(data)))
}

type diagnosticCaptureCandidate struct {
	dir        string
	traceID    string
	size       int64
	capturedAt time.Time
	complete   bool
}

type DiagnosticCaptureStorageStatus struct {
	CurrentBytes          int64  `json:"current_bytes"`
	TemporaryBytes        int64  `json:"temporary_bytes"`
	LastCleanupAt         int64  `json:"last_cleanup_at"`
	LastCleanupCutoff     int64  `json:"last_cleanup_cutoff"`
	LastDeletedCount      int64  `json:"last_deleted_count"`
	LastFreedBytes        int64  `json:"last_freed_bytes"`
	LastCleanupStatus     string `json:"last_cleanup_status"`
	NextCleanupEligibleAt int64  `json:"next_cleanup_eligible_at"`
	LastTempCleanupAt     int64  `json:"last_temp_cleanup_at"`
	LastTempDeletedCount  int64  `json:"last_temp_deleted_count"`
	LastTempFreedBytes    int64  `json:"last_temp_freed_bytes"`
}

func enforceDiagnosticCaptureStorage(cfg DiagnosticCaptureConfig, activePath string, previousSize, currentSize int64) {
	recordDiagnosticCaptureStorageChange(cfg, activePath, currentSize-previousSize)
}

// recordDiagnosticCaptureTempStorageDeletion updates an initialized counter
// after the asynchronous temp janitor removes files. When initialization has
// not completed, the upcoming startup scan already sees the post-cleanup size.
func recordDiagnosticCaptureTempStorageDeletion(cfg DiagnosticCaptureConfig, freedBytes int64) {
	if freedBytes <= 0 {
		return
	}
	diagnosticCaptureStorageState.Lock()
	defer diagnosticCaptureStorageState.Unlock()
	if !diagnosticCaptureStorageState.initialized ||
		diagnosticCaptureStorageState.captureDir != cfg.CaptureDir ||
		diagnosticCaptureStorageState.tempDir != cfg.TempDir ||
		diagnosticCaptureStorageState.failureDir != cfg.FailureDir {
		return
	}
	diagnosticCaptureStorageState.totalBytes -= freedBytes
	diagnosticCaptureStorageState.generation++
	if diagnosticCaptureStorageState.totalBytes < 0 {
		diagnosticCaptureStorageState.totalBytes = 0
	}
	diagnosticCaptureStorageState.tempBytes -= freedBytes
	if diagnosticCaptureStorageState.tempBytes < 0 {
		diagnosticCaptureStorageState.tempBytes = 0
	}
}

func recordDiagnosticCaptureTempBytesChange(cfg DiagnosticCaptureConfig, delta int64) {
	if delta == 0 {
		return
	}
	diagnosticCaptureStorageState.Lock()
	defer diagnosticCaptureStorageState.Unlock()
	if !diagnosticCaptureStorageState.initialized ||
		diagnosticCaptureStorageState.captureDir != cfg.CaptureDir ||
		diagnosticCaptureStorageState.tempDir != cfg.TempDir ||
		diagnosticCaptureStorageState.failureDir != cfg.FailureDir {
		return
	}
	diagnosticCaptureStorageState.tempBytes += delta
	diagnosticCaptureStorageState.generation++
	if diagnosticCaptureStorageState.tempBytes < 0 {
		diagnosticCaptureStorageState.tempBytes = 0
	}
}

func cleanupDiagnosticCaptureStorageIfNeeded(cfg DiagnosticCaptureConfig) {
	if !cfg.AutoCleanupEnabled || cfg.MaxStorageBytes <= 0 {
		return
	}
	recordDiagnosticCaptureStorageChange(cfg, "", 0)
}

// NotifyDiagnosticCaptureCleanupSettingsChanged invalidates a previously
// computed retention wait and schedules one asynchronous capacity check. It is
// called only after an administrator changes a setting that can alter which
// capture directories are eligible for deletion.
func NotifyDiagnosticCaptureCleanupSettingsChanged() {
	diagnosticCaptureStorageState.Lock()
	diagnosticCaptureStorageState.nextEligibleAt = time.Time{}
	diagnosticCaptureStorageState.lastAttempt = time.Time{}
	diagnosticCaptureStorageState.Unlock()
	go func() {
		cfg := DiagnosticCaptureConfigFromOptions()
		refreshDiagnosticCaptureStorageState(cfg)
		cleanupDiagnosticCaptureStorageIfNeeded(cfg)
	}()
}

func refreshDiagnosticCaptureStorageState(cfg DiagnosticCaptureConfig) {
	// The counter is maintained from capture writes and deletes. A full scan is
	// only needed after startup or a directory change; doing it every cleanup
	// interval would contend with active capture workers on large stores.
	diagnosticCaptureStorageState.Lock()
	needsScan := !diagnosticCaptureStorageState.initialized ||
		diagnosticCaptureStorageState.captureDir != cfg.CaptureDir ||
		diagnosticCaptureStorageState.tempDir != cfg.TempDir ||
		diagnosticCaptureStorageState.failureDir != cfg.FailureDir
	diagnosticCaptureStorageState.Unlock()
	if !needsScan {
		return
	}

	totalBytes, tempBytes, err := scanDiagnosticCaptureTotalBytes(cfg)
	if err != nil {
		common.SysError("failed to scan diagnostic capture storage: " + err.Error())
		return
	}

	diagnosticCaptureStorageState.Lock()
	diagnosticCaptureStorageState.captureDir = cfg.CaptureDir
	diagnosticCaptureStorageState.tempDir = cfg.TempDir
	diagnosticCaptureStorageState.failureDir = cfg.FailureDir
	diagnosticCaptureStorageState.totalBytes = totalBytes
	diagnosticCaptureStorageState.tempBytes = tempBytes
	diagnosticCaptureStorageState.initialized = true
	if cfg.NextCleanupEligibleAt > time.Now().Unix() {
		diagnosticCaptureStorageState.nextEligibleAt = time.Unix(cfg.NextCleanupEligibleAt, 0)
	} else {
		diagnosticCaptureStorageState.nextEligibleAt = time.Time{}
	}
	diagnosticCaptureStorageState.lastReconciled = time.Now()
	diagnosticCaptureStorageState.Unlock()
}

// reconcileDiagnosticCaptureStorage refreshes accounting from disk once per
// day. It only publishes the scan when no capture mutation occurred while the
// filesystem was being traversed, so concurrent relay traffic cannot lose an
// in-memory accounting update.
func reconcileDiagnosticCaptureStorage() {
	cfg := DiagnosticCaptureConfigFromOptions()
	diagnosticCaptureStorageState.Lock()
	if !diagnosticCaptureStorageState.initialized ||
		diagnosticCaptureStorageState.captureDir != cfg.CaptureDir ||
		diagnosticCaptureStorageState.tempDir != cfg.TempDir ||
		diagnosticCaptureStorageState.failureDir != cfg.FailureDir {
		diagnosticCaptureStorageState.Unlock()
		refreshDiagnosticCaptureStorageState(cfg)
		return
	}
	generation := diagnosticCaptureStorageState.generation
	diagnosticCaptureStorageState.Unlock()

	totalBytes, tempBytes, err := scanDiagnosticCaptureTotalBytes(cfg)
	if err != nil {
		common.SysError("failed to reconcile diagnostic capture storage: " + err.Error())
		return
	}

	diagnosticCaptureStorageState.Lock()
	defer diagnosticCaptureStorageState.Unlock()
	if diagnosticCaptureStorageState.captureDir != cfg.CaptureDir ||
		diagnosticCaptureStorageState.tempDir != cfg.TempDir ||
		diagnosticCaptureStorageState.failureDir != cfg.FailureDir ||
		diagnosticCaptureStorageState.generation != generation {
		return
	}
	diagnosticCaptureStorageState.totalBytes = totalBytes
	diagnosticCaptureStorageState.tempBytes = tempBytes
	diagnosticCaptureStorageState.lastReconciled = time.Now()
}

func recordDiagnosticCaptureStorageChange(cfg DiagnosticCaptureConfig, activePath string, delta int64) {
	if !cfg.AutoCleanupEnabled || cfg.MaxStorageBytes <= 0 {
		diagnosticCaptureStorageState.Lock()
		if diagnosticCaptureStorageState.initialized &&
			diagnosticCaptureStorageState.captureDir == cfg.CaptureDir &&
			diagnosticCaptureStorageState.tempDir == cfg.TempDir &&
			diagnosticCaptureStorageState.failureDir == cfg.FailureDir {
			diagnosticCaptureStorageState.totalBytes += delta
			if diagnosticCaptureStorageState.totalBytes < 0 {
				diagnosticCaptureStorageState.totalBytes = 0
			}
		}
		diagnosticCaptureStorageState.Unlock()
		return
	}

	diagnosticCaptureStorageState.Lock()
	now := time.Now()
	if !diagnosticCaptureStorageState.initialized ||
		diagnosticCaptureStorageState.captureDir != cfg.CaptureDir ||
		diagnosticCaptureStorageState.tempDir != cfg.TempDir ||
		diagnosticCaptureStorageState.failureDir != cfg.FailureDir {
		diagnosticCaptureStorageState.Unlock()
		refreshDiagnosticCaptureStorageState(cfg)
		// Every caller records a filesystem mutation after it has completed.
		// The initialization scan above already includes that mutation, so
		// applying delta again would double-count the first write or delete.
		return
	}
	diagnosticCaptureStorageState.totalBytes += delta
	diagnosticCaptureStorageState.generation++
	if diagnosticCaptureStorageState.totalBytes < 0 {
		diagnosticCaptureStorageState.totalBytes = 0
	}
	if diagnosticCaptureStorageState.totalBytes < cfg.MaxStorageBytes ||
		time.Since(diagnosticCaptureStorageState.lastAttempt) < time.Minute {
		diagnosticCaptureStorageState.Unlock()
		return
	}
	if !diagnosticCaptureStorageState.nextEligibleAt.IsZero() && now.Before(diagnosticCaptureStorageState.nextEligibleAt) {
		diagnosticCaptureStorageState.Unlock()
		return
	}
	diagnosticCaptureStorageState.lastAttempt = now
	totalBytes := diagnosticCaptureStorageState.totalBytes
	diagnosticCaptureStorageState.Unlock()
	go cleanupDiagnosticCaptureStorage(cfg, filepath.Clean(activePath), totalBytes, now)
}

// cleanupDiagnosticCaptureStorage runs independently of body spooling. A
// rate-limited cleanup may take hours; blocking the capture worker here would
// turn a disk-maintenance delay into an unbounded pending-event backlog.
func cleanupDiagnosticCaptureStorage(cfg DiagnosticCaptureConfig, activePath string, totalBytes int64, now time.Time) {
	// A single cleanup owns the deletion rate. Later triggers are satisfied by
	// the running pass or the next periodic check instead of queueing workers.
	if !diagnosticCaptureCleanupMu.TryLock() {
		return
	}
	defer diagnosticCaptureCleanupMu.Unlock()

	tempFreedBytes := int64(0)
	if cfg.TempDir != "" {
		cutoff := now.Add(-time.Duration(cfg.TempRetentionMinutes) * time.Minute)
		deletedCount, freedBytes := cleanupDiagnosticCaptureTempFilesAtRate(cfg.TempDir, cutoff, cfg.CleanupRateBytesPerSecond)
		recordDiagnosticCaptureTempCleanup(deletedCount, freedBytes)
		recordDiagnosticCaptureTempBytesChange(cfg, -freedBytes)
		tempFreedBytes = freedBytes
		totalBytes -= freedBytes
		if totalBytes < 0 {
			totalBytes = 0
		}
	}
	targetBytes := cfg.MaxStorageBytes
	if cfg.CleanupPercent > 0 {
		targetBytes = cfg.MaxStorageBytes * (100 - cfg.CleanupPercent) / 100
	}
	cutoff := now.Add(-time.Duration(cfg.MinRetentionMinutes) * time.Minute)
	var incompleteCutoff time.Time
	if cfg.IncompleteTimeoutMinutes > 0 {
		incompleteCutoff = now.Add(-time.Duration(cfg.IncompleteTimeoutMinutes) * time.Minute)
	}
	remainingBytes, deletedCount, freedBytes, lastDeletedAt := cleanupDiagnosticCaptureStorageByDate(
		totalBytes,
		cfg,
		activePath,
		targetBytes,
		cutoff,
		incompleteCutoff,
	)
	diagnosticCaptureStorageState.Lock()
	// Concurrent capture workers may have appended bytes while cleanup ran.
	// Deduct only what this cleanup actually removed from their latest total.
	sameConfig := diagnosticCaptureStorageState.initialized &&
		diagnosticCaptureStorageState.captureDir == cfg.CaptureDir &&
		diagnosticCaptureStorageState.tempDir == cfg.TempDir &&
		diagnosticCaptureStorageState.failureDir == cfg.FailureDir
	if sameConfig {
		diagnosticCaptureStorageState.totalBytes -= (totalBytes - remainingBytes) + tempFreedBytes
		diagnosticCaptureStorageState.generation++
		if diagnosticCaptureStorageState.totalBytes < 0 {
			diagnosticCaptureStorageState.totalBytes = 0
		}
	}
	currentBytes := diagnosticCaptureStorageState.totalBytes
	diagnosticCaptureStorageState.Unlock()
	if !sameConfig {
		return
	}

	status := "completed"
	nextEligibleAt := int64(0)
	if currentBytes > targetBytes {
		status = "retention_limited"
		next := now.Add(time.Duration(cfg.MinRetentionMinutes) * time.Minute)
		if cfg.IncompleteTimeoutMinutes > 0 {
			incompleteNext := now.Add(time.Duration(cfg.IncompleteTimeoutMinutes) * time.Minute)
			if next.IsZero() || incompleteNext.Before(next) {
				next = incompleteNext
			}
		}
		if next.After(now) {
			nextEligibleAt = next.Unix()
		}
	}
	diagnosticCaptureStorageState.Lock()
	if diagnosticCaptureStorageState.captureDir == cfg.CaptureDir &&
		diagnosticCaptureStorageState.tempDir == cfg.TempDir &&
		diagnosticCaptureStorageState.failureDir == cfg.FailureDir {
		if nextEligibleAt > 0 {
			diagnosticCaptureStorageState.nextEligibleAt = time.Unix(nextEligibleAt, 0)
		} else {
			diagnosticCaptureStorageState.nextEligibleAt = time.Time{}
		}
	}
	diagnosticCaptureStorageState.Unlock()
	if err := model.UpdateOptionsBulk(map[string]string{
		DiagnosticCaptureLastCleanupAtKey:           strconv.FormatInt(time.Now().Unix(), 10),
		DiagnosticCaptureLastCleanupCutoffKey:       strconv.FormatInt(lastDeletedAt, 10),
		DiagnosticCaptureLastCleanupDeletedCountKey: strconv.FormatInt(deletedCount, 10),
		DiagnosticCaptureLastCleanupFreedBytesKey:   strconv.FormatInt(freedBytes, 10),
		DiagnosticCaptureLastCleanupStatusKey:       status,
		DiagnosticCaptureNextCleanupEligibleAtKey:   strconv.FormatInt(nextEligibleAt, 10),
	}); err != nil {
		common.SysError("failed to save diagnostic capture cleanup status: " + err.Error())
	}
}

type diagnosticCaptureDayDir struct {
	path    string
	date    time.Time
	failure bool
}

type diagnosticCaptureCandidateHeap []diagnosticCaptureCandidate

func (h diagnosticCaptureCandidateHeap) Len() int { return len(h) }
func (h diagnosticCaptureCandidateHeap) Less(i, j int) bool {
	return h[i].capturedAt.After(h[j].capturedAt)
}
func (h diagnosticCaptureCandidateHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *diagnosticCaptureCandidateHeap) Push(value any) {
	*h = append(*h, value.(diagnosticCaptureCandidate))
}
func (h *diagnosticCaptureCandidateHeap) Pop() any {
	current := *h
	last := len(current) - 1
	value := current[last]
	*h = current[:last]
	return value
}

func diagnosticCaptureRateDuration(bytes, rateBytesPerSecond int64) time.Duration {
	if bytes <= 0 || rateBytesPerSecond <= 0 {
		return 0
	}
	seconds := bytes / rateBytesPerSecond
	if seconds > int64((time.Duration(1<<63-1))/time.Second) {
		return time.Duration(1<<63 - 1)
	}
	milliseconds := (bytes % rateBytesPerSecond) * 1000 / rateBytesPerSecond
	return time.Duration(seconds)*time.Second + time.Duration(milliseconds)*time.Millisecond
}

// cleanupDiagnosticCaptureStorageByDate scans only one date directory at a
// time. It deliberately avoids retaining every capture directory in memory.
func cleanupDiagnosticCaptureStorageByDate(totalBytes int64, cfg DiagnosticCaptureConfig, activePath string, targetBytes int64, cutoff, incompleteCutoff time.Time) (int64, int64, int64, int64) {
	return cleanupDiagnosticCaptureStorageByRoots(totalBytes, cfg, activePath, targetBytes, cutoff, incompleteCutoff, []string{cfg.CaptureDir, cfg.FailureDir})
}

func cleanupDiagnosticCaptureStorageByRoots(totalBytes int64, cfg DiagnosticCaptureConfig, activePath string, targetBytes int64, cutoff, incompleteCutoff time.Time, roots []string) (int64, int64, int64, int64) {
	days := make([]diagnosticCaptureDayDir, 0)
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		rootDays, err := diagnosticCaptureDayDirs(root)
		if err != nil {
			common.SysError("failed to list diagnostic capture directories: " + err.Error())
			continue
		}
		for index := range rootDays {
			rootDays[index].failure = filepath.Clean(root) == filepath.Clean(cfg.FailureDir)
		}
		days = append(days, rootDays...)
	}
	sort.Slice(days, func(i, j int) bool {
		return days[i].date.Before(days[j].date)
	})

	var deletedCount int64
	var freedBytes int64
	var lastDeletedAt int64
	batchCount := 0
	batchFreedBytes := int64(0)
	batchStartedAt := time.Now()
	for dayIndex := 0; dayIndex < len(days); {
		if totalBytes <= targetBytes {
			break
		}
		date := days[dayIndex].date
		dateDays := make([]diagnosticCaptureDayDir, 0)
		for dayIndex < len(days) && days[dayIndex].date.Equal(date) {
			dateDays = append(dateDays, days[dayIndex])
			dayIndex++
		}
		for totalBytes > targetBytes {
			candidates := diagnosticCaptureOldestCandidates(dateDays, activePath, cutoff, incompleteCutoff)
			if len(candidates) == 0 {
				break
			}
			for _, candidate := range candidates {
				if totalBytes <= targetBytes {
					break
				}
				actualSize, sizeErr := diagnosticCaptureDirectorySize(candidate.dir)
				if sizeErr != nil || actualSize == 0 {
					continue
				}
				if err := os.RemoveAll(candidate.dir); err != nil {
					common.SysError("failed to delete diagnostic capture: " + err.Error())
					continue
				}
				totalBytes -= actualSize
				if totalBytes < 0 {
					totalBytes = 0
				}
				freedBytes += actualSize
				deletedCount++
				lastDeletedAt = candidate.capturedAt.Unix()
				batchCount++
				batchFreedBytes += actualSize
				if cfg.CleanupRateBytesPerSecond > 0 || batchCount >= diagnosticCaptureCleanupBatchSize {
					if cfg.CleanupRateBytesPerSecond > 0 {
						expected := diagnosticCaptureRateDuration(batchFreedBytes, cfg.CleanupRateBytesPerSecond)
						if remaining := expected - time.Since(batchStartedAt); remaining > 0 {
							time.Sleep(remaining)
						}
					}
					batchCount = 0
					batchFreedBytes = 0
					batchStartedAt = time.Now()
					runtime.Gosched()
				}
			}
			if len(candidates) < diagnosticCaptureCleanupSelectSize {
				break
			}
		}
		for _, day := range dateDays {
			removeEmptyDiagnosticCaptureDirectories(filepath.Dir(filepath.Dir(day.path)), day.path)
		}
	}
	if batchCount > 0 && cfg.CleanupRateBytesPerSecond > 0 {
		expected := diagnosticCaptureRateDuration(batchFreedBytes, cfg.CleanupRateBytesPerSecond)
		if remaining := expected - time.Since(batchStartedAt); remaining > 0 {
			time.Sleep(remaining)
		}
	}
	return totalBytes, deletedCount, freedBytes, lastDeletedAt
}

func diagnosticCaptureOldestCandidates(days []diagnosticCaptureDayDir, activePath string, cutoff, incompleteCutoff time.Time) []diagnosticCaptureCandidate {
	candidates := make(diagnosticCaptureCandidateHeap, 0, diagnosticCaptureCleanupSelectSize)
	for _, day := range days {
		dir, err := os.Open(day.path)
		if err != nil {
			continue
		}
		for {
			entries, readErr := dir.ReadDir(diagnosticCaptureDirectoryReadSize)
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				candidateDir := filepath.Join(day.path, entry.Name())
				if filepath.Clean(filepath.Join(candidateDir, "request-log.json")) == activePath {
					continue
				}
				capturedAt, created := diagnosticCaptureCreatedAt(candidateDir)
				if !created {
					continue
				}
				candidate := diagnosticCaptureCandidate{
					dir:        candidateDir,
					traceID:    entry.Name(),
					capturedAt: capturedAt,
					complete:   diagnosticCaptureComplete(candidateDir),
				}
				if isDiagnosticCaptureActive(candidate.traceID) ||
					(candidate.complete && !candidate.capturedAt.Before(cutoff)) ||
					(!candidate.complete && (incompleteCutoff.IsZero() || !candidate.capturedAt.Before(incompleteCutoff))) ||
					(day.failure && diagnosticCaptureFailureRetryable(candidateDir, time.Now())) {
					continue
				}
				if candidates.Len() < diagnosticCaptureCleanupSelectSize {
					heap.Push(&candidates, candidate)
				} else if candidate.capturedAt.Before(candidates[0].capturedAt) {
					heap.Pop(&candidates)
					heap.Push(&candidates, candidate)
				}
			}
			if readErr == io.EOF || readErr != nil {
				break
			}
		}
		_ = dir.Close()
	}
	result := make([]diagnosticCaptureCandidate, len(candidates))
	copy(result, candidates)
	sort.Slice(result, func(i, j int) bool { return result[i].capturedAt.Before(result[j].capturedAt) })
	return result
}

func diagnosticCaptureFailureRetryable(dir string, now time.Time) bool {
	data, err := os.ReadFile(filepath.Join(dir, "capture-failure.json"))
	if err != nil {
		return false
	}
	var record diagnosticCaptureFailureRecord
	if common.Unmarshal(data, &record) != nil || !record.Retryable {
		return false
	}
	firstFailedAt := record.FirstFailedAt
	if firstFailedAt == 0 {
		firstFailedAt = record.LastAttemptAt
	}
	if firstFailedAt == 0 {
		firstFailedAt = record.StartedAt
	}
	return firstFailedAt > 0 && now.Sub(time.Unix(0, firstFailedAt)) < diagnosticCaptureRetryWindow
}

// removeEmptyDiagnosticCaptureDirectories removes empty directories below root
// while preserving the configured root itself. It is used after deleting a
// request-scoped directory so retention cleanup does not leave empty channel or
// date directories behind.
func removeEmptyDiagnosticCaptureDirectories(root, dir string) {
	root = filepath.Clean(root)
	if root == "." || root == "" {
		return
	}
	for current := filepath.Clean(dir); strings.HasPrefix(current, root+string(os.PathSeparator)); current = filepath.Dir(current) {
		if err := os.Remove(current); err != nil {
			return
		}
	}
}

func diagnosticCaptureDayDirs(captureDir string) ([]diagnosticCaptureDayDir, error) {
	channels, err := os.ReadDir(captureDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	days := make([]diagnosticCaptureDayDir, 0)
	for _, channel := range channels {
		if !channel.IsDir() {
			continue
		}
		channelDir := filepath.Join(captureDir, channel.Name())
		dateDirs, err := os.ReadDir(channelDir)
		if err != nil {
			continue
		}
		for _, dateDir := range dateDirs {
			if !dateDir.IsDir() {
				continue
			}
			date, err := time.ParseInLocation("2006-01-02", dateDir.Name(), time.Local)
			if err != nil {
				continue
			}
			days = append(days, diagnosticCaptureDayDir{path: filepath.Join(channelDir, dateDir.Name()), date: date})
		}
	}
	sort.Slice(days, func(i, j int) bool {
		return days[i].date.Before(days[j].date)
	})
	return days, nil
}

func cleanupDiagnosticCaptureCandidates(totalBytes int64, candidates []diagnosticCaptureCandidate, targetBytes int64, cutoff, incompleteCutoff time.Time, rateBytesPerSecond int64) (int64, int64, int64, int64) {
	var deletedCount int64
	var freedBytes int64
	var lastDeletedAt int64
	batchCount := 0
	batchFreedBytes := int64(0)
	batchStartedAt := time.Now()
	for _, candidate := range candidates {
		if totalBytes <= targetBytes {
			break
		}
		if isDiagnosticCaptureActive(candidate.traceID) {
			continue
		}
		if candidate.complete {
			if !candidate.capturedAt.Before(cutoff) {
				continue
			}
		} else if !incompleteCutoff.IsZero() {
			if !candidate.capturedAt.Before(incompleteCutoff) {
				continue
			}
		} else {
			continue
		}
		if err := os.RemoveAll(candidate.dir); err != nil {
			common.SysError("failed to delete diagnostic capture: " + err.Error())
			continue
		}
		totalBytes -= candidate.size
		freedBytes += candidate.size
		batchFreedBytes += candidate.size
		deletedCount++
		lastDeletedAt = candidate.capturedAt.Unix()
		batchCount++
		if rateBytesPerSecond > 0 || batchCount >= diagnosticCaptureCleanupBatchSize {
			if rateBytesPerSecond > 0 {
				expected := diagnosticCaptureRateDuration(batchFreedBytes, rateBytesPerSecond)
				if remaining := expected - time.Since(batchStartedAt); remaining > 0 {
					time.Sleep(remaining)
				}
			}
			batchCount = 0
			batchFreedBytes = 0
			batchStartedAt = time.Now()
			runtime.Gosched()
		}
	}
	return totalBytes, deletedCount, freedBytes, lastDeletedAt
}

func scanDiagnosticCaptureStorage(captureDir, activePath string) (int64, []diagnosticCaptureCandidate, error) {
	activePath = filepath.Clean(activePath)
	var totalBytes int64
	var candidates []diagnosticCaptureCandidate
	err := filepath.WalkDir(captureDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		totalBytes += info.Size()
		if entry.Name() != "request-log.json" || filepath.Clean(path) == activePath {
			return nil
		}
		captureDir := filepath.Dir(path)
		traceID := filepath.Base(captureDir)
		capturedAt, created := diagnosticCaptureCreatedAt(captureDir)
		if !created {
			return nil
		}
		captureSize, err := diagnosticCaptureDirectorySize(captureDir)
		if err != nil {
			return err
		}
		candidates = append(candidates, diagnosticCaptureCandidate{
			dir:        captureDir,
			traceID:    traceID,
			size:       captureSize,
			capturedAt: capturedAt,
			complete:   diagnosticCaptureComplete(captureDir),
		})
		return nil
	})
	return totalBytes, candidates, err
}

func scanDiagnosticCaptureTotalBytes(cfg DiagnosticCaptureConfig) (int64, int64, error) {
	formalBytes, err := diagnosticCaptureDirectorySize(cfg.CaptureDir)
	if err != nil && !os.IsNotExist(err) {
		return 0, 0, err
	}
	if os.IsNotExist(err) {
		formalBytes = 0
	}
	tempBytes, err := diagnosticCaptureTempStorageBytes(cfg.TempDir)
	if err != nil {
		return 0, 0, err
	}
	failureBytes, err := diagnosticCaptureDirectorySize(cfg.FailureDir)
	if err != nil && !os.IsNotExist(err) {
		return 0, 0, err
	}
	if os.IsNotExist(err) {
		failureBytes = 0
	}
	return formalBytes + tempBytes + failureBytes, tempBytes, nil
}

func scanDiagnosticCaptureTotalStorage(cfg DiagnosticCaptureConfig, activePath string) (int64, []diagnosticCaptureCandidate, error) {
	formalBytes, candidates, err := scanDiagnosticCaptureStorage(cfg.CaptureDir, activePath)
	if err != nil && !os.IsNotExist(err) {
		return 0, nil, err
	}
	if os.IsNotExist(err) {
		formalBytes = 0
	}
	tempBytes := int64(0)
	if cfg.TempDir != "" {
		tempBytes, err = diagnosticCaptureDirectorySize(cfg.TempDir)
		if err != nil && !os.IsNotExist(err) {
			return 0, nil, err
		}
		if os.IsNotExist(err) {
			tempBytes = 0
		}
	}
	return formalBytes + tempBytes, candidates, nil
}

func diagnosticCaptureTempStorageBytes(tempDir string) (int64, error) {
	if tempDir == "" {
		return 0, nil
	}
	size, err := diagnosticCaptureDirectorySize(tempDir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	return size, err
}

func diagnosticCaptureDirectorySize(dir string) (int64, error) {
	var size int64
	err := filepath.WalkDir(dir, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		return nil
	})
	return size, err
}

func diagnosticCaptureCreatedAt(dir string) (time.Time, bool) {
	data, err := os.ReadFile(filepath.Join(dir, ".capture-created-at"))
	if err != nil {
		return time.Time{}, false
	}
	nanos, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || nanos <= 0 {
		return time.Time{}, false
	}
	return time.Unix(0, nanos), true
}

func GetDiagnosticCaptureStorageStatus() (DiagnosticCaptureStorageStatus, error) {
	cfg := DiagnosticCaptureConfigFromOptions()
	var currentBytes, temporaryBytes int64
	diagnosticCaptureStorageState.Lock()
	if diagnosticCaptureStorageState.initialized &&
		diagnosticCaptureStorageState.captureDir == cfg.CaptureDir &&
		diagnosticCaptureStorageState.tempDir == cfg.TempDir &&
		diagnosticCaptureStorageState.failureDir == cfg.FailureDir {
		currentBytes = diagnosticCaptureStorageState.totalBytes
		temporaryBytes = diagnosticCaptureStorageState.tempBytes
	}
	diagnosticCaptureStorageState.Unlock()
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	lastCleanupStatus := common.OptionMap[DiagnosticCaptureLastCleanupStatusKey]
	// A retention-limited result describes the previous cleanup run. Once the
	// current usage is back below the configured trigger, it is no longer an
	// active restriction and should not remain as a stale warning in the UI.
	if lastCleanupStatus == "retention_limited" && cfg.MaxStorageBytes > 0 && currentBytes <= cfg.MaxStorageBytes {
		lastCleanupStatus = ""
	}
	return DiagnosticCaptureStorageStatus{
		CurrentBytes:          currentBytes,
		TemporaryBytes:        temporaryBytes,
		LastCleanupAt:         diagnosticCaptureOptionInt64(DiagnosticCaptureLastCleanupAtKey),
		LastCleanupCutoff:     diagnosticCaptureOptionInt64(DiagnosticCaptureLastCleanupCutoffKey),
		LastDeletedCount:      diagnosticCaptureOptionInt64(DiagnosticCaptureLastCleanupDeletedCountKey),
		LastFreedBytes:        diagnosticCaptureOptionInt64(DiagnosticCaptureLastCleanupFreedBytesKey),
		LastCleanupStatus:     lastCleanupStatus,
		NextCleanupEligibleAt: diagnosticCaptureOptionInt64(DiagnosticCaptureNextCleanupEligibleAtKey),
		LastTempCleanupAt:     diagnosticCaptureOptionInt64(DiagnosticCaptureLastTempCleanupAtKey),
		LastTempDeletedCount:  diagnosticCaptureOptionInt64(DiagnosticCaptureLastTempDeletedCountKey),
		LastTempFreedBytes:    diagnosticCaptureOptionInt64(DiagnosticCaptureLastTempFreedBytesKey),
	}, nil
}

func recordDiagnosticCaptureTempCleanup(deletedCount, freedBytes int64) {
	if err := model.UpdateOptionsBulk(map[string]string{
		DiagnosticCaptureLastTempCleanupAtKey:    strconv.FormatInt(time.Now().Unix(), 10),
		DiagnosticCaptureLastTempDeletedCountKey: strconv.FormatInt(deletedCount, 10),
		DiagnosticCaptureLastTempFreedBytesKey:   strconv.FormatInt(freedBytes, 10),
	}); err != nil {
		common.SysError("failed to save diagnostic capture temporary cleanup status: " + err.Error())
	}
}

func diagnosticCaptureOptionInt64(key string) int64 {
	value, _ := strconv.ParseInt(common.OptionMap[key], 10, 64)
	return value
}

func upsertAPIRequest(combined *diagnosticCombinedCPAJSON, item diagnosticAPIRequestJSON) {
	if combined == nil {
		return
	}
	for i := range combined.APIRequests {
		if combined.APIRequests[i].Sequence == item.Sequence {
			combined.APIRequests[i] = item
			return
		}
	}
	combined.APIRequests = append(combined.APIRequests, item)
}

func upsertAPIResponse(combined *diagnosticCombinedCPAJSON, item diagnosticAPIResponseJSON) {
	if combined == nil {
		return
	}
	for i := range combined.APIResponses {
		if combined.APIResponses[i].Sequence == item.Sequence {
			combined.APIResponses[i] = item
			return
		}
	}
	combined.APIResponses = append(combined.APIResponses, item)
}

func buildDiagnosticCPAJSON(cfg DiagnosticCaptureConfig, flow *DiagnosticFlow, sequence int64, role, part string, meta map[string]any, body captureBody) diagnosticCPAJSON {
	capturedAt := stringFromMeta(meta, "captured_at")
	if capturedAt == "" {
		capturedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	content := diagnosticCPAJSON{
		Format:     "cpa-sections-json",
		Version:    1,
		CapturedAt: capturedAt,
		Direction:  role,
		Section:    part,
		Meta:       compactCaptureMeta(meta),
	}

	headers := headersFromMeta(meta, "headers")
	bodyJSON := encodeDiagnosticBody(cfg.Mode, body)
	if part == "request" {
		if role == "outbound" {
			content.APIRequest = &diagnosticAPIRequestJSON{
				Sequence:    sequence,
				Timestamp:   capturedAt,
				UpstreamURL: stringFromMeta(meta, "url"),
				HTTPMethod:  stringFromMeta(meta, "method"),
				Headers:     headers,
				Body:        bodyJSON,
			}
			return content
		}
		content.RequestInfo = &diagnosticRequestInfoJSON{
			AppVersion:          stringFromMeta(meta, "protocol"),
			URL:                 stringFromMeta(meta, "path"),
			Method:              stringFromMeta(meta, "method"),
			DownstreamTransport: "http",
			UpstreamTransport:   "http",
			Timestamp:           capturedAt,
			RemoteAddr:          stringFromMeta(meta, "remote_addr"),
		}
		if content.RequestInfo.AppVersion == "" {
			content.RequestInfo.AppVersion = common.Version
		}
		content.Headers = headers
		content.RequestBody = &bodyJSON
		return content
	}

	if role == "outbound" {
		content.APIResponse = &diagnosticAPIResponseJSON{
			Sequence:  sequence,
			Timestamp: capturedAt,
			Status:    intFromMeta(meta, "status_code"),
			Headers:   headers,
			Body:      bodyJSON,
			Error:     stringFromMeta(meta, "error"),
		}
		return content
	}

	content.Response = &diagnosticResponseJSON{
		Status:     intFromMeta(meta, "status_code"),
		DurationMS: int64FromMeta(meta, "duration_ms"),
		Headers:    headers,
		Body:       bodyJSON,
	}
	return content
}

func compactCaptureMeta(meta map[string]any) map[string]any {
	result := make(map[string]any, len(meta))
	for key, value := range meta {
		switch key {
		case "headers", "captured_at", "method", "path", "url", "remote_addr", "status_code", "duration_ms", "error":
			continue
		default:
			result[key] = value
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func encodeDiagnosticBody(mode string, body captureBody) diagnosticBodyJSON {
	result := diagnosticBodyJSON{
		Mode:         mode,
		Encoding:     "empty",
		OriginalSize: body.OriginalSize,
		SavedSize:    body.SavedSize,
		Truncated:    body.Truncated,
	}
	if mode != "full" {
		result.Encoding = "metadata-only"
		return result
	}
	if len(body.Data) == 0 {
		return result
	}
	if body.OriginalSize == 0 {
		result.OriginalSize = int64(len(body.Data))
	}
	if body.SavedSize == 0 {
		result.SavedSize = int64(len(body.Data))
	}

	trimmed := bytes.TrimSpace(body.Data)
	var parsed any
	if len(trimmed) > 0 && json.Unmarshal(trimmed, &parsed) == nil {
		result.Encoding = "json"
		result.JSON = parsed
		return result
	}
	if utf8.Valid(body.Data) {
		result.Encoding = "text"
		result.Text = string(body.Data)
		return result
	}
	result.Encoding = "base64"
	result.Base64 = base64.StdEncoding.EncodeToString(body.Data)
	return result
}

func headersFromMeta(meta map[string]any, key string) map[string][]string {
	value, ok := meta[key]
	if !ok || value == nil {
		return nil
	}
	if headers, ok := value.(map[string][]string); ok {
		return headers
	}
	return nil
}

func stringFromMeta(meta map[string]any, key string) string {
	value, ok := meta[key]
	if !ok || value == nil {
		return ""
	}
	if str, ok := value.(string); ok {
		return str
	}
	return fmt.Sprint(value)
}

func intFromMeta(meta map[string]any, key string) int {
	value, ok := meta[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
}

func int64FromMeta(meta map[string]any, key string) int64 {
	value, ok := meta[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}

func leftPadSequence(sequence int64) string {
	if sequence < 0 {
		sequence = 0
	}
	return fmt.Sprintf("%06d", sequence)
}

func getInboundRequestBody(c *gin.Context, maxBytes int64) captureBody {
	storage, err := common.GetBodyStorage(c)
	if err != nil || storage == nil {
		return captureBody{}
	}
	body, err := storage.Bytes()
	if err != nil {
		return captureBody{}
	}
	return truncateCaptureBody(body, maxBytes)
}

func readDiagnosticRequestBody(body io.Reader, maxBytes int64) (io.Reader, captureBody) {
	data, err := io.ReadAll(body)
	if err != nil {
		return body, captureBody{}
	}
	return bytes.NewReader(data), truncateCaptureBody(data, maxBytes)
}

func truncateCaptureBody(data []byte, maxBytes int64) captureBody {
	if maxBytes <= 0 || int64(len(data)) <= maxBytes {
		return captureBody{Data: data, OriginalSize: int64(len(data)), SavedSize: int64(len(data))}
	}
	return captureBody{
		Data:         data[:maxBytes],
		OriginalSize: int64(len(data)),
		SavedSize:    maxBytes,
		Truncated:    true,
	}
}

func parseDiagnosticPathRules(raw string) []string {
	parts := strings.Split(raw, ",")
	rules := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			rules = append(rules, part)
		}
	}
	return rules
}

func (cfg DiagnosticCaptureConfig) shouldCapturePath(path string) bool {
	if len(cfg.PathRules) == 0 {
		return true
	}
	for _, rule := range cfg.PathRules {
		if strings.HasPrefix(rule, "*") && strings.HasSuffix(rule, "*") && len(rule) > 2 {
			if strings.Contains(path, strings.Trim(rule, "*")) {
				return true
			}
			continue
		}
		if strings.HasSuffix(rule, "*") {
			if strings.HasPrefix(path, strings.TrimSuffix(rule, "*")) {
				return true
			}
			continue
		}
		if path == rule {
			return true
		}
	}
	return false
}

func safeTraceID(value string) string {
	return safeCaptureName(value, "trace")
}

func safeCaptureName(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		if r > 127 {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('_')
	}
	result := strings.Trim(b.String(), " .")
	if result == "" {
		return fallback
	}
	return result
}

func redactHeaders(headers http.Header) map[string][]string {
	result := make(map[string][]string, len(headers))
	for key, values := range headers {
		switch strings.ToLower(key) {
		case "authorization", "cookie", "set-cookie", "proxy-authorization", "x-api-key", "x-api-access-key", "x-api-app-id", "x-goog-api-key":
			result[key] = make([]string, len(values))
			for index, value := range values {
				result[key][index] = partiallyRedactDiagnosticHeader(value)
			}
		default:
			// Copy the slice so later request-header mutations cannot alter the
			// asynchronous record.
			result[key] = append([]string(nil), values...)
		}
	}
	return result
}

func partiallyRedactDiagnosticHeader(value string) string {
	const visibleCharacters = 6
	runes := []rune(value)
	if len(runes) == 0 {
		return "<redacted>"
	}
	visible := visibleCharacters
	if maximumVisible := (len(runes) - 1) / 2; visible > maximumVisible {
		visible = maximumVisible
	}
	if visible == 0 {
		visible = 1
	}
	return string(runes[:visible]) + "..." + string(runes[len(runes)-visible:])
}
