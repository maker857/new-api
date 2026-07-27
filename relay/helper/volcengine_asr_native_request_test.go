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

func TestNativeASRRequestValidationPreservesNativeBody(t *testing.T) {
	body := `{"audio":{"url":"https://example.com/input.mp4"},"request":{"model_name":"bigmodel","show_utterances":true}}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/auc/bigmodel/submit", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("X-Api-Resource-Id", "volc.seedasr.auc")

	request, err := GetAndValidateVolcengineASRNativeRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "volc.seedasr.auc", request.Model)
	assert.JSONEq(t, body, string(request.RawBody))
}

func TestNativeASRRequestRequiresResourceID(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/auc/bigmodel/query", strings.NewReader(`{"task_id":"task-1"}`))

	_, err := GetAndValidateVolcengineASRNativeRequest(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "X-Api-Resource-Id is required")
}
