package doubao

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-0-fast-260128",
		Prompt: "A dress changes from black to white",
		Metadata: map[string]any{
			"model":      "untrusted-model",
			"resolution": "720p",
		},
	})
	require.NoError(t, err)

	payloadJSON, err := common.Marshal(payload)
	require.NoError(t, err)
	var actual map[string]any
	require.NoError(t, common.Unmarshal(payloadJSON, &actual))

	assert.Equal(t, "doubao-seedance-2-0-fast-260128", actual["model"])
	assert.Equal(t, "720p", actual["resolution"])
}
