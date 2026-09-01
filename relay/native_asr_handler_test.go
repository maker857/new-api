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

func TestNativeVolcengineASRRejectsNonVolcengineChannel(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/auc/bigmodel/submit", nil)
	c.Set(string(constant.ContextKeyChannelType), constant.ChannelTypeOpenAI)
	c.Set(string(constant.ContextKeyChannelKey), "channel-key")
	c.Set(string(constant.ContextKeyChannelOtherSetting), kitdto.ChannelOtherSettings{
		VolcASR: &kitdto.VolcASRConfig{Protocol: kitdto.VolcASRProtocolV3AUC, ResourceID: "volc.seedasr.auc", AuthMode: kitdto.VolcASRAuthModeNewConsole},
	})
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeVolcengineASRNative,
		OriginModelName: "volc.seedasr.auc",
		Request:         &dto.VolcengineASRNativeRequest{Model: "volc.seedasr.auc", RawBody: []byte(`{"request":{"model_name":"bigmodel"}}`)},
	}

	apiErr := NativeVolcengineASRHelper(c, info)
	require.NotNil(t, apiErr)
	assert.Contains(t, apiErr.Error(), "requires volcengine api type")
}

func TestNativeVolcengineASRRejectsChannelResourceMismatch(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/auc/bigmodel/submit", nil)
	c.Set(string(constant.ContextKeyChannelType), constant.ChannelTypeVolcEngine)
	c.Set(string(constant.ContextKeyChannelKey), "channel-key")
	c.Set(string(constant.ContextKeyChannelOtherSetting), kitdto.ChannelOtherSettings{
		VolcASR: &kitdto.VolcASRConfig{Protocol: kitdto.VolcASRProtocolV3AUC, ResourceID: "volc.bigasr.auc", AuthMode: kitdto.VolcASRAuthModeNewConsole},
	})
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeVolcengineASRNative,
		OriginModelName: "volc.seedasr.auc",
		Request:         &dto.VolcengineASRNativeRequest{Model: "volc.seedasr.auc", RawBody: []byte(`{"request":{"model_name":"bigmodel"}}`)},
	}

	apiErr := NativeVolcengineASRHelper(c, info)
	require.NotNil(t, apiErr)
	assert.Contains(t, apiErr.Error(), "resource mismatch")
}
