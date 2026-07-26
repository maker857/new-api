package volcengine

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestV3AuthHeaders(t *testing.T) {
	t.Run("new console api key", func(t *testing.T) {
		headers, err := buildV3AuthHeaders("console-api-key", dto.VolcTTSConfig{
			Protocol:   dto.VolcTTSProtocolV3WsUni,
			ResourceID: "seed-tts-2.0",
			AuthMode:   dto.VolcTTSAuthModeNewConsole,
		}, "connect-1")
		require.NoError(t, err)
		assert.Equal(t, "console-api-key", headers.Get("X-Api-Key"))
		assert.Empty(t, headers.Get("X-Api-App-Id"))
		assert.Equal(t, "seed-tts-2.0", headers.Get("X-Api-Resource-Id"))
		assert.Equal(t, "connect-1", headers.Get("X-Api-Connect-Id"))
		assert.Equal(t, "*", headers.Get("X-Control-Require-Usage-Tokens-Return"))
	})

	t.Run("legacy key", func(t *testing.T) {
		headers, err := buildV3AuthHeaders("app-id|access-token", dto.VolcTTSConfig{
			Protocol:   dto.VolcTTSProtocolV3HTTPChunked,
			ResourceID: "seed-icl-2.0",
			AuthMode:   dto.VolcTTSAuthModeLegacy,
		}, "connect-2")
		require.NoError(t, err)
		assert.Equal(t, "app-id", headers.Get("X-Api-App-Id"))
		assert.Equal(t, "access-token", headers.Get("X-Api-Access-Key"))
		assert.Empty(t, headers.Get("X-Api-Key"))
	})

	t.Run("legacy rejects malformed key", func(t *testing.T) {
		_, err := buildV3AuthHeaders("access-token", dto.VolcTTSConfig{
			Protocol:   dto.VolcTTSProtocolV3WsUni,
			ResourceID: "seed-tts-2.0",
			AuthMode:   dto.VolcTTSAuthModeLegacy,
		}, "connect-3")
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "access-token")
	})

	t.Run("usage can be disabled", func(t *testing.T) {
		requireUsage := false
		headers, err := buildV3AuthHeaders("console-api-key", dto.VolcTTSConfig{
			Protocol:     dto.VolcTTSProtocolV3WsUni,
			ResourceID:   "seed-tts-2.0",
			AuthMode:     dto.VolcTTSAuthModeNewConsole,
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
	assert.Equal(t, "seed-tts-2.0", decoded.ReqParams.Model)
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
	endpoint, err := getV3TTSEndpoint(dto.VolcTTSProtocolV3WsUni)
	require.NoError(t, err)
	assert.Equal(t, "wss://openspeech.bytedance.com/api/v3/tts/unidirectional/stream", endpoint)

	endpoint, err = getV3TTSEndpoint(dto.VolcTTSProtocolV3HTTPChunked)
	require.NoError(t, err)
	assert.Equal(t, "https://openspeech.bytedance.com/api/v3/tts/unidirectional", endpoint)

	_, err = getV3TTSEndpoint(dto.VolcTTSProtocolV1WsBinary)
	require.Error(t, err)
}
