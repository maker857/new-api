package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetModelRequestForNativeASRUsesResourceHeader(t *testing.T) {
	c := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(c)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v3/auc/bigmodel/submit", nil)
	ctx.Request.Header.Set("X-Api-Resource-Id", "volc.seedasr.auc")

	request, shouldSelectChannel, err := getModelRequest(ctx)
	require.NoError(t, err)
	require.True(t, shouldSelectChannel)
	require.Equal(t, "volc.seedasr.auc", request.Model)
}
