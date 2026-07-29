package volcengine

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeTTSHTTPForwardsNDJSONAndPreservesUsage(t *testing.T) {
	const gatewayToken = "gateway-token"
	const channelKey = "channel-console-key"
	const requestID = "11111111-1111-4111-8111-111111111111"
	const connectID = "22222222-2222-4222-8222-222222222222"
	lines := []string{
		`{"code":0,"data":"YQ==","sentence":{"words":[{"word":"你","startTime":0.12,"endTime":0.31,"confidence":0.98}]}}`,
		`{"code":0,"data":"Yg==","sentence":{"phonemes":[{"phoneme":"ni3","startTime":0.31,"endTime":0.52}]},"usage":{"text_words":23}}`,
		`{"code":20000000,"message":"finished","usage":{"text_words":23}}`,
	}
	var receivedBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, channelKey, r.Header.Get("X-Api-Key"))
		assert.NotEqual(t, gatewayToken, r.Header.Get("X-Api-Key"))
		assert.Equal(t, "seed-tts-2.0", r.Header.Get("X-Api-Resource-Id"))
		assert.Equal(t, requestID, r.Header.Get("X-Api-Request-Id"))
		assert.Equal(t, connectID, r.Header.Get("X-Api-Connect-Id"))
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Tt-Logid", "native-log-id")
		_, _ = io.WriteString(w, strings.Join(lines, "\n")+"\n")
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/tts/unidirectional", strings.NewReader(`{"req_params":{"text":"你好","speaker":"seed-voice","audio_params":{"enable_subtitle":true}}}`))
	c.Request.Header.Set("X-Api-Request-Id", requestID)
	c.Request.Header.Set("X-Api-Connect-Id", connectID)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: channelKey}}
	request := &dto.VolcengineTTSNativeRequest{RawBody: []byte(`{"req_params":{"text":"你好","speaker":"seed-voice","audio_params":{"enable_subtitle":true}}}`)}
	cfg := dto.VolcTTSConfig{Protocol: dto.VolcTTSProtocolV3HTTPChunked, ResourceID: "seed-tts-2.0", AuthMode: dto.VolcTTSAuthModeNewConsole}

	usage, apiErr := HandleNativeTTSHTTP(c, upstream.URL, request, info, cfg)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 23, usage.PromptTokens)
	assert.Equal(t, strings.Join(lines, "\n")+"\n", recorder.Body.String())
	assert.Equal(t, "native-log-id", recorder.Header().Get("X-Volc-Logid"))
	assert.JSONEq(t, string(request.RawBody), string(receivedBody))
}

func TestNativeTTSHTTPRejectsMalformedNDJSON(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "{not-json}\n")
	}))
	defer upstream.Close()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/tts/unidirectional", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "channel-key"}}
	request := &dto.VolcengineTTSNativeRequest{RawBody: []byte(`{"req_params":{"text":"你好","speaker":"seed-voice"}}`)}
	cfg := dto.VolcTTSConfig{Protocol: dto.VolcTTSProtocolV3HTTPChunked, ResourceID: "seed-tts-2.0", AuthMode: dto.VolcTTSAuthModeNewConsole}

	_, apiErr := HandleNativeTTSHTTP(c, upstream.URL, request, info, cfg)
	require.NotNil(t, apiErr)
	assert.Contains(t, apiErr.Error(), "parse volcengine native tts response")
}

func TestNativeTTSHTTPDoesNotRetryMalformedStreamAfterWriting(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"code":0,"data":"YQ=="}`+"\n{not-json}\n")
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/tts/unidirectional", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "channel-key"}}
	request := &dto.VolcengineTTSNativeRequest{RawBody: []byte(`{"req_params":{"text":"你好","speaker":"seed-voice"}}`)}
	cfg := dto.VolcTTSConfig{Protocol: dto.VolcTTSProtocolV3HTTPChunked, ResourceID: "seed-tts-2.0", AuthMode: dto.VolcTTSAuthModeNewConsole}

	_, apiErr := HandleNativeTTSHTTP(c, upstream.URL, request, info, cfg)
	require.NotNil(t, apiErr)
	assert.Equal(t, `{"code":0,"data":"YQ=="}`+"\n", recorder.Body.String())
	assert.True(t, c.GetBool("native_response_committed"))
	assert.True(t, types.IsSkipRetryError(apiErr))
}

func TestNativeTTSHTTPForwardsProviderErrorWithoutRetry(t *testing.T) {
	errorLine := `{"code":45000000,"message":"provider rejected channel-key"}`
	redactedLine := `{"code":45000000,"message":"provider rejected [REDACTED]"}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, errorLine+"\n")
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/tts/unidirectional", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "channel-key"}}
	request := &dto.VolcengineTTSNativeRequest{RawBody: []byte(`{"req_params":{"text":"你好","speaker":"seed-voice"}}`)}
	cfg := dto.VolcTTSConfig{Protocol: dto.VolcTTSProtocolV3HTTPChunked, ResourceID: "seed-tts-2.0", AuthMode: dto.VolcTTSAuthModeNewConsole}

	_, apiErr := HandleNativeTTSHTTP(c, upstream.URL, request, info, cfg)
	require.NotNil(t, apiErr)
	assert.Equal(t, redactedLine+"\n", recorder.Body.String())
	assert.NotContains(t, recorder.Body.String(), "channel-key")
	assert.True(t, c.GetBool("native_response_committed"))
	assert.True(t, types.IsSkipRetryError(apiErr))
}

func TestNativeTTSHTTPPreservesNonSuccessResponse(t *testing.T) {
	const secret = "channel-secret"
	providerBody := `{"code":45000001,"message":"bad credential channel-secret"}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, providerBody)
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/tts/unidirectional", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: secret}}
	request := &dto.VolcengineTTSNativeRequest{RawBody: []byte(`{"req_params":{"text":"你好","speaker":"seed-voice"}}`)}
	cfg := dto.VolcTTSConfig{Protocol: dto.VolcTTSProtocolV3HTTPChunked, ResourceID: "seed-tts-2.0", AuthMode: dto.VolcTTSAuthModeNewConsole}

	_, apiErr := HandleNativeTTSHTTP(c, upstream.URL, request, info, cfg)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.JSONEq(t, `{"code":45000001,"message":"bad credential [REDACTED]"}`, recorder.Body.String())
	assert.NotContains(t, recorder.Body.String(), secret)
	assert.NotContains(t, apiErr.Error(), secret)
	assert.True(t, c.GetBool("native_response_committed"))
	assert.True(t, types.IsSkipRetryError(apiErr))
}

func TestNativeTTSHTTPCancellationStopsUpstreamRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer func() {
		close(release)
		upstream.Close()
	}()

	requestContext, cancel := context.WithCancel(context.Background())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/tts/unidirectional", nil).WithContext(requestContext)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "channel-key"}}
	request := &dto.VolcengineTTSNativeRequest{RawBody: []byte(`{"req_params":{"text":"你好","speaker":"seed-voice"}}`)}
	cfg := dto.VolcTTSConfig{Protocol: dto.VolcTTSProtocolV3HTTPChunked, ResourceID: "seed-tts-2.0", AuthMode: dto.VolcTTSAuthModeNewConsole}
	result := make(chan *types.NewAPIError, 1)
	go func() {
		_, apiErr := HandleNativeTTSHTTP(c, upstream.URL, request, info, cfg)
		result <- apiErr
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upstream request did not start")
	}
	cancel()
	select {
	case apiErr := <-result:
		require.NotNil(t, apiErr)
		assert.True(t, types.IsSkipRetryError(apiErr))
		assert.False(t, types.IsRecordErrorLog(apiErr))
		assert.Equal(t, 499, apiErr.StatusCode)
	case <-time.After(time.Second):
		t.Fatal("native handler did not return after cancellation")
	}
}

type nativeTTSFailingWriter struct {
	gin.ResponseWriter
	writes int
}

type nativeTTSPartialFailWriter struct {
	gin.ResponseWriter
}

func (w *nativeTTSPartialFailWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, errors.New("downstream partial write failed")
	}
	n, _ := w.ResponseWriter.Write(p[:1])
	return n, errors.New("downstream partial write failed")
}

func (w *nativeTTSFailingWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes > 1 {
		return 0, errors.New("downstream write failed")
	}
	return w.ResponseWriter.Write(p)
}

func TestNativeTTSHTTPWriteFailureAfterCommitDisablesRetry(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "{\"code\":0,\"data\":\"YQ==\"}\n{\"code\":20000000}\n")
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Writer = &nativeTTSFailingWriter{ResponseWriter: c.Writer}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/tts/unidirectional", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "channel-key"}}
	request := &dto.VolcengineTTSNativeRequest{RawBody: []byte(`{"req_params":{"text":"你好","speaker":"seed-voice"}}`)}
	cfg := dto.VolcTTSConfig{Protocol: dto.VolcTTSProtocolV3HTTPChunked, ResourceID: "seed-tts-2.0", AuthMode: dto.VolcTTSAuthModeNewConsole}

	_, apiErr := HandleNativeTTSHTTP(c, upstream.URL, request, info, cfg)
	require.NotNil(t, apiErr)
	assert.True(t, c.GetBool("native_response_committed"))
	assert.True(t, types.IsSkipRetryError(apiErr))
}

func TestNativeTTSHTTPPartialFirstWriteDisablesRetry(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "{\"code\":0,\"data\":\"YQ==\"}\n")
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Writer = &nativeTTSPartialFailWriter{ResponseWriter: c.Writer}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/tts/unidirectional", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "channel-key"}}
	request := &dto.VolcengineTTSNativeRequest{RawBody: []byte(`{"req_params":{"text":"你好","speaker":"seed-voice"}}`)}
	cfg := dto.VolcTTSConfig{Protocol: dto.VolcTTSProtocolV3HTTPChunked, ResourceID: "seed-tts-2.0", AuthMode: dto.VolcTTSAuthModeNewConsole}

	_, apiErr := HandleNativeTTSHTTP(c, upstream.URL, request, info, cfg)
	require.NotNil(t, apiErr)
	assert.True(t, c.GetBool("native_response_committed"))
	assert.True(t, types.IsSkipRetryError(apiErr))
}
