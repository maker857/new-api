package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeVolcengineTTSRejectsNonVolcengineChannel(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/tts/unidirectional", nil)
	c.Set(string(constant.ContextKeyChannelType), constant.ChannelTypeOpenAI)
	c.Set(string(constant.ContextKeyChannelKey), "channel-key")
	c.Set(string(constant.ContextKeyOriginalModel), "seed-tts-2.0")
	c.Set(string(constant.ContextKeyChannelOtherSetting), kitdto.ChannelOtherSettings{
		VolcTTS: &kitdto.VolcTTSConfig{Protocol: kitdto.VolcTTSProtocolV3HTTPChunked, ResourceID: "seed-tts-2.0", AuthMode: kitdto.VolcTTSAuthModeNewConsole},
	})
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeVolcengineTTSNative,
		OriginModelName: "seed-tts-2.0",
		Request:         &dto.VolcengineTTSNativeRequest{Model: "seed-tts-2.0", RawBody: []byte(`{"req_params":{"text":"你好","speaker":"seed-voice"}}`)},
	}

	apiErr := NativeVolcengineTTSHelper(c, info)
	require.NotNil(t, apiErr)
	assert.Contains(t, apiErr.Error(), "requires volcengine api type")
}

func TestNativeVolcengineTTSRejectsChannelResourceMismatch(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/tts/unidirectional", nil)
	c.Set(string(constant.ContextKeyChannelType), constant.ChannelTypeVolcEngine)
	c.Set(string(constant.ContextKeyChannelKey), "channel-key")
	c.Set(string(constant.ContextKeyOriginalModel), "seed-tts-2.0")
	c.Set(string(constant.ContextKeyChannelOtherSetting), kitdto.ChannelOtherSettings{
		VolcTTS: &kitdto.VolcTTSConfig{Protocol: kitdto.VolcTTSProtocolV3HTTPChunked, ResourceID: "seed-tts-1.0", AuthMode: kitdto.VolcTTSAuthModeNewConsole},
	})
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeVolcengineTTSNative,
		OriginModelName: "seed-tts-2.0",
		Request:         &dto.VolcengineTTSNativeRequest{Model: "seed-tts-2.0", RawBody: []byte(`{"req_params":{"text":"你好","speaker":"seed-voice"}}`)},
	}

	apiErr := NativeVolcengineTTSHelper(c, info)
	require.NotNil(t, apiErr)
	assert.Contains(t, apiErr.Error(), "resource mismatch")
}
