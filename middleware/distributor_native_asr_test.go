package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"
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

func TestGetModelRequestForNativeSeedanceUsesBodyModel(t *testing.T) {
	c := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(c)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/plan/v3/contents/generations/tasks",
		strings.NewReader(`{"model":"doubao-seedance-2-0-260128","content":[{"type":"text","text":"A dress changes color"}]}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	request, shouldSelectChannel, err := getModelRequest(ctx)
	require.NoError(t, err)
	require.True(t, shouldSelectChannel)
	require.Equal(t, "doubao-seedance-2-0-260128", request.Model)
	require.Equal(t, relayconstant.RelayModeVideoSubmit, ctx.GetInt("relay_mode"))
}
