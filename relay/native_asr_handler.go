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
	"github.com/gin-gonic/gin"
)

func NativeVolcengineASRHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	request, ok := info.Request.(*dto.VolcengineASRNativeRequest)
	if !ok {
		return types.NewError(errors.New("invalid native volcengine asr request type"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if info.RelayMode != relayconstant.RelayModeVolcengineASRNative {
		return types.NewError(errors.New("invalid native volcengine asr relay mode"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	info.InitChannelMeta(c)
	if info.ApiType != channelconstant.APITypeVolcEngine {
		return types.NewError(fmt.Errorf("native volcengine asr requires volcengine api type, got %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	if err := helper.ModelMappedHelper(c, info, request); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	cfg := info.ChannelOtherSettings.VolcASR
	if cfg == nil || cfg.EffectiveProtocol() != kitdto.VolcASRProtocolV3AUC {
		return types.NewErrorWithStatusCode(errors.New("native volcengine asr requires v3 auc channel configuration"), types.ErrorCodeBadRequestBody, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	resourceModel := info.UpstreamModelName
	if resourceModel == "" {
		resourceModel = request.Model
	}
	if resourceModel != "" && cfg.ResourceID != "" && resourceModel != cfg.ResourceID {
		return types.NewErrorWithStatusCode(fmt.Errorf("native volcengine asr resource mismatch: model %s, channel resource %s", resourceModel, cfg.ResourceID), types.ErrorCodeChannelModelMappedError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	return volcengine.HandleNativeASRHTTP(c, request, info, *cfg)
}
