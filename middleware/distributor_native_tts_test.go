package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeTTSModelRequestUsesResourceID(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/tts/unidirectional", nil)
	c.Request.Header.Set("X-Api-Resource-Id", "seed-tts-2.0")

	request, shouldSelectChannel, err := getModelRequest(c)
	require.NoError(t, err)
	assert.True(t, shouldSelectChannel)
	assert.Equal(t, "seed-tts-2.0", request.Model)
	assert.Equal(t, relayconstant.RelayModeVolcengineTTSNative, c.GetInt("relay_mode"))
}
