package dto

import (
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type VolcengineASRNativeRequest struct {
	Model   string `json:"-"`
	RawBody []byte `json:"-"`
}

func (r *VolcengineASRNativeRequest) GetTokenCountMeta() *types.TokenCountMeta {
	return &types.TokenCountMeta{TokenType: types.TokenTypeTextNumber}
}

func (r *VolcengineASRNativeRequest) IsStream(_ *gin.Context) bool {
	return false
}

func (r *VolcengineASRNativeRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}
