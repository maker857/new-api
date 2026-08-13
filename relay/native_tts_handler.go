package relay

import (
	"errors"
	"fmt"
	"net/http"

	channelconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/volcengine"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func NativeVolcengineTTSHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	request, ok := info.Request.(*dto.VolcengineTTSNativeRequest)
	if !ok {
		return types.NewError(errors.New("invalid native volcengine tts request type"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if info.RelayMode != relayconstant.RelayModeVolcengineTTSNative {
		return types.NewError(errors.New("invalid native volcengine tts relay mode"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	info.InitChannelMeta(c)
	if info.ApiType != channelconstant.APITypeVolcEngine {
		return types.NewError(fmt.Errorf("native volcengine tts requires volcengine api type, got %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	if err := helper.ModelMappedHelper(c, info, request); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	cfg := info.ChannelOtherSettings.VolcTTS
	if cfg == nil || cfg.EffectiveProtocol() != kitdto.VolcTTSProtocolV3HTTPChunked {
		return types.NewErrorWithStatusCode(errors.New("native volcengine tts requires v3 http chunked channel configuration"), types.ErrorCodeBadRequestBody, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if info.UpstreamModelName != "" && cfg.ResourceID != "" && info.UpstreamModelName != cfg.ResourceID {
		return types.NewErrorWithStatusCode(fmt.Errorf("native volcengine tts resource mismatch: model %s, channel resource %s", info.UpstreamModelName, cfg.ResourceID), types.ErrorCodeChannelModelMappedError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	requestURL, err := (&volcengine.Adaptor{}).GetRequestURL(info)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeBadRequestBody, http.StatusInternalServerError)
	}
	usage, apiErr := volcengine.HandleNativeTTSHTTP(c, requestURL, request, info, *cfg)
	if apiErr != nil {
		return apiErr
	}
	if usage == nil {
		return nil
	}
	service.PostTextConsumeQuota(c, info, usage, nil)
	return nil
}
