package volcengine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAdaptorGetModelListIncludesSeedTTSResources(t *testing.T) {
	models := (&Adaptor{}).GetModelList()

	assert.Contains(t, models, "seed-tts-1.0-concurr")
	assert.Contains(t, models, "seed-tts-2.0")
	assert.Contains(t, models, "seed-icl-2.0")
}
