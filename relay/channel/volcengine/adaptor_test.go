package volcengine

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdaptorGetModelListIncludesSeedTTSResources(t *testing.T) {
	models := (&Adaptor{}).GetModelList()

	assert.Contains(t, models, "seed-tts-1.0-concurr")
	assert.Contains(t, models, "seed-tts-2.0")
	assert.Contains(t, models, "seed-icl-2.0")
}

func TestAdaptorV1FallbackKeepsLegacyURLAuthAndRequestBehavior(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeAudioSpeech,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:               "legacy-app|legacy-token",
			ChannelOtherSettings: dto.ChannelOtherSettings{},
		},
	}
	adaptor := &Adaptor{}

	requestURL, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "wss://openspeech.bytedance.com/api/v1/tts/ws_binary", requestURL)

	body, err := adaptor.ConvertAudioRequest(c, info, dto.AudioRequest{
		Model:          "legacy-model",
		Input:          "legacy text",
		Voice:          "alloy",
		ResponseFormat: "mp3",
	})
	require.NoError(t, err)
	_, err = io.ReadAll(body)
	require.NoError(t, err)
	requestValue, exists := c.Get(contextKeyTTSRequest)
	require.True(t, exists)
	volcRequest, ok := requestValue.(VolcengineTTSRequest)
	require.True(t, ok)
	assert.Equal(t, "legacy-app", volcRequest.App.AppID)
	assert.Equal(t, "legacy-token", volcRequest.App.Token)
	assert.Equal(t, mapVoiceType("alloy"), volcRequest.Audio.VoiceType)
	assert.True(t, info.IsStream)
}
