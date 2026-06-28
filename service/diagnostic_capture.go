package service

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

const defaultDiagnosticCapturePaths = "/v1/*,/v1beta/*,/pg/*,/mj/*,*/mj/*,/suno/*,/kling/v1/*,/jimeng/*"

const (
	DiagnosticCaptureEnabledKey   = "DiagnosticCaptureEnabled"
	DiagnosticCaptureModeKey      = "DiagnosticCaptureMode"
	DiagnosticCaptureDirKey       = "DiagnosticCaptureDir"
	DiagnosticCaptureMaxBodyMBKey = "DiagnosticCaptureMaxBodyMB"
	DiagnosticCapturePathsKey     = "DiagnosticCapturePaths"

	DiagnosticTraceHeader   = "X-Diagnostic-Trace-Id"
	DiagnosticChannelHeader = "X-Diagnostic-Channel"
)

type DiagnosticCaptureConfig struct {
	Enabled      bool
	Mode         string
	CaptureDir   string
	MaxBodyBytes int64
	PathRules    []string
}

type DiagnosticFlow struct {
	TraceID string
	Channel string
	Started time.Time
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
	TraceID     string                     `json:"trace_id"`
	Channel     string                     `json:"channel"`
	Direction   string                     `json:"direction"`
	Section     string                     `json:"section"`
	Sequence    int64                      `json:"sequence"`
	Meta        map[string]any             `json:"meta,omitempty"`
	RequestInfo *diagnosticRequestInfoJSON `json:"request_info,omitempty"`
	Headers     map[string][]string        `json:"headers,omitempty"`
	RequestBody *diagnosticBodyJSON        `json:"request_body,omitempty"`
	APIRequest  *diagnosticAPIRequestJSON  `json:"api_request,omitempty"`
	APIResponse *diagnosticAPIResponseJSON `json:"api_response,omitempty"`
	Response    *diagnosticResponseJSON    `json:"response,omitempty"`
}

type diagnosticCombinedCPAJSON struct {
	Format       string                      `json:"format"`
	Version      int                         `json:"version"`
	TraceID      string                      `json:"trace_id"`
	Channel      string                      `json:"channel"`
	StartedAt    string                      `json:"started_at"`
	UpdatedAt    string                      `json:"updated_at"`
	RequestInfo  *diagnosticRequestInfoJSON  `json:"request_info,omitempty"`
	Headers      map[string][]string         `json:"headers,omitempty"`
	RequestBody  *diagnosticBodyJSON         `json:"request_body,omitempty"`
	APIRequests  []diagnosticAPIRequestJSON  `json:"api_requests,omitempty"`
	APIResponses []diagnosticAPIResponseJSON `json:"api_responses,omitempty"`
	Response     *diagnosticResponseJSON     `json:"response,omitempty"`
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
	Sequence    int64               `json:"sequence"`
	Timestamp   string              `json:"timestamp"`
	UpstreamURL string              `json:"upstream_url"`
	HTTPMethod  string              `json:"http_method"`
	Headers     map[string][]string `json:"headers,omitempty"`
	Body        diagnosticBodyJSON  `json:"body"`
}

type diagnosticAPIResponseJSON struct {
	Sequence  int64               `json:"sequence"`
	Timestamp string              `json:"timestamp"`
	Status    int                 `json:"status,omitempty"`
	Headers   map[string][]string `json:"headers,omitempty"`
	Body      diagnosticBodyJSON  `json:"body"`
	Error     string              `json:"error,omitempty"`
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
	Flow     *DiagnosticFlow
	Sequence int64
	Started  time.Time
}

var diagnosticSequence = struct {
	sync.Mutex
	value int64
}{}

var diagnosticCaptureWriteMu sync.Mutex

func DefaultDiagnosticCaptureOptions() map[string]string {
	return map[string]string{
		DiagnosticCaptureEnabledKey:   "false",
		DiagnosticCaptureModeKey:      "full",
		DiagnosticCaptureDirKey:       "captures",
		DiagnosticCaptureMaxBodyMBKey: "10",
		DiagnosticCapturePathsKey:     defaultDiagnosticCapturePaths,
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
	mode := strings.ToLower(strings.TrimSpace(options[DiagnosticCaptureModeKey]))
	if mode != "metadata" && mode != "full" {
		mode = "full"
	}
	return DiagnosticCaptureConfig{
		Enabled:      options[DiagnosticCaptureEnabledKey] == "true",
		Mode:         mode,
		CaptureDir:   strings.TrimSpace(options[DiagnosticCaptureDirKey]),
		MaxBodyBytes: maxBodyMB * 1024 * 1024,
		PathRules:    parseDiagnosticPathRules(options[DiagnosticCapturePathsKey]),
	}
}

func DiagnosticCaptureModeValid(mode string) bool {
	mode = strings.ToLower(strings.TrimSpace(mode))
	return mode == "metadata" || mode == "full"
}

func DiagnosticCaptureMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := DiagnosticCaptureConfigFromOptions()
		if !cfg.Enabled || !cfg.shouldCapturePath(c.Request.URL.Path) {
			c.Next()
			return
		}

		flow := &DiagnosticFlow{
			TraceID: ensureDiagnosticTraceID(c.Request),
			Channel: "unknown",
			Started: time.Now(),
		}
		sequence := nextDiagnosticSequence()
		c.Set("diagnostic_flow", flow)

		writer := newDiagnosticResponseWriter(c.Writer, cfg)
		c.Writer = writer
		c.Next()

		channel := strings.TrimSpace(c.GetString("channel_name"))
		if channel == "" {
			channel = flow.Channel
		}
		flow.Channel = safeCaptureName(channel, "unknown")

		var reqBody captureBody
		if cfg.Mode == "full" {
			reqBody = getInboundRequestBody(c, cfg.MaxBodyBytes)
		}
		writeCapture(cfg, flow, sequence, "inbound", "request", map[string]any{
			"captured_at": time.Now().UTC().Format(time.RFC3339Nano),
			"role":        "inbound",
			"sequence":    sequence,
			"trace_id":    flow.TraceID,
			"channel":     flow.Channel,
			"method":      c.Request.Method,
			"path":        c.Request.URL.RequestURI(),
			"remote_addr": c.ClientIP(),
			"headers":     redactHeaders(c.Request.Header),
		}, reqBody)

		respBody := captureBody{}
		if cfg.Mode == "full" {
			respBody = writer.body()
		}
		writeCapture(cfg, flow, sequence, "inbound", "response", map[string]any{
			"captured_at":  time.Now().UTC().Format(time.RFC3339Nano),
			"role":         "inbound",
			"sequence":     sequence,
			"trace_id":     flow.TraceID,
			"channel":      flow.Channel,
			"status_code":  c.Writer.Status(),
			"duration_ms":  time.Since(flow.Started).Milliseconds(),
			"headers":      redactHeaders(c.Writer.Header()),
			"body_capture": cfg.Mode,
		}, respBody)
	}
}

func PrepareDiagnosticOutboundRequest(c *gin.Context, info *relaycommon.RelayInfo, method, url string, headers http.Header, body io.Reader) (io.Reader, *DiagnosticExchange) {
	cfg := DiagnosticCaptureConfigFromOptions()
	if !cfg.Enabled || c == nil || c.Request == nil || !cfg.shouldCapturePath(c.Request.URL.Path) {
		return body, nil
	}
	flow := getOrCreateDiagnosticFlow(c)
	channel := ""
	if info != nil && info.ChannelMeta != nil {
		channel = info.ChannelMeta.ChannelName
	}
	if channel == "" {
		channel = c.GetString("channel_name")
	}
	flow.Channel = safeCaptureName(channel, "unknown")
	sequence := nextDiagnosticSequence()

	var bodyCapture captureBody
	if cfg.Mode == "full" && body != nil {
		body, bodyCapture = readDiagnosticRequestBody(body, cfg.MaxBodyBytes)
	}
	writeCapture(cfg, flow, sequence, "outbound", "request", map[string]any{
		"captured_at": time.Now().UTC().Format(time.RFC3339Nano),
		"role":        "outbound",
		"sequence":    sequence,
		"trace_id":    flow.TraceID,
		"channel":     flow.Channel,
		"method":      method,
		"url":         url,
		"headers":     redactHeaders(headers),
	}, bodyCapture)
	return body, &DiagnosticExchange{Flow: flow, Sequence: sequence, Started: time.Now()}
}

func WrapDiagnosticOutboundResponse(c *gin.Context, resp *http.Response, exchange *DiagnosticExchange) {
	if resp == nil || exchange == nil || exchange.Flow == nil {
		return
	}
	flow := exchange.Flow
	cfg := DiagnosticCaptureConfigFromOptions()
	if !cfg.Enabled {
		return
	}
	if cfg.Mode != "full" || resp.Body == nil {
		writeCapture(cfg, flow, exchange.Sequence, "outbound", "response", map[string]any{
			"captured_at": time.Now().UTC().Format(time.RFC3339Nano),
			"role":        "outbound",
			"sequence":    exchange.Sequence,
			"trace_id":    flow.TraceID,
			"channel":     flow.Channel,
			"status_code": resp.StatusCode,
			"duration_ms": time.Since(exchange.Started).Milliseconds(),
			"headers":     redactHeaders(resp.Header),
		}, captureBody{})
		return
	}
	resp.Body = &captureReadCloser{
		ReadCloser: resp.Body,
		maxBytes:   cfg.MaxBodyBytes,
		onClose: func(data []byte, originalSize int64, truncated bool) {
			writeCapture(cfg, flow, exchange.Sequence, "outbound", "response", map[string]any{
				"captured_at": time.Now().UTC().Format(time.RFC3339Nano),
				"role":        "outbound",
				"sequence":    exchange.Sequence,
				"trace_id":    flow.TraceID,
				"channel":     flow.Channel,
				"status_code": resp.StatusCode,
				"duration_ms": time.Since(exchange.Started).Milliseconds(),
				"headers":     redactHeaders(resp.Header),
			}, captureBody{
				Data:         data,
				OriginalSize: originalSize,
				SavedSize:    int64(len(data)),
				Truncated:    truncated,
			})
		},
	}
}

func (c *captureReadCloser) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	if n > 0 {
		c.total += int64(n)
		remaining := c.maxBytes - int64(c.buf.Len())
		if remaining > 0 {
			if int64(n) > remaining {
				c.buf.Write(p[:remaining])
			} else {
				c.buf.Write(p[:n])
			}
		}
	}
	return n, err
}

func (c *captureReadCloser) Close() error {
	if !c.closed {
		c.closed = true
		if c.onClose != nil {
			c.onClose(c.buf.Bytes(), c.total, c.total > int64(c.buf.Len()))
		}
	}
	return c.ReadCloser.Close()
}

func getOrCreateDiagnosticFlow(c *gin.Context) *DiagnosticFlow {
	if value, ok := c.Get("diagnostic_flow"); ok {
		if flow, ok := value.(*DiagnosticFlow); ok && flow != nil {
			return flow
		}
	}
	flow := &DiagnosticFlow{
		TraceID: ensureDiagnosticTraceID(c.Request),
		Channel: "unknown",
		Started: time.Now(),
	}
	c.Set("diagnostic_flow", flow)
	return flow
}

func ensureDiagnosticTraceID(req *http.Request) string {
	if req != nil {
		if traceID := strings.TrimSpace(req.Header.Get(DiagnosticTraceHeader)); traceID != "" {
			return safeTraceID(traceID)
		}
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
	day := time.Now().Format("2006-01-02")
	base := filepath.Join(cfg.CaptureDir, channel, day, traceID)
	if err := os.MkdirAll(base, 0o755); err != nil {
		common.SysError("failed to create diagnostic capture dir: " + err.Error())
		return
	}
	content := buildDiagnosticCPAJSON(cfg, flow, sequence, role, part, meta, body)
	writeCombinedCapture(filepath.Join(base, "request-log.json"), flow, content)
}

func writeCombinedCapture(path string, flow *DiagnosticFlow, content diagnosticCPAJSON) {
	diagnosticCaptureWriteMu.Lock()
	defer diagnosticCaptureWriteMu.Unlock()

	combined := diagnosticCombinedCPAJSON{
		Format:    "cpa-sections-json",
		Version:   1,
		TraceID:   flow.TraceID,
		Channel:   flow.Channel,
		StartedAt: flow.Started.UTC().Format(time.RFC3339Nano),
		UpdatedAt: content.CapturedAt,
	}
	if data, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(data)) > 0 {
		_ = json.Unmarshal(data, &combined)
	}
	if combined.Format == "" {
		combined.Format = "cpa-sections-json"
	}
	if combined.Version == 0 {
		combined.Version = 1
	}
	combined.TraceID = flow.TraceID
	combined.Channel = flow.Channel
	if combined.StartedAt == "" {
		combined.StartedAt = flow.Started.UTC().Format(time.RFC3339Nano)
	}
	combined.UpdatedAt = content.CapturedAt

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
	if err == nil {
		_ = os.WriteFile(path, append(data, '\n'), 0o600)
	}
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
		TraceID:    flow.TraceID,
		Channel:    flow.Channel,
		Direction:  role,
		Section:    part,
		Sequence:   sequence,
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
			AppVersion:          common.Version,
			URL:                 stringFromMeta(meta, "path"),
			Method:              stringFromMeta(meta, "method"),
			DownstreamTransport: "http",
			UpstreamTransport:   "http",
			Timestamp:           capturedAt,
			RemoteAddr:          stringFromMeta(meta, "remote_addr"),
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
		result.Text = string(body.Data)
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
		lower := strings.ToLower(key)
		switch lower {
		case "authorization", "cookie", "set-cookie", "proxy-authorization", "x-api-key", "x-goog-api-key":
			result[key] = []string{"[REDACTED]"}
		default:
			result[key] = values
		}
	}
	return result
}
