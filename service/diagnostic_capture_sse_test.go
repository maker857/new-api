package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticAggregateAnthropicSSE(t *testing.T) {
	raw := []byte("event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":2,\"cache_creation_input_tokens\":11}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" + "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello \"}}\n\n" + "event: content_block_delta\n" + "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"world\"}}\n\n" + "event: content_block_stop\n" + "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" + "event: message_delta\n" + "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n" + "event: message_stop\n" + "data: {\"type\":\"message_stop\"}\n\n")

	events, err := diagnosticParseSSE(raw)
	require.NoError(t, err)
	message, err := diagnosticAggregateAnthropicSSE(events)
	require.NoError(t, err)

	content, ok := message["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 1)
	block, ok := content[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "hello world", block["text"])
	assert.Equal(t, "end_turn", message["stop_reason"])
	usage := message["usage"].(map[string]any)
	assert.Equal(t, float64(2), usage["input_tokens"])
	assert.Equal(t, float64(11), usage["cache_creation_input_tokens"])
	assert.Equal(t, float64(3), usage["output_tokens"])
}

func TestDiagnosticAggregateAnthropicSSERejectsIncompleteStream(t *testing.T) {
	events, err := diagnosticParseSSE([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"content\":[]}}\n\n"))
	require.NoError(t, err)
	_, err = diagnosticAggregateAnthropicSSE(events)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "message_stop")
}

func TestDiagnosticCaptureSSEWritesConvertedResponse(t *testing.T) {
	root := t.TempDir()
	stream := []byte("event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":2,\"cache_creation_input_tokens\":11}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"complete\"}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n")
	streamPath := filepath.Join(root, "response.part")
	require.NoError(t, os.WriteFile(streamPath, stream, 0o600))

	streamJSON, streamText, parseErr, eventCount, err := diagnosticCaptureSSEFile(streamPath)
	require.NoError(t, err)
	require.NotEmpty(t, streamJSON)
	require.Empty(t, streamText)
	require.Empty(t, parseErr)

	flow := &DiagnosticFlow{
		TraceID: "sse-trace",
		Channel: "channel",
		Started: time.Date(2026, 8, 5, 10, 0, 0, 0, time.Local),
	}
	state := &diagnosticCapturePartState{
		partID:       "outbound-000001-response",
		sequence:     1,
		role:         "outbound",
		part:         "response",
		tempPath:     streamPath,
		originalSize: int64(len(stream)),
		savedSize:    int64(len(stream)),
		complete:     true,
		streamJSON:   streamJSON,
		streamEvents: eventCount,
		meta: map[string]any{
			"captured_at": "2026-08-05T02:00:00Z",
			"status_code": 200,
			"headers":     map[string][]string{"Content-Type": {"text/event-stream"}},
		},
	}
	cfg := DiagnosticCaptureConfig{Enabled: true, Mode: "full", CaptureDir: filepath.Join(root, "captures")}
	require.NoError(t, writeDiagnosticCaptureSession(cfg, flow, []*diagnosticCapturePartState{state}))

	data, err := os.ReadFile(filepath.Join(cfg.CaptureDir, "channel", "2026-08-05", "sse-trace", "request-log.json"))
	require.NoError(t, err)
	var combined diagnosticCombinedCPAJSON
	require.NoError(t, common.Unmarshal(data, &combined))
	require.Len(t, combined.APIResponses, 1)
	body := combined.APIResponses[0].Response.Body
	assert.Equal(t, "parsed", body.Mode)
	assert.Equal(t, "json", body.Encoding)
	require.NotNil(t, body.Conversion)
	assert.True(t, body.Conversion.Converted)
	assert.Equal(t, "sse", body.Conversion.Source)
	assert.Equal(t, eventCount, body.Conversion.EventCount)
	message, ok := body.JSON.(map[string]any)
	require.True(t, ok)
	usage, ok := message["usage"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(2), usage["input_tokens"])
	assert.Equal(t, float64(11), usage["cache_creation_input_tokens"])
	assert.Equal(t, float64(3), usage["output_tokens"])
}
