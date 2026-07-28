package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSetVideoRouterRegistersNativeSeedanceTaskRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetVideoRouter(engine)

	for _, route := range engine.Routes() {
		if route.Method == http.MethodPost && route.Path == "/api/plan/v3/contents/generations/tasks" {
			return
		}
	}

	require.Fail(t, "native Seedance task route is not registered")
}
