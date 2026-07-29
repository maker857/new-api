package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestNativeTTSTokenAuthUsesXApiKey(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/tts/unidirectional", nil)
	c.Request.Header.Set("Authorization", "Bearer ignored-token")
	c.Request.Header.Set("X-Api-Key", "sk-gateway-token")

	applyNativeTTSTokenAuthorization(c)

	assert.Equal(t, "Bearer sk-gateway-token", c.Request.Header.Get("Authorization"))
}

func TestNativeTTSTokenAuthDoesNotAffectOtherRoutes(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	c.Request.Header.Set("Authorization", "Bearer openai-token")
	c.Request.Header.Set("X-Api-Key", "sk-native-token")

	applyNativeTTSTokenAuthorization(c)

	assert.Equal(t, "Bearer openai-token", c.Request.Header.Get("Authorization"))
}
