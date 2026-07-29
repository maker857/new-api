package helper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeTTSRequestValidation(t *testing.T) {
	body := `{"namespace":"UnidirectionalTTS","req_params":{"text":"你好","speaker":"speaker-id","audio_params":{"enable_subtitle":true,"speech_rate":0},"future_option":false}}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/tts/unidirectional", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("X-Api-Resource-Id", "seed-tts-2.0")

	request, err := GetAndValidateVolcengineTTSNativeRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "seed-tts-2.0", request.Model)
	assert.Equal(t, "你好", request.ReqParams.Text)
	assert.Equal(t, "speaker-id", request.ReqParams.Speaker)
	assert.JSONEq(t, body, string(request.RawBody))
}

func TestNativeTTSRequestRejectsMissingRequiredFields(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "text", body: `{"req_params":{"speaker":"speaker-id"}}`},
		{name: "speaker", body: `{"req_params":{"text":"你好"}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/tts/unidirectional", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request.Header.Set("X-Api-Resource-Id", "seed-tts-2.0")

			_, err := GetAndValidateVolcengineTTSNativeRequest(c)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.name+" is required")
		})
	}
}

func TestNativeTTSRequestRequiresResourceID(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/tts/unidirectional", strings.NewReader(`{"req_params":{"text":"你好","speaker":"speaker-id"}}`))
	c.Request.Header.Set("Content-Type", "application/json")

	_, err := GetAndValidateVolcengineTTSNativeRequest(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "X-Api-Resource-Id is required")
}
