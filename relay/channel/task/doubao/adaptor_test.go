package doubao

import (
	"testing"

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
