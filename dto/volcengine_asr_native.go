package dto

import (
	"net/http"

	"github.com/QuantumNous/new-api/relaykit/types"
)

type VolcengineASRNativeRequest struct {
	Model   string `json:"-"`
	RawBody []byte `json:"-"`
}

func (r *VolcengineASRNativeRequest) GetTokenCountMeta() *types.TokenCountMeta {
	return &types.TokenCountMeta{TokenType: types.TokenTypeTextNumber}
}

func (r *VolcengineASRNativeRequest) IsStream(_ *http.Request) bool {
	return false
}

func (r *VolcengineASRNativeRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}
