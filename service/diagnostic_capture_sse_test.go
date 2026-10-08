package service

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticCaptureRequestLogPersistsStreamStatusIndependentlyFromTruncation(t *testing.T) {
	root := t.TempDir()
	body := []byte("data: {\"id\":\"stream-1\"}\n")
	streamPath := filepath.Join(root, "response.part")
	require.NoError(t, os.WriteFile(streamPath, body, 0o600))

	info := &relaycommon.RelayInfo{
		IsStream:              true,
		ReceivedResponseCount: 86,
		StreamStatus:          relaycommon.NewStreamStatus(),
	}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, fmt.Errorf("context canceled"))
	flow := &DiagnosticFlow{
		TraceID:   "client-gone-trace",
		Channel:   "channel",
		Started:   time.Date(2026, 8, 5, 10, 0, 0, 0, time.Local),
		relayInfo: info,
		Context: diagnosticRequestContextJSON{
			Status:       "error",
			StreamStatus: diagnosticStreamStatus(info),
		},
	}
	state := &diagnosticCapturePartState{
		partID:           "outbound-000001-response",
		sequence:         1,
		role:             "outbound",
		part:             "response",
		tempPath:         streamPath,
		originalSize:     int64(len(body) + 10),
		savedSize:        int64(len(body)),
		complete:         false,
		streamJSON:       []byte(`{"id":"stream-1"}`),
		streamSSEParsed:  true,
		streamAggregated: false,
		streamParseErr:   "Anthropic response aggregation failed: invalid tool input at index 0",
		streamEvents:     86,
		meta: map[string]any{
			"captured_at": "2026-08-05T02:00:00Z",
			"status_code": 200,
			"headers":     map[string][]string{"Content-Type": {"text/event-stream"}},
		},
	}
	cfg := DiagnosticCaptureConfig{Enabled: true, Mode: "full", CaptureDir: filepath.Join(root, "captures")}
	require.NoError(t, writeDiagnosticCaptureSession(cfg, flow, []*diagnosticCapturePartState{state}))

	data, err := os.ReadFile(filepath.Join(cfg.CaptureDir, "channel", "2026-08-05", "client-gone-trace", "request-log.json"))
	require.NoError(t, err)
	var combined map[string]any
	require.NoError(t, common.Unmarshal(data, &combined))

	contextValue, ok := combined["request_context"].(map[string]any)
	require.True(t, ok)
	streamStatus, ok := contextValue["stream_status"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "error", streamStatus["status"])
	assert.Equal(t, "client_gone", streamStatus["end_reason"])
	assert.Equal(t, "context canceled", streamStatus["end_error"])
	assert.Equal(t, float64(86), streamStatus["received_response_count"])
	assert.Equal(t, float64(0), streamStatus["error_count"])

	responses, ok := combined["api_responses"].([]any)
	require.True(t, ok)
	bodyValue := responses[0].(map[string]any)["response"].(map[string]any)["body"].(map[string]any)
	assert.Equal(t, true, bodyValue["truncated"])
	conversion := bodyValue["conversion"].(map[string]any)
	assert.Equal(t, true, conversion["converted"])
	assert.Equal(t, true, conversion["sse_parsed"])
	assert.Equal(t, false, conversion["response_aggregated"])
	assert.Contains(t, conversion["parse_error"], "invalid tool input")
	assert.Equal(t, float64(86), conversion["event_count"])
}

func TestDiagnosticCapturePersistsNonClientTruncationReason(t *testing.T) {
	info := &relaycommon.RelayInfo{
		IsStream:              true,
		ReceivedResponseCount: 4,
		StreamStatus:          relaycommon.NewStreamStatus(),
	}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
	status := diagnosticStreamStatus(info)
	require.NotNil(t, status)
	assert.Equal(t, "ok", status.Status)
	assert.Equal(t, "eof", status.EndReason)
	assert.Equal(t, 4, status.ReceivedResponseCount)
	assert.Equal(t, 0, status.ErrorCount)
}

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

func TestDiagnosticAggregateOpenAIResponsesSSE(t *testing.T) {
	raw := []byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"output\":[]}}\n\n" +
		"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[]}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"delta\":\"hello\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}]}}\n\n")
	events, err := diagnosticParseSSE(raw)
	require.NoError(t, err)
	response, err := diagnosticAggregateOpenAIResponsesSSE(events)
	require.NoError(t, err)
	assert.Equal(t, "resp_1", response["id"])
	assert.Equal(t, "completed", response["status"])
	output := response["output"].([]any)
	assert.Equal(t, "hello", output[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"])
}

func TestDiagnosticCaptureSSEFileConvertsOpenAIResponses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "response.part")
	raw := []byte("event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"output\":[]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"output\":[]}}\n\n")
	require.NoError(t, os.WriteFile(path, raw, 0o600))

	data, fallback, parseErr, eventCount, err := diagnosticCaptureSSEFile(path)
	require.NoError(t, err)
	require.Empty(t, fallback)
	require.Empty(t, parseErr)
	assert.Equal(t, 2, eventCount)
	var response map[string]any
	require.NoError(t, common.Unmarshal(data, &response))
	assert.Equal(t, "resp_1", response["id"])
	assert.Equal(t, "completed", response["status"])
}

func TestDiagnosticCaptureSSEFileReportsAggregationFailure(t *testing.T) {
	tests := []struct {
		name       string
		raw        []byte
		wantReason string
	}{
		{
			name: "invalid tool input",
			raw: []byte("event: message_start\n" +
				"data: {\"type\":\"message_start\",\"message\":{\"content\":[]}}\n\n" +
				"event: content_block_start\n" +
				"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"input\":{},\"name\":\"tool\"}}\n\n" +
				"event: content_block_delta\n" +
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"broken\\\":\"}}\n\n" +
				"event: content_block_stop\n" +
				"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
				"event: message_stop\n" +
				"data: {\"type\":\"message_stop\"}\n\n"),
			wantReason: "Anthropic response aggregation failed: invalid tool input at index 0: unexpected end of JSON input",
		},
		{
			name: "unsupported delta",
			raw: []byte("event: message_start\n" +
				"data: {\"type\":\"message_start\",\"message\":{\"content\":[]}}\n\n" +
				"event: content_block_start\n" +
				"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
				"event: content_block_delta\n" +
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"citations_delta\"}}\n\n" +
				"event: message_stop\n" +
				"data: {\"type\":\"message_stop\"}\n\n"),
			wantReason: "Anthropic response aggregation failed: unsupported content block delta type \"citations_delta\"",
		},
		{
			name: "missing message stop",
			raw: []byte("event: message_start\n" +
				"data: {\"type\":\"message_start\",\"message\":{\"content\":[]}}\n\n"),
			wantReason: "Anthropic response aggregation failed: SSE stream ended before message_stop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "response.part")
			require.NoError(t, os.WriteFile(path, tt.raw, 0o600))

			data, fallback, parseErr, eventCount, sseParsed, aggregated, err := diagnosticCaptureSSEFileWithStatus(path)
			require.NoError(t, err)
			require.Empty(t, fallback)
			require.NotEmpty(t, data)
			assert.Equal(t, tt.wantReason, parseErr)
			assert.Greater(t, eventCount, 0)
			assert.True(t, sseParsed)
			assert.False(t, aggregated)
		})
	}
}

func TestDiagnosticAggregateOpenAIChatSSE(t *testing.T) {
	raw := []byte("data: {\"id\":\"chat_1\",\"model\":\"gpt-test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello \"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chat_1\",\"model\":\"gpt-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"world\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n")
	events, err := diagnosticParseSSE(raw)
	require.NoError(t, err)
	response, err := diagnosticAggregateOpenAIChatSSE(events)
	require.NoError(t, err)
	assert.Equal(t, "chat_1", response["id"])
	choice := response["choices"].([]any)[0].(map[string]any)
	assert.Equal(t, "hello world", choice["message"].(map[string]any)["content"])
	assert.Equal(t, "stop", choice["finish_reason"])
}

func TestDiagnosticAggregateUnknownSSEKeepsEventData(t *testing.T) {
	events, err := diagnosticParseSSE([]byte("event: custom.delta\ndata: {\"value\":1}\n\n"))
	require.NoError(t, err)
	result := diagnosticAggregateSSE(events).(map[string]any)
	items := result["events"].([]map[string]any)
	require.Len(t, items, 1)
	assert.Equal(t, "custom.delta", items[0]["event"])
	assert.Equal(t, float64(1), items[0]["data"].(map[string]any)["value"])
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
