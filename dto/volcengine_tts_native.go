package dto

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type VolcengineTTSNativeRequest struct {
	Namespace string                       `json:"namespace,omitempty"`
	ReqParams VolcengineTTSNativeReqParams `json:"req_params"`
	Model     string                       `json:"-"`
	RawBody   json.RawMessage              `json:"-"`
}

type VolcengineTTSNativeReqParams struct {
	Text        string          `json:"text"`
	Speaker     string          `json:"speaker"`
	AudioParams json.RawMessage `json:"audio_params,omitempty"`
}

func (r *VolcengineTTSNativeRequest) GetTokenCountMeta() *types.TokenCountMeta {
	return &types.TokenCountMeta{
		CombineText: r.ReqParams.Text,
		TokenType:   types.TokenTypeTextNumber,
	}
}

func (r *VolcengineTTSNativeRequest) IsStream(_ *gin.Context) bool {
	return false
}

func (r *VolcengineTTSNativeRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}
