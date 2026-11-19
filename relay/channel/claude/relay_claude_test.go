package claude

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/tokenkit"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func commonPointer[T any](value T) *T {
	return &value
}

func TestClaudeStreamFinalUsageEstimatesOnlyUncountedUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, startUsage, deltaUsage, stopReason string // no deltaUsage: the stream broke before message_delta
		streamText                               bool
		wantInput, wantOutput                    int
		wantLocalCount                           bool
		estimatePrompt                           int
		wantEstimated                            *bool
	}{
		{name: "cache-only refusal keeps zeros", startUsage: `{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":20000,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":20000}}`, deltaUsage: `{"output_tokens":0}`, stopReason: "refusal"},
		{name: "broken stream keeps cache-only input", startUsage: `{"input_tokens":0,"output_tokens":1,"cache_read_input_tokens":50000}`, streamText: true, wantOutput: 2, wantLocalCount: true},
		{name: "zeros after streamed text are estimated", startUsage: `{"input_tokens":0,"output_tokens":0}`, deltaUsage: `{"output_tokens":0}`, stopReason: "end_turn", streamText: true, wantInput: 1200, wantOutput: 2, wantLocalCount: true},
		{name: "zero delta input keeps start input", startUsage: `{"input_tokens":1000,"output_tokens":1}`, deltaUsage: `{"input_tokens":0,"output_tokens":50}`, stopReason: "end_turn", streamText: true, wantInput: 1000, wantOutput: 50},
		{name: "empty upstream uses request estimate", estimatePrompt: 13_808_340, wantInput: 13_808_340, wantLocalCount: true, wantEstimated: commonPointer(true)},
		{name: "terminal zero usage uses request estimate", startUsage: `{"input_tokens":0,"output_tokens":0}`, deltaUsage: `{"output_tokens":0}`, stopReason: "end_turn", estimatePrompt: 13_808_340, wantInput: 13_808_340, wantLocalCount: true, wantEstimated: commonPointer(false)},
		{name: "upstream usage overrides request estimate", startUsage: `{"input_tokens":322172,"output_tokens":0}`, deltaUsage: `{"output_tokens":0}`, stopReason: "end_turn", estimatePrompt: 13_808_340, wantInput: 322_172, wantEstimated: commonPointer(false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-opus-4-8"}}
			estimate := tc.estimatePrompt
			if estimate == 0 {
				estimate = 1200
			}
			info.SetEstimatePromptTokens(estimate)
			state := &ClaudeResponseInfo{Usage: &dto.Usage{}}
			if tc.startUsage != "" {
				start := fmt.Sprintf(`{"type":"message_start","message":{"id":"msg_1","model":"claude-opus-4-8","usage":%s}}`, tc.startUsage)
				require.Nil(t, HandleStreamResponseData(c, info, state, start))
			}
			if tc.streamText {
				require.Nil(t, HandleStreamResponseData(c, info, state, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`))
			}
			if tc.deltaUsage != "" {
				delta := fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%q},"usage":%s}`, tc.stopReason, tc.deltaUsage)
				require.Nil(t, HandleStreamResponseData(c, info, state, delta))
				require.Nil(t, HandleStreamResponseData(c, info, state, `{"type":"message_stop"}`))
			}
			HandleStreamFinalResponse(c, info, state)
			assert.Equal(t, tc.wantInput, state.Usage.PromptTokens)
			assert.Equal(t, tc.wantOutput, state.Usage.CompletionTokens)
			assert.Equal(t, tc.wantLocalCount, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
			canonical, ok := state.Usage.BillingUsage.CanonicalUsage()
			require.True(t, ok)
			assert.Equal(t, tc.wantInput, canonical.PromptTokens, "settlement must bill the same input")
			assert.Equal(t, tc.wantOutput, canonical.CompletionTokens)
			if tc.wantEstimated != nil {
				assert.Equal(t, *tc.wantEstimated, state.Usage.BillingUsage.Estimated)
			}
		})
	}
}

func TestResponseOpenAI2ClaudeToolUseInputIsObject(t *testing.T) {
	tests := []struct {
		name string
		args string
		want map[string]any
	}{
		{name: "object", args: `{"q":"x"}`, want: map[string]any{"q": "x"}},
		{name: "empty", args: "", want: map[string]any{}},
		{name: "invalid", args: "{", want: map[string]any{}},
		{name: "null", args: "null", want: map[string]any{}},
		{name: "array", args: `["x"]`, want: map[string]any{}},
		{name: "string", args: `"x"`, want: map[string]any{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := dto.Message{Role: "assistant"}
			msg.SetToolCalls([]dto.ToolCallRequest{
				{
					ID:   "call_1",
					Type: "function",
					Function: dto.FunctionRequest{
						Name:      "lookup",
						Arguments: tt.args,
					},
				},
			})
			resp := relayconvert.ResponseOpenAI2Claude(&dto.OpenAITextResponse{
				Id:    "chatcmpl_1",
				Model: "gpt-test",
				Choices: []dto.OpenAITextResponseChoice{
					{Message: msg, FinishReason: "tool_calls"},
				},
			}, nil)

			require.Len(t, resp.Content, 1)
			assert.Equal(t, "tool_use", resp.Content[0].Type)
			encoded, err := common.Marshal(resp.Content[0].Input)
			require.NoError(t, err)
			want, err := common.Marshal(tt.want)
			require.NoError(t, err)
			assert.Equal(t, string(want), string(encoded))
		})
	}
}

func TestFormatClaudeResponseInfo_MessageStart(t *testing.T) {
	claudeInfo := &ClaudeResponseInfo{
		Usage: &dto.Usage{},
	}
	claudeResponse := &dto.ClaudeResponse{
		Type: "message_start",
		Message: &dto.ClaudeMediaMessage{
			Id:    "msg_123",
			Model: "claude-3-5-sonnet",
			Usage: &dto.ClaudeUsage{
				InputTokens:              100,
				OutputTokens:             1,
				CacheCreationInputTokens: 50,
				CacheReadInputTokens:     30,
			},
		},
	}

	ok := FormatClaudeResponseInfo(claudeResponse, nil, claudeInfo)
	if !ok {
		t.Fatal("expected true")
	}
	if claudeInfo.Usage.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d, want 100", claudeInfo.Usage.PromptTokens)
	}
	if claudeInfo.Usage.PromptTokensDetails.CachedTokens != 30 {
		t.Errorf("CachedTokens = %d, want 30", claudeInfo.Usage.PromptTokensDetails.CachedTokens)
	}
	if claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens != 50 {
		t.Errorf("CachedCreationTokens = %d, want 50", claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens)
	}
	if claudeInfo.ResponseId != "msg_123" {
		t.Errorf("ResponseId = %s, want msg_123", claudeInfo.ResponseId)
	}
	if claudeInfo.Model != "claude-3-5-sonnet" {
		t.Errorf("Model = %s, want claude-3-5-sonnet", claudeInfo.Model)
	}
}

func TestFormatClaudeResponseInfo_MessageDelta_FullUsage(t *testing.T) {
	// message_start 先积累 usage
	claudeInfo := &ClaudeResponseInfo{
		Usage: &dto.Usage{
			PromptTokens: 100,
			PromptTokensDetails: dto.InputTokenDetails{
				CachedTokens:         30,
				CachedCreationTokens: 50,
			},
			CompletionTokens: 1,
		},
	}

	// message_delta 带完整 usage（原生 Anthropic 场景）
	claudeResponse := &dto.ClaudeResponse{
		Type: "message_delta",
		Usage: &dto.ClaudeUsage{
			InputTokens:              100,
			OutputTokens:             200,
			CacheCreationInputTokens: 50,
			CacheReadInputTokens:     30,
		},
	}

	ok := FormatClaudeResponseInfo(claudeResponse, nil, claudeInfo)
	if !ok {
		t.Fatal("expected true")
	}
	if claudeInfo.Usage.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d, want 100", claudeInfo.Usage.PromptTokens)
	}
	if claudeInfo.Usage.CompletionTokens != 200 {
		t.Errorf("CompletionTokens = %d, want 200", claudeInfo.Usage.CompletionTokens)
	}
	if claudeInfo.Usage.TotalTokens != 300 {
		t.Errorf("TotalTokens = %d, want 300", claudeInfo.Usage.TotalTokens)
	}
	if !claudeInfo.Done {
		t.Error("expected Done = true")
	}
}

func TestFormatClaudeResponseInfo_MessageDelta_OnlyOutputTokens(t *testing.T) {
	// 模拟 Bedrock: message_start 已积累 usage
	claudeInfo := &ClaudeResponseInfo{
		Usage: &dto.Usage{
			PromptTokens: 100,
			PromptTokensDetails: dto.InputTokenDetails{
				CachedTokens:         30,
				CachedCreationTokens: 50,
			},
			CompletionTokens:            1,
			ClaudeCacheCreation5mTokens: 10,
			ClaudeCacheCreation1hTokens: 20,
		},
	}

	// Bedrock 的 message_delta 只有 output_tokens，缺少 input_tokens 和 cache 字段
	claudeResponse := &dto.ClaudeResponse{
		Type: "message_delta",
		Usage: &dto.ClaudeUsage{
			OutputTokens: 200,
			// InputTokens, CacheCreationInputTokens, CacheReadInputTokens 都是 0
		},
	}

	ok := FormatClaudeResponseInfo(claudeResponse, nil, claudeInfo)
	if !ok {
		t.Fatal("expected true")
	}
	// PromptTokens 应保持 message_start 的值（因为 message_delta 的 InputTokens=0，不更新）
	if claudeInfo.Usage.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d, want 100", claudeInfo.Usage.PromptTokens)
	}
	if claudeInfo.Usage.CompletionTokens != 200 {
		t.Errorf("CompletionTokens = %d, want 200", claudeInfo.Usage.CompletionTokens)
	}
	if claudeInfo.Usage.TotalTokens != 300 {
		t.Errorf("TotalTokens = %d, want 300", claudeInfo.Usage.TotalTokens)
	}
	// cache 字段应保持 message_start 的值
	if claudeInfo.Usage.PromptTokensDetails.CachedTokens != 30 {
		t.Errorf("CachedTokens = %d, want 30", claudeInfo.Usage.PromptTokensDetails.CachedTokens)
	}
	if claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens != 50 {
		t.Errorf("CachedCreationTokens = %d, want 50", claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens)
	}
	if claudeInfo.Usage.ClaudeCacheCreation5mTokens != 10 {
		t.Errorf("ClaudeCacheCreation5mTokens = %d, want 10", claudeInfo.Usage.ClaudeCacheCreation5mTokens)
	}
	if claudeInfo.Usage.ClaudeCacheCreation1hTokens != 20 {
		t.Errorf("ClaudeCacheCreation1hTokens = %d, want 20", claudeInfo.Usage.ClaudeCacheCreation1hTokens)
	}
	if !claudeInfo.Done {
		t.Error("expected Done = true")
	}
}

func TestFormatClaudeResponseInfo_NilClaudeInfo(t *testing.T) {
	claudeResponse := &dto.ClaudeResponse{Type: "message_start"}
	ok := FormatClaudeResponseInfo(claudeResponse, nil, nil)
	if ok {
		t.Error("expected false for nil claudeInfo")
	}
}

func TestFormatClaudeResponseInfo_ContentBlockDelta(t *testing.T) {
	text := "hello"
	claudeInfo := &ClaudeResponseInfo{
		Usage:        &dto.Usage{},
		ResponseText: strings.Builder{},
	}
	claudeResponse := &dto.ClaudeResponse{
		Type: "content_block_delta",
		Delta: &dto.ClaudeMediaMessage{
			Text: &text,
		},
	}

	ok := FormatClaudeResponseInfo(claudeResponse, nil, claudeInfo)
	if !ok {
		t.Fatal("expected true")
	}
	if claudeInfo.ResponseText.String() != "hello" {
		t.Errorf("ResponseText = %q, want %q", claudeInfo.ResponseText.String(), "hello")
	}
}

// A stream cut off during an agent tool call must still estimate the call's
// output tokens when the final usage never arrives.
func TestClaudeStreamFallbackCountsInterruptedToolUse(t *testing.T) {
	claudeInfo := &ClaudeResponseInfo{Usage: &dto.Usage{}}
	events := []*dto.ClaudeResponse{
		{Type: "content_block_start", ContentBlock: &dto.ClaudeMediaMessage{Type: "tool_use", Id: "toolu_1", Name: "read_file"}},
		{Type: "content_block_delta", Delta: &dto.ClaudeMediaMessage{Type: "input_json_delta", PartialJson: commonPointer(`{"path":`)}},
		{Type: "content_block_delta", Delta: &dto.ClaudeMediaMessage{Type: "input_json_delta", PartialJson: commonPointer(`"main.go"}`)}},
	}
	for _, event := range events {
		require.True(t, FormatClaudeResponseInfo(event, nil, claudeInfo))
	}
	require.Equal(t, `read_file{"path":"main.go"}`, claudeInfo.ResponseText.String())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-sonnet-5"},
	}
	info.SetEstimatePromptTokens(100)
	HandleStreamFinalResponse(c, info, claudeInfo)

	// Text estimate plus the per-tool_use framing the upstream bills.
	assert.Equal(t, tokenkit.Estimate("claude-sonnet-5", `read_file{"path":"main.go"}`)+40, claudeInfo.Usage.CompletionTokens)
	assert.Equal(t, 100, claudeInfo.Usage.PromptTokens)
}

func TestBuildOpenAIStyleUsageFromClaudeUsage(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 20,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         30,
			CachedCreationTokens: 50,
		},
		ClaudeCacheCreation5mTokens: 10,
		ClaudeCacheCreation1hTokens: 20,
		UsageSemantic:               "anthropic",
	}

	openAIUsage := buildOpenAIStyleUsageFromClaudeUsage(usage)

	if openAIUsage.PromptTokens != 180 {
		t.Fatalf("PromptTokens = %d, want 180", openAIUsage.PromptTokens)
	}
	if openAIUsage.InputTokens != 180 {
		t.Fatalf("InputTokens = %d, want 180", openAIUsage.InputTokens)
	}
	if openAIUsage.TotalTokens != 200 {
		t.Fatalf("TotalTokens = %d, want 200", openAIUsage.TotalTokens)
	}
	if openAIUsage.UsageSemantic != "openai" {
		t.Fatalf("UsageSemantic = %s, want openai", openAIUsage.UsageSemantic)
	}
	if openAIUsage.UsageSource != "anthropic" {
		t.Fatalf("UsageSource = %s, want anthropic", openAIUsage.UsageSource)
	}
}

func TestBuildOpenAIStyleUsageFromClaudeUsagePreservesCacheCreationRemainder(t *testing.T) {
	tests := []struct {
		name                    string
		cachedCreationTokens    int
		cacheCreationTokens5m   int
		cacheCreationTokens1h   int
		expectedTotalInputToken int
	}{
		{
			name:                    "prefers aggregate when it includes remainder",
			cachedCreationTokens:    50,
			cacheCreationTokens5m:   10,
			cacheCreationTokens1h:   20,
			expectedTotalInputToken: 180,
		},
		{
			name:                    "falls back to split tokens when aggregate missing",
			cachedCreationTokens:    0,
			cacheCreationTokens5m:   10,
			cacheCreationTokens1h:   20,
			expectedTotalInputToken: 160,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage := &dto.Usage{
				PromptTokens:     100,
				CompletionTokens: 20,
				PromptTokensDetails: dto.InputTokenDetails{
					CachedTokens:         30,
					CachedCreationTokens: tt.cachedCreationTokens,
				},
				ClaudeCacheCreation5mTokens: tt.cacheCreationTokens5m,
				ClaudeCacheCreation1hTokens: tt.cacheCreationTokens1h,
				UsageSemantic:               "anthropic",
			}

			openAIUsage := buildOpenAIStyleUsageFromClaudeUsage(usage)

			if openAIUsage.PromptTokens != tt.expectedTotalInputToken {
				t.Fatalf("PromptTokens = %d, want %d", openAIUsage.PromptTokens, tt.expectedTotalInputToken)
			}
			if openAIUsage.InputTokens != tt.expectedTotalInputToken {
				t.Fatalf("InputTokens = %d, want %d", openAIUsage.InputTokens, tt.expectedTotalInputToken)
			}
		})
	}
}

func TestBuildOpenAIStyleUsageFromClaudeUsageDefaultsAggregateCacheCreationTo5m(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 20,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         30,
			CachedCreationTokens: 50,
		},
		UsageSemantic: "anthropic",
	}

	openAIUsage := buildOpenAIStyleUsageFromClaudeUsage(usage)

	require.Equal(t, 50, openAIUsage.ClaudeCacheCreation5mTokens)
	require.Equal(t, 0, openAIUsage.ClaudeCacheCreation1hTokens)
}

func applyOpenAIChatReasoningThroughHandlerOrder(t *testing.T, original dto.GeneralOpenAIRequest) (*dto.GeneralOpenAIRequest, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		OriginModelName: original.Model,
		Request:         &original,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: original.Model,
		},
	}
	outbound, err := common.DeepCopy(&original)
	require.NoError(t, err)
	require.NoError(t, helper.ModelMappedHelper(c, info, outbound))
	err = helper.ApplyReasoningModelSuffix(nil, info, outbound)
	require.NoError(t, err)
	return outbound, info
}

func TestOpenAIChatRequestToClaudeMessages_ClaudeOpus48HighUsesAdaptiveThinking(t *testing.T) {
	original := dto.GeneralOpenAIRequest{
		Model:       "claude-opus-4-8-high",
		Temperature: commonPointer(0.7),
		TopP:        commonPointer(0.9),
		TopK:        commonPointer(40),
		Messages: []dto.Message{
			{
				Role:    "user",
				Content: "hello",
			},
		},
	}

	outbound, info := applyOpenAIChatReasoningThroughHandlerOrder(t, original)
	claudeRequest, err := relayconvert.OpenAIChatRequestToClaudeMessages(nil, info, *outbound)
	require.NoError(t, err)
	require.Equal(t, "claude-opus-4-8", claudeRequest.Model)
	require.NotNil(t, claudeRequest.Thinking)
	require.Equal(t, "adaptive", claudeRequest.Thinking.Type)
	require.Equal(t, "summarized", claudeRequest.Thinking.Display)
	require.JSONEq(t, `{"effort":"high"}`, string(claudeRequest.OutputConfig))
	require.Nil(t, claudeRequest.Temperature)
	require.Nil(t, claudeRequest.TopP)
	require.Nil(t, claudeRequest.TopK)
}

func TestOpenAIChatRequestToClaudeMessages_ClaudeOpus48ThinkingUsesAdaptiveHighEffort(t *testing.T) {
	original := dto.GeneralOpenAIRequest{
		Model:       "claude-opus-4-8-thinking",
		Temperature: commonPointer(0.7),
		TopP:        commonPointer(0.9),
		TopK:        commonPointer(40),
		Messages: []dto.Message{
			{
				Role:    "user",
				Content: "hello",
			},
		},
	}

	outbound, info := applyOpenAIChatReasoningThroughHandlerOrder(t, original)
	claudeRequest, err := relayconvert.OpenAIChatRequestToClaudeMessages(nil, info, *outbound)
	require.NoError(t, err)
	require.Equal(t, "claude-opus-4-8", claudeRequest.Model)
	require.NotNil(t, claudeRequest.Thinking)
	require.Equal(t, "adaptive", claudeRequest.Thinking.Type)
	require.Equal(t, "summarized", claudeRequest.Thinking.Display)
	require.JSONEq(t, `{"effort":"high"}`, string(claudeRequest.OutputConfig))
	require.Nil(t, claudeRequest.Temperature)
	require.Nil(t, claudeRequest.TopP)
	require.Nil(t, claudeRequest.TopK)
}

func TestClaudeResponsePassesInputTransformationsThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-opus-5-5"},
	}
	transformations := `"input_transformations":[{"type":"thinking_dropped","path":"messages.1.content.0","reason":"prefix_binding_mismatch"}]`

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	body := `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"hi"}],` +
		`"stop_reason":"end_turn",` + transformations + `,"usage":{"input_tokens":5,"output_tokens":1}}`
	require.Nil(t, HandleClaudeResponseData(c, info, &ClaudeResponseInfo{Usage: &dto.Usage{}}, nil, []byte(body)))
	assert.Equal(t, body, w.Body.String())

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	event := `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],` +
		transformations + `,"usage":{"input_tokens":5,"output_tokens":1}}}`
	require.Nil(t, HandleStreamResponseData(c, info, &ClaudeResponseInfo{Usage: &dto.Usage{}}, event))
	assert.Contains(t, w.Body.String(), "data: "+event+"\n")
}

// Compaction usage is reported only in usage.iterations; the payloads below are
// the shapes the Anthropic API returned for on-demand and threshold compaction.
func TestClaudeCompactionUsageBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const onDemandUsage = `{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,` +
		`"iterations":[{"type":"compaction","input_tokens":108,"output_tokens":408,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}]}`
	const thresholdUsage = `{"input_tokens":23000,"output_tokens":1000,"cache_read_input_tokens":500,"cache_creation_input_tokens":0,` +
		`"iterations":[{"type":"compaction","input_tokens":180000,"output_tokens":3500,"cache_read_input_tokens":200,"cache_creation_input_tokens":40,"cache_creation":{"ephemeral_5m_input_tokens":40,"ephemeral_1h_input_tokens":0}},` +
		`{"type":"message","input_tokens":23000,"output_tokens":1000,"cache_read_input_tokens":500,"cache_creation_input_tokens":0}]}`
	// Returned by api.anthropic.com for a threshold compaction request on
	// claude-fable-5-2: the top-level usage equals the message iteration only.
	const officialThresholdUsage = `{"input_tokens":630,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":346,` +
		`"iterations":[{"type":"compaction","input_tokens":95026,"output_tokens":1024,"cache_read_input_tokens":0,"cache_creation_input_tokens":0},` +
		`{"type":"message","input_tokens":630,"output_tokens":346,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}]}`
	const advisorUsage = `{"input_tokens":1760,"output_tokens":531,"iterations":[{"type":"message","input_tokens":412,"output_tokens":89},` +
		`{"type":"advisor_message","model":"claude-opus-5-5","input_tokens":823,"output_tokens":1612},{"type":"message","input_tokens":1348,"output_tokens":442}]}`

	for _, tc := range []struct {
		name                                  string
		enabled                               bool
		relayFormat                           types.RelayFormat
		usage                                 string
		wantInput, wantOutput, wantCR, wantCC int
		wantCompaction                        bool
	}{
		{name: "on-demand compaction is billed", enabled: true, relayFormat: types.RelayFormatClaude, usage: onDemandUsage, wantInput: 108, wantOutput: 408, wantCompaction: true},
		{name: "official threshold compaction", enabled: true, relayFormat: types.RelayFormatClaude, usage: officialThresholdUsage, wantInput: 95656, wantOutput: 1370, wantCompaction: true},
		{name: "threshold compaction adds to the reply", enabled: true, relayFormat: types.RelayFormatClaude, usage: thresholdUsage, wantInput: 203000, wantOutput: 4500, wantCR: 700, wantCC: 40, wantCompaction: true},
		{name: "switch off keeps top-level usage", relayFormat: types.RelayFormatClaude, usage: thresholdUsage, wantInput: 23000, wantOutput: 1000, wantCR: 500},
		{name: "advisor and message entries are not added", enabled: true, relayFormat: types.RelayFormatClaude, usage: advisorUsage, wantInput: 1760, wantOutput: 531},
		{name: "converted client format is left alone", enabled: true, relayFormat: types.RelayFormatOpenAI, usage: thresholdUsage, wantInput: 23000, wantOutput: 1000, wantCR: 500},
	} {
		newInfo := func() *relaycommon.RelayInfo {
			return &relaycommon.RelayInfo{RelayFormat: tc.relayFormat, ChannelMeta: &relaycommon.ChannelMeta{
				UpstreamModelName:    "claude-fable-5-2",
				ChannelOtherSettings: dto.ChannelOtherSettings{ClaudeCompactionUsageEnabled: tc.enabled},
			}}
		}
		assertBilled := func(t *testing.T, info *relaycommon.RelayInfo, usage *dto.Usage) {
			assert.Equal(t, tc.wantInput, usage.PromptTokens)
			assert.Equal(t, tc.wantOutput, usage.CompletionTokens)
			assert.Equal(t, tc.wantCR, usage.PromptTokensDetails.CachedTokens)
			assert.Equal(t, tc.wantCC, usage.PromptTokensDetails.CachedCreationTokens)
			canonical, ok := usage.BillingUsage.CanonicalUsage()
			require.True(t, ok)
			assert.Equal(t, tc.wantInput, canonical.PromptTokens, "settlement must bill the same input")
			assert.Equal(t, tc.wantOutput, canonical.CompletionTokens)
			assert.Equal(t, tc.wantCR, canonical.PromptTokensDetails.CachedTokens)
			assert.Equal(t, tc.wantCC, canonical.PromptTokensDetails.CachedCreationTokens)
			assert.Equal(t, tc.wantCompaction, info.CompactionUsage != nil)
		}

		t.Run(tc.name+"/non-stream", func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			info := newInfo()
			state := &ClaudeResponseInfo{Usage: &dto.Usage{}}
			body := `{"id":"msg_1","type":"message","role":"assistant","model":"claude-fable-5-2",` +
				`"content":[{"type":"compaction","content":"summary","signature":"sig"}],"stop_reason":"compaction","usage":` + tc.usage + `}`
			require.Nil(t, HandleClaudeResponseData(c, info, state, nil, []byte(body)))
			assertBilled(t, info, state.Usage)
			if tc.relayFormat == types.RelayFormatClaude {
				assert.Equal(t, body, w.Body.String(), "the client receives the upstream usage unchanged")
			}
		})

		t.Run(tc.name+"/stream", func(t *testing.T) {
			if tc.relayFormat != types.RelayFormatClaude {
				t.Skip("converted stream formats are covered by the non-stream case")
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			info := newInfo()
			info.SetEstimatePromptTokens(48)
			state := &ClaudeResponseInfo{Usage: &dto.Usage{}}
			delta := `{"type":"message_delta","delta":{"stop_reason":"compaction"},"usage":` + tc.usage + `}`
			for _, event := range []string{
				`{"type":"message_start","message":{"id":"msg_1","model":"claude-fable-5-2","content":[],"usage":{"input_tokens":0,"output_tokens":0}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"compaction","content":"summary","signature":"sig"}}`,
				`{"type":"content_block_stop","index":0}`,
				delta,
				`{"type":"message_stop"}`,
			} {
				require.Nil(t, HandleStreamResponseData(c, info, state, event))
			}
			HandleStreamFinalResponse(c, info, state)
			assertBilled(t, info, state.Usage)
			assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens), "real compaction usage must not fall back to estimation")
			for line := range strings.SplitSeq(w.Body.String(), "\n") {
				event, ok := strings.CutPrefix(line, "data: ")
				if !ok || !strings.Contains(event, `"message_delta"`) {
					continue
				}
				assert.Equal(t, gjson.Get(delta, "usage.input_tokens").Int(), gjson.Get(event, "usage.input_tokens").Int(),
					"the client's top-level usage must not include compaction")
				assert.Equal(t, gjson.Get(delta, "usage.iterations").Raw, gjson.Get(event, "usage.iterations").Raw)
			}
		})
	}
}
