package doubao

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingReadCloser struct {
	closed bool
}

func (r *failingReadCloser) Read(_ []byte) (int, error) {
	return 0, errors.New("upstream read failed")
}

func (r *failingReadCloser) Close() error {
	r.closed = true
	return nil
}

func TestDoResponseClosesUpstreamBodyWhenReadFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	body := &failingReadCloser{}

	_, _, taskErr := (&TaskAdaptor{}).DoResponse(context, &http.Response{Body: body}, &relaycommon.RelayInfo{})

	require.NotNil(t, taskErr)
	assert.Equal(t, "read_response_body_failed", taskErr.Code)
	assert.True(t, body.closed)
}

func TestDoResponseWritesNativeSeedanceResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set(string(constant.ContextKeyNativeSeedanceResponse), true)
	upstreamBody := []byte("{\n  \"id\": \"cgt-native-task\",\n  \"vendor_metadata\": {\"nested\": [true, 2]}\n}\n")

	taskID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(context, &http.Response{
		Body: io.NopCloser(bytes.NewReader(upstreamBody)),
	}, &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"}})

	require.Nil(t, taskErr)
	assert.Equal(t, "cgt-native-task", taskID)
	assert.Equal(t, upstreamBody, taskData)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	assert.Equal(t, upstreamBody, recorder.Body.Bytes())
}

func TestDoResponseKeepsOpenAIVideoResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	upstreamBody := []byte(`{"id":"cgt-upstream-task"}`)

	taskID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(context, &http.Response{
		Body: io.NopCloser(bytes.NewReader(upstreamBody)),
	}, &relaycommon.RelayInfo{
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
		OriginModelName: "doubao-seedance-2-0-260128",
	})

	require.Nil(t, taskErr)
	assert.Equal(t, "cgt-upstream-task", taskID)
	assert.Equal(t, upstreamBody, taskData)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"id":"task_public"`)
	assert.NotContains(t, recorder.Body.String(), "cgt-upstream-task")
}

func TestBuildTaskURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{
			name:    "root endpoint",
			baseURL: "https://ark.cn-beijing.volces.com",
			want:    "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks",
		},
		{
			name:    "plan endpoint",
			baseURL: "https://ark.cn-beijing.volces.com/api/plan",
			want:    "https://ark.cn-beijing.volces.com/api/plan/v3/contents/generations/tasks",
		},
		{
			name:    "plan endpoint with trailing slash",
			baseURL: "https://ark.cn-beijing.volces.com/api/plan/",
			want:    "https://ark.cn-beijing.volces.com/api/plan/v3/contents/generations/tasks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildTaskURL(tt.baseURL); got != tt.want {
				t.Errorf("buildTaskURL(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}

func TestConvertToRequestPayloadForwardsDuration(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:    "doubao-seedance-2-0-260128",
		Prompt:   "A dress changes from black to white",
		Duration: 4,
	}

	payload, err := adaptor.convertToRequestPayload(&req)
	require.NoError(t, err)
	require.NotNil(t, payload.Duration)
	assert.Equal(t, 4, int(*payload.Duration))
}

func TestBuildRequestBodyPreservesNativeSeedanceContentOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set(string(constant.ContextKeyNativeSeedanceResponse), true)
	context.Set("task_request", relaycommon.TaskSubmitReq{
		Model: "doubao-seedance-2-0-260128",
		Extra: map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": "First instruction"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/input.png"}},
				map[string]any{"type": "text", "text": "Second instruction"},
			},
		},
	})

	body, err := (&TaskAdaptor{}).BuildRequestBody(context, &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		ChannelMeta:   &relaycommon.ChannelMeta{},
	})
	require.NoError(t, err)
	payloadJSON, err := io.ReadAll(body)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(payloadJSON, &payload))
	content, ok := payload["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 3)
	assert.Equal(t, "First instruction", content[0].(map[string]any)["text"])
	assert.Equal(t, "image_url", content[1].(map[string]any)["type"])
	assert.Equal(t, "Second instruction", content[2].(map[string]any)["text"])
}

func TestValidateRequestAcceptsNativeSeedanceContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, "/api/plan/v3/contents/generations/tasks", strings.NewReader(`{
		"model":"doubao-seedance-2-0-260128",
		"content":[{"type":"text","text":"A dress changes from black to white"}],
		"duration":15
	}`))
	request.Header.Set("Content-Type", "application/json")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	context.Set(string(constant.ContextKeyNativeSeedanceResponse), true)

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(context, &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	})
	require.Nil(t, taskErr)

	stored, err := relaycommon.GetTaskRequest(context)
	require.NoError(t, err)
	assert.Equal(t, "doubao-seedance-2-0-260128", stored.Model)
	require.Contains(t, stored.Extra, "content")
}

func TestConvertToRequestPayloadMapsOpenAIVideoDimensionsForSeedance(t *testing.T) {
	var req relaycommon.TaskSubmitReq
	err := common.Unmarshal([]byte(`{
		"model":"doubao-seedance-2-0-fast-260128",
		"prompt":"A dress changes from black to white",
		"duration":15,
		"width":720,
		"height":1280,
		"seed":646957806,
		"fps":30,
		"n":1,
		"response_format":"url",
		"user":"new-canvas"
	}`), &req)
	require.NoError(t, err)

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req)
	require.NoError(t, err)

	payloadJSON, err := common.Marshal(payload)
	require.NoError(t, err)
	var actual map[string]any
	require.NoError(t, common.Unmarshal(payloadJSON, &actual))

	assert.Equal(t, float64(15), actual["duration"])
	assert.Equal(t, "720p", actual["resolution"])
	assert.Equal(t, "9:16", actual["ratio"])
	assert.Equal(t, float64(646957806), actual["seed"])
	assert.Equal(t, float64(30), actual["fps"])
	assert.Equal(t, float64(1), actual["n"])
	assert.Equal(t, "url", actual["response_format"])
	assert.Equal(t, "new-canvas", actual["user"])
	assert.NotContains(t, actual, "width")
	assert.NotContains(t, actual, "height")
}

func TestConvertToRequestPayloadPrefersExplicitSeedanceDimensions(t *testing.T) {
	var req relaycommon.TaskSubmitReq
	err := common.Unmarshal([]byte(`{
		"model":"doubao-seedance-2-0-fast-260128",
		"prompt":"A dress changes from black to white",
		"width":720,
		"height":1280,
		"resolution":"1080p",
		"ratio":"16:9"
	}`), &req)
	require.NoError(t, err)

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req)
	require.NoError(t, err)

	payloadJSON, err := common.Marshal(payload)
	require.NoError(t, err)
	var actual map[string]any
	require.NoError(t, common.Unmarshal(payloadJSON, &actual))

	assert.Equal(t, "1080p", actual["resolution"])
	assert.Equal(t, "16:9", actual["ratio"])
}

func TestConvertToRequestPayloadPreservesSeedanceExtraOptionsWithStringMetadata(t *testing.T) {
	var req relaycommon.TaskSubmitReq
	err := common.Unmarshal([]byte(`{
		"model":"doubao-seedance-2-0-fast-260128",
		"prompt":"A dress changes from black to white",
		"metadata":"{\"resolution\":\"1080p\"}",
		"seed":7
	}`), &req)
	require.NoError(t, err)

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req)
	require.NoError(t, err)

	payloadJSON, err := common.Marshal(payload)
	require.NoError(t, err)
	var actual map[string]any
	require.NoError(t, common.Unmarshal(payloadJSON, &actual))

	assert.Equal(t, "1080p", actual["resolution"])
	assert.Equal(t, float64(7), actual["seed"])
}

func TestConvertToRequestPayloadProtectsSeedanceCanonicalFields(t *testing.T) {
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:    "doubao-seedance-2-0-fast-260128",
		Prompt:   "A dress changes from black to white",
		Duration: 15,
		Extra: map[string]any{
			"model":    "untrusted-model",
			"content":  []any{map[string]any{"type": "text", "text": "untrusted prompt"}},
			"duration": 99,
			"seed":     7,
		},
	})
	require.NoError(t, err)

	payloadJSON, err := common.Marshal(payload)
	require.NoError(t, err)
	var actual map[string]any
	require.NoError(t, common.Unmarshal(payloadJSON, &actual))

	assert.Equal(t, "doubao-seedance-2-0-fast-260128", actual["model"])
	assert.Equal(t, float64(15), actual["duration"])
	assert.Equal(t, float64(7), actual["seed"])
	content, ok := actual["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 1)
	assert.Equal(t, "A dress changes from black to white", content[0].(map[string]any)["text"])
}

func TestConvertToRequestPayloadRejectsUnsupportedSeedanceDimensions(t *testing.T) {
	var req relaycommon.TaskSubmitReq
	err := common.Unmarshal([]byte(`{
		"model":"doubao-seedance-2-0-fast-260128",
		"prompt":"A dress changes from black to white",
		"width":800,
		"height":600
	}`), &req)
	require.NoError(t, err)

	_, err = (&TaskAdaptor{}).convertToRequestPayload(&req)
	require.ErrorContains(t, err, "unsupported Seedance dimensions 800x600")
}

func TestConvertToRequestPayloadPreventsMetadataFromOverridingSeedanceModel(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-0-fast-260128",
		Prompt: "A dress changes from black to white",
		Metadata: map[string]any{
			"model":      "untrusted-model",
			"resolution": "720p",
		},
	}

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req)
	require.NoError(t, err)

	payloadJSON, err := common.Marshal(payload)
	require.NoError(t, err)
	var actual map[string]any
	require.NoError(t, common.Unmarshal(payloadJSON, &actual))

	assert.Equal(t, "doubao-seedance-2-0-fast-260128", actual["model"])
	assert.Equal(t, "720p", actual["resolution"])
	assert.Equal(t, "untrusted-model", req.Metadata["model"])
}

func TestConvertToRequestPayloadPreventsMetadataFromSettingSeedanceDuration(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-0-fast-260128",
		Prompt: "A dress changes from black to white",
		Metadata: map[string]any{
			"duration":   99,
			"resolution": "720p",
		},
	}

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req)
	require.NoError(t, err)

	payloadJSON, err := common.Marshal(payload)
	require.NoError(t, err)
	var actual map[string]any
	require.NoError(t, common.Unmarshal(payloadJSON, &actual))

	assert.NotContains(t, actual, "duration")
	assert.Equal(t, "720p", actual["resolution"])
	assert.Equal(t, 99, req.Metadata["duration"])
}

func TestConvertToRequestPayloadRetainsSeedanceMetadataMediaWithoutMutatingMetadata(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-0-fast-260128",
		Prompt: "A dress changes from black to white",
		Images: []string{"https://example.com/input.png"},
		Metadata: map[string]any{
			"content": []any{
				map[string]any{
					"type": "text",
					"text": "untrusted prompt",
				},
				map[string]any{
					"type":      "video_url",
					"video_url": map[string]any{"url": "https://example.com/untrusted.mp4"},
				},
			},
			"seed": 7,
		},
	}

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req)
	require.NoError(t, err)

	payloadJSON, err := common.Marshal(payload)
	require.NoError(t, err)
	var actual map[string]any
	require.NoError(t, common.Unmarshal(payloadJSON, &actual))

	assert.Equal(t, float64(7), actual["seed"])
	content, ok := actual["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 3)
	assert.Equal(t, "image_url", content[0].(map[string]any)["type"])
	assert.Equal(t, "https://example.com/input.png", content[0].(map[string]any)["image_url"].(map[string]any)["url"])
	assert.Equal(t, "video_url", content[1].(map[string]any)["type"])
	assert.Equal(t, "https://example.com/untrusted.mp4", content[1].(map[string]any)["video_url"].(map[string]any)["url"])
	assert.Equal(t, "text", content[2].(map[string]any)["type"])
	assert.Equal(t, "A dress changes from black to white", content[2].(map[string]any)["text"])
	assert.Contains(t, req.Metadata, "content")
	assert.Equal(t, 7, req.Metadata["seed"])
}

func TestEstimateBillingUsesDerivedSeedanceResolution(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-0-260128",
		Prompt: "A dress changes from black to white",
		Extra: map[string]any{
			"width":  float64(1920),
			"height": float64(1080),
		},
	})

	ratios := (&TaskAdaptor{}).EstimateBilling(context, &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
	})

	assert.Equal(t, 51.0/46.0, ratios["video_input"])
}

func TestEstimateBillingRecognizesVideoInNativeSeedanceContent(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{
		Model: "doubao-seedance-2-0-260128",
		Extra: map[string]any{
			"resolution": "720p",
			"content": []any{
				map[string]any{"type": "text", "text": "Animate this clip"},
				map[string]any{"type": "video_url", "video_url": map[string]any{"url": "https://example.com/input.mp4"}},
			},
		},
	})

	ratios := (&TaskAdaptor{}).EstimateBilling(context, &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
	})

	assert.Equal(t, 28.0/46.0, ratios["video_input"])
}

func TestValidateRequestRejectsUnsupportedSeedanceDimensions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"doubao-seedance-2-0-260128",
		"prompt":"A dress changes from black to white",
		"width":800,
		"height":600
	}`))
	request.Header.Set("Content-Type", "application/json")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(context, &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	})

	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_request", taskErr.Code)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.True(t, taskErr.LocalError)
}
