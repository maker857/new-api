package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRelayDoesNotAppendOpenAIErrorAfterNativeResponseCommitted(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/tts/unidirectional", strings.NewReader("{not-json}"))
	c.Set("native_response_committed", true)

	Relay(c, types.RelayFormatVolcengineTTSNative)

	assert.Empty(t, recorder.Body.String())
}
