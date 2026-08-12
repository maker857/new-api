package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNativeTTSRequestMetadata(t *testing.T) {
	request := &VolcengineTTSNativeRequest{
		Model: "seed-tts-2.0",
		ReqParams: VolcengineTTSNativeReqParams{
			Text:    "你好",
			Speaker: "speaker-id",
		},
	}

	assert.Equal(t, "你好", request.GetTokenCountMeta().CombineText)
	assert.True(t, request.IsStream(nil))

	request.SetModelName("mapped-seed-tts-2.0")
	assert.Equal(t, "mapped-seed-tts-2.0", request.Model)
}
