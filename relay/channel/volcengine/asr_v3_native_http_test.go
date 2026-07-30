package volcengine

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildV3ASRAuthHeaders(t *testing.T) {
	headers, err := buildV3ASRAuthHeaders("app-key|access-key", dto.VolcASRConfig{
		Protocol: dto.VolcASRProtocolV3AUC, ResourceID: "volc.seedasr.auc", AuthMode: dto.VolcASRAuthModeLegacy,
	}, "req-1")
	require.NoError(t, err)
	require.Equal(t, "app-key", headers.Get("X-Api-App-Key"))
	require.Equal(t, "access-key", headers.Get("X-Api-Access-Key"))
	require.Equal(t, "volc.seedasr.auc", headers.Get("X-Api-Resource-Id"))
	require.Equal(t, "req-1", headers.Get("X-Api-Request-Id"))
	require.Equal(t, "-1", headers.Get("X-Api-Sequence"))
}

func TestGetV3ASREndpoint(t *testing.T) {
	require.Equal(t, "https://openspeech.bytedance.com/api/v3/auc/bigmodel/submit", getV3ASREndpoint("/api/v3/auc/bigmodel/submit"))
	require.Equal(t, "https://openspeech.bytedance.com/api/v3/auc/bigmodel/query", getV3ASREndpoint("/api/v3/auc/bigmodel/query"))
}

func TestHandleNativeASRHTTPPreservesRequestAndResponse(t *testing.T) {
	rawRequest := `{"audio":{"url":"https://example.com/test.wav"},"request":{"show_utterances":true}}`
	rawResponse := `{"result":{"text":"你好","utterances":[{"words":[{"text":"你","start_time":0,"end_time":120}]}]}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, rawRequest, string(body))
		assert.Equal(t, "api-key", r.Header.Get("X-Api-Key"))
		assert.Equal(t, "volc.seedasr.auc", r.Header.Get("X-Api-Resource-Id"))
		assert.Equal(t, "-1", r.Header.Get("X-Api-Sequence"))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Api-Status-Code", "20000000")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(rawResponse))
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/auc/bigmodel/query", strings.NewReader(rawRequest))
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ApiType:        constant.APITypeVolcEngine,
		ApiKey:         "api-key",
		ChannelSetting: dto.ChannelSettings{},
	}}
	request := &dto.VolcengineASRNativeRequest{Model: "volc.seedasr.auc", RawBody: []byte(rawRequest)}
	cfg := dto.VolcASRConfig{Protocol: dto.VolcASRProtocolV3AUC, ResourceID: "volc.seedasr.auc", AuthMode: dto.VolcASRAuthModeNewConsole}

	apiErr := handleNativeASRHTTP(c, upstream.URL, request, info, cfg)
	require.Nil(t, apiErr)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, rawResponse, recorder.Body.String())
	assert.Equal(t, "20000000", recorder.Header().Get("X-Api-Status-Code"))
	assert.True(t, c.GetBool("native_response_committed"))
}
