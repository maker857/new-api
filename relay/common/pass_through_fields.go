package common

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ApplyPassThroughFields copies the channel's listed top-level fields from the
// client body into jsonData, the marshaled upstream request. It runs before
// RemoveDisabledFields and param override so both still apply to the copied
// fields. A field is copied only when the client sent it and the converted
// body lacks it: values the gateway produced (mapped model, converted
// parameters) are never replaced. Requests converted to another protocol are
// left alone, because a field's meaning does not carry across protocols.
func ApplyPassThroughFields(c *gin.Context, info *RelayInfo, convertedRequest any, jsonData []byte) ([]byte, error) {
	if info == nil || info.ChannelMeta == nil {
		return jsonData, nil
	}
	settings := info.ChannelOtherSettings
	if !settings.PassThroughFieldsEnabled || len(settings.PassThroughFields) == 0 {
		return jsonData, nil
	}
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		return jsonData, nil
	}
	convertedFormat, ok := convmeta.GuessRelayFormatFromRequest(convertedRequest)
	if !ok || convertedFormat != info.RelayFormat {
		return jsonData, nil
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	clientBody, err := storage.Bytes()
	if err != nil {
		return nil, err
	}

	for _, name := range settings.PassThroughFields {
		if !dto.IsPassThroughFieldAllowed(name) {
			continue
		}
		clientValue := gjson.GetBytes(clientBody, name)
		if !clientValue.Exists() || gjson.GetBytes(jsonData, name).Exists() {
			continue
		}
		jsonData, err = sjson.SetRawBytes(jsonData, name, []byte(clientValue.Raw))
		if err != nil {
			return nil, err
		}
	}
	return jsonData, nil
}
