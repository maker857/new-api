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

	routes := map[string]bool{}
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	require.True(t, routes[http.MethodPost+" /api/v3/contents/generations/tasks"])
	require.True(t, routes[http.MethodGet+" /api/v3/contents/generations/tasks/:task_id"])
}
