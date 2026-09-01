package volcengine

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestV3HTTPChunkedStreamsJSONLinesAndReturnsUsage(t *testing.T) {
	lines := []string{
		fmt.Sprintf(`{"code":0,"message":"OK","data":%q}`, base64.StdEncoding.EncodeToString([]byte("chunk-1"))),
		fmt.Sprintf(`{"code":0,"message":"OK","data":%q,"usage":{"text_words":23}}`, base64.StdEncoding.EncodeToString([]byte("chunk-2"))),
		`{"code":20000000,"message":"finished","usage":{"text_words":23}}`,
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "console-api-key" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			http.Error(w, readErr.Error(), http.StatusBadRequest)
			return
		}
		var requestBody v3HTTPRequestBody
		if common.Unmarshal(body, &requestBody) != nil || requestBody.ReqParams.Text != "http chunked text" {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		w.Header().Set("X-Tt-Logid", "log-id-123")
		w.WriteHeader(http.StatusOK)
		stream := strings.Join(lines, "\n") + "\n"
		for start := 0; start < len(stream); start += 3 {
			end := start + 3
			if end > len(stream) {
				end = len(stream)
			}
			_, _ = w.Write([]byte(stream[start:end]))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "console-api-key"}}
	request := VolcengineTTSRequest{User: VolcengineTTSUser{UID: "relay"}, Audio: VolcengineTTSAudio{VoiceType: "seed-voice", Rate: 24000}, Request: VolcengineTTSReqInfo{Text: "http chunked text", Model: "seed-tts-2.0"}}
	cfg := kitdto.VolcTTSConfig{Protocol: kitdto.VolcTTSProtocolV3HTTPChunked, ResourceID: "seed-tts-2.0", AuthMode: kitdto.VolcTTSAuthModeNewConsole}

	usageAny, apiErr := handleTTSV3HTTPChunked(c, upstream.URL, request, info, "mp3", cfg)
	require.Nil(t, apiErr)
	usage, ok := usageAny.(*kitdto.Usage)
	require.True(t, ok)
	assert.Equal(t, 23, usage.PromptTokens)
	assert.Equal(t, "chunk-1chunk-2", recorder.Body.String())
	assert.Equal(t, "log-id-123", recorder.Header().Get("X-Volc-Logid"))
}

func TestV3HTTPChunkedProviderErrorBecomesAPIError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"code":45000000,"message":"provider rejected request"}`+"\n")
	}))
	defer upstream.Close()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "console-api-key"}}
	cfg := kitdto.VolcTTSConfig{Protocol: kitdto.VolcTTSProtocolV3HTTPChunked, ResourceID: "seed-tts-2.0", AuthMode: kitdto.VolcTTSAuthModeNewConsole}
	_, apiErr := handleTTSV3HTTPChunked(c, upstream.URL, VolcengineTTSRequest{}, info, "mp3", cfg)
	require.NotNil(t, apiErr)
	assert.Contains(t, apiErr.Error(), "provider rejected request")
}

func TestV3HTTPChunkedNon200BodyIsBoundedAndCredentialsAreRedacted(t *testing.T) {
	secret := "console-super-secret"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, "%s:%s", secret, strings.Repeat("x", 10000))
	}))
	defer upstream.Close()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: secret}}
	cfg := kitdto.VolcTTSConfig{Protocol: kitdto.VolcTTSProtocolV3HTTPChunked, ResourceID: "seed-tts-2.0", AuthMode: kitdto.VolcTTSAuthModeNewConsole}
	_, apiErr := handleTTSV3HTTPChunked(c, upstream.URL, VolcengineTTSRequest{}, info, "mp3", cfg)
	require.NotNil(t, apiErr)
	assert.NotContains(t, apiErr.Error(), secret)
	assert.Less(t, len(apiErr.Error()), 5000)
}

func TestV3WSUnidirectionalStreamsAudioAndUsage(t *testing.T) {
	serverErr := make(chan error, 1)
	finished := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "console-api-key" || r.Header.Get("X-Api-Resource-Id") != "seed-tts-2.0" {
			serverErr <- assert.AnError
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()

		startConnection, err := ReceiveMessage(conn)
		if err != nil || startConnection.EventType != EventType_StartConnection {
			serverErr <- err
			return
		}
		if err = writeV3TestServerEvent(conn, EventType_ConnectionStarted, "", r.Header.Get("X-Api-Connect-Id"), []byte("{}")); err != nil {
			serverErr <- err
			return
		}

		startSession, err := ReceiveMessage(conn)
		if err != nil || startSession.EventType != EventType_StartSession {
			serverErr <- err
			return
		}
		var payload v3StartSessionPayload
		if err = common.Unmarshal(startSession.Payload, &payload); err != nil || payload.ReqParams.Text != "stream this text" {
			serverErr <- err
			return
		}
		if err = writeV3TestServerEvent(conn, EventType_SessionStarted, startSession.SessionID, "", []byte("{}")); err != nil {
			serverErr <- err
			return
		}

		finishSession, err := ReceiveMessage(conn)
		if err != nil || finishSession.EventType != EventType_FinishSession {
			serverErr <- err
			return
		}
		if err = writeV3TestAudioEvent(conn, startSession.SessionID, []byte("audio-1")); err != nil {
			serverErr <- err
			return
		}
		if err = writeV3TestAudioEvent(conn, startSession.SessionID, []byte("audio-2")); err != nil {
			serverErr <- err
			return
		}
		usagePayload, err := common.Marshal(v3SessionResultEnvelope{Usage: &v3UsageStats{TextWords: 17}})
		if err != nil {
			serverErr <- err
			return
		}
		if err = writeV3TestServerEvent(conn, EventType_SessionFinished, startSession.SessionID, "", usagePayload); err != nil {
			serverErr <- err
			return
		}

		finishConnection, err := ReceiveMessage(conn)
		if err != nil || finishConnection.EventType != EventType_FinishConnection {
			serverErr <- err
			return
		}
		close(finished)
	}))
	defer server.Close()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "console-api-key"}}
	request := VolcengineTTSRequest{
		User:    VolcengineTTSUser{UID: "relay-user"},
		Audio:   VolcengineTTSAudio{VoiceType: "seed-voice", Rate: 24000},
		Request: VolcengineTTSReqInfo{Text: "stream this text", Model: "seed-tts-2.0"},
	}
	cfg := kitdto.VolcTTSConfig{Protocol: kitdto.VolcTTSProtocolV3WsUni, ResourceID: "seed-tts-2.0", AuthMode: kitdto.VolcTTSAuthModeNewConsole}

	usageAny, apiErr := handleTTSV3WSUnidirectional(c, "ws"+strings.TrimPrefix(server.URL, "http"), request, info, "mp3", cfg)
	require.Nil(t, apiErr)
	usage, ok := usageAny.(*kitdto.Usage)
	require.True(t, ok)
	assert.Equal(t, 17, usage.PromptTokens)
	assert.Equal(t, "audio-1audio-2", recorder.Body.String())
	select {
	case <-finished:
	case err := <-serverErr:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("server did not observe finish connection")
	}
}

func TestV3WSCancellationReturnsWithoutUpstreamError(t *testing.T) {
	startReceived := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if _, err = ReceiveMessage(conn); err != nil {
			return
		}
		close(startReceived)
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()

	requestContext, cancel := context.WithCancel(context.Background())
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil).WithContext(requestContext)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "console-api-key"}}
	cfg := kitdto.VolcTTSConfig{Protocol: kitdto.VolcTTSProtocolV3WsUni, ResourceID: "seed-tts-2.0", AuthMode: kitdto.VolcTTSAuthModeNewConsole}
	result := make(chan *types.NewAPIError, 1)
	go func() {
		_, apiErr := handleTTSV3WSUnidirectional(c, "ws"+strings.TrimPrefix(server.URL, "http"), VolcengineTTSRequest{}, info, "mp3", cfg)
		result <- apiErr
	}()

	select {
	case <-startReceived:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive start connection")
	}
	select {
	case apiErr := <-result:
		assert.Nil(t, apiErr)
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit after cancellation")
	}
}

func writeV3TestServerEvent(conn *websocket.Conn, event EventType, sessionID, connectID string, payload []byte) error {
	message, _ := NewMessage(MsgTypeFullServerResponse, MsgTypeFlagWithEvent)
	message.EventType = event
	message.SessionID = sessionID
	message.ConnectID = connectID
	message.Payload = payload
	frame, err := message.Marshal()
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.BinaryMessage, frame)
}

func writeV3TestAudioEvent(conn *websocket.Conn, sessionID string, payload []byte) error {
	message, _ := NewMessage(MsgTypeAudioOnlyServer, MsgTypeFlagWithEvent)
	message.EventType = EventType_TTSResponse
	message.SessionID = sessionID
	message.Payload = payload
	frame, err := message.Marshal()
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.BinaryMessage, frame)
}

func TestV3AuthHeaders(t *testing.T) {
	t.Run("new console api key", func(t *testing.T) {
		headers, err := buildV3AuthHeaders("console-api-key", kitdto.VolcTTSConfig{
			Protocol:   kitdto.VolcTTSProtocolV3WsUni,
			ResourceID: "seed-tts-2.0",
			AuthMode:   kitdto.VolcTTSAuthModeNewConsole,
		}, "connect-1")
		require.NoError(t, err)
		assert.Equal(t, "console-api-key", headers.Get("X-Api-Key"))
		assert.Empty(t, headers.Get("X-Api-App-Id"))
		assert.Equal(t, "seed-tts-2.0", headers.Get("X-Api-Resource-Id"))
		assert.Equal(t, "connect-1", headers.Get("X-Api-Connect-Id"))
		assert.Equal(t, "*", headers.Get("X-Control-Require-Usage-Tokens-Return"))
	})

	t.Run("legacy key", func(t *testing.T) {
		headers, err := buildV3AuthHeaders("app-id|access-token", kitdto.VolcTTSConfig{
			Protocol:   kitdto.VolcTTSProtocolV3HTTPChunked,
			ResourceID: "seed-icl-2.0",
			AuthMode:   kitdto.VolcTTSAuthModeLegacy,
		}, "connect-2")
		require.NoError(t, err)
		assert.Equal(t, "app-id", headers.Get("X-Api-App-Id"))
		assert.Equal(t, "access-token", headers.Get("X-Api-Access-Key"))
		assert.Empty(t, headers.Get("X-Api-Key"))
	})

	t.Run("legacy rejects malformed key", func(t *testing.T) {
		_, err := buildV3AuthHeaders("access-token", kitdto.VolcTTSConfig{
			Protocol:   kitdto.VolcTTSProtocolV3WsUni,
			ResourceID: "seed-tts-2.0",
			AuthMode:   kitdto.VolcTTSAuthModeLegacy,
		}, "connect-3")
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "access-token")
	})

	t.Run("usage can be disabled", func(t *testing.T) {
		requireUsage := false
		headers, err := buildV3AuthHeaders("console-api-key", kitdto.VolcTTSConfig{
			Protocol:     kitdto.VolcTTSProtocolV3WsUni,
			ResourceID:   "seed-tts-2.0",
			AuthMode:     kitdto.VolcTTSAuthModeNewConsole,
			RequireUsage: &requireUsage,
		}, "connect-4")
		require.NoError(t, err)
		assert.Empty(t, headers.Get("X-Control-Require-Usage-Tokens-Return"))
	})
}

func TestV3StartSessionPayloadMapsAudioRequest(t *testing.T) {
	request := VolcengineTTSRequest{
		User: VolcengineTTSUser{UID: "relay-user"},
		Audio: VolcengineTTSAudio{
			VoiceType:  "zh_female_seed_voice",
			Encoding:   "mp3",
			SpeedRatio: 1.25,
			Rate:       24000,
			Bitrate:    128000,
		},
		Request: VolcengineTTSReqInfo{
			Text:  "hello volcengine",
			Model: "seed-tts-2.0",
		},
	}

	payload, err := buildV3StartSessionPayload(request, "mp3")
	require.NoError(t, err)

	var decoded v3StartSessionPayload
	require.NoError(t, common.Unmarshal(payload, &decoded))
	assert.Equal(t, int32(EventType_StartSession), decoded.Event)
	assert.Equal(t, "UnidirectionalTTS", decoded.Namespace)
	assert.Equal(t, "hello volcengine", decoded.ReqParams.Text)
	assert.Equal(t, "zh_female_seed_voice", decoded.ReqParams.Speaker)
	assert.NotContains(t, string(payload), `"model"`)
	assert.Equal(t, "mp3", decoded.ReqParams.AudioParams.Format)
	require.NotNil(t, decoded.ReqParams.AudioParams.SampleRate)
	assert.Equal(t, 24000, *decoded.ReqParams.AudioParams.SampleRate)
	require.NotNil(t, decoded.ReqParams.AudioParams.BitRate)
	assert.Equal(t, 128000, *decoded.ReqParams.AudioParams.BitRate)
	require.NotNil(t, decoded.ReqParams.AudioParams.SpeechRate)
	assert.Equal(t, 25, *decoded.ReqParams.AudioParams.SpeechRate)
}

func TestV3UsageUsesUpstreamWordsAndSaturates(t *testing.T) {
	usage, err := parseV3Usage([]byte(`{"usage":{"text_words":42}}`), 9)
	require.NoError(t, err)
	assert.Equal(t, 42, usage.PromptTokens)
	assert.Equal(t, 42, usage.TotalTokens)

	usage, clamp, err := parseV3UsageChecked([]byte(`{"usage":{"text_words":1e100}}`), 9)
	require.NoError(t, err)
	require.NotNil(t, clamp)
	assert.Equal(t, math.MaxInt32, usage.PromptTokens)

	usage, err = parseV3Usage([]byte(`{"message":"ok"}`), 9)
	require.NoError(t, err)
	assert.Equal(t, 9, usage.PromptTokens)
}

func TestV3EndpointResolution(t *testing.T) {
	endpoint, err := getV3TTSEndpoint(kitdto.VolcTTSProtocolV3WsUni)
	require.NoError(t, err)
	assert.Equal(t, "wss://openspeech.bytedance.com/api/v3/tts/unidirectional/stream", endpoint)

	endpoint, err = getV3TTSEndpoint(kitdto.VolcTTSProtocolV3HTTPChunked)
	require.NoError(t, err)
	assert.Equal(t, "https://openspeech.bytedance.com/api/v3/tts/unidirectional", endpoint)

	_, err = getV3TTSEndpoint(kitdto.VolcTTSProtocolV1WsBinary)
	require.Error(t, err)
}
