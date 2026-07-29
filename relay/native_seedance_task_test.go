package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteNativeSeedanceErrorResponsePreservesUpstreamResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	upstreamBody := []byte("{\n  \"error\": {\"code\": \"InvalidParameter\"}\n}\n")
	upstreamResponse := &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/problem+json"}},
	}

	taskErr := writeNativeSeedanceErrorResponse(c, upstreamResponse, upstreamBody)

	require.NotNil(t, taskErr)
	assert.False(t, taskErr.LocalError)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.True(t, c.GetBool("native_response_committed"))
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Equal(t, "application/problem+json", recorder.Header().Get("Content-Type"))
	assert.Equal(t, upstreamBody, recorder.Body.Bytes())

	_, err := io.ReadAll(recorder.Result().Body)
	require.NoError(t, err)
}
