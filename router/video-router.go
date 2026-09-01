package router

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
)

func SetVideoRouter(router *gin.Engine) {
	videoSharedRouter := router.Group("/v1")
	videoSharedRouter.Use(middleware.RouteTag("relay"))
	videoSharedRouter.Use(middleware.TokenAuth())
	videoSharedRouter.Use(middleware.SystemPerformanceCheck())
	videoSharedRouter.POST(
		"/video/generations",
		middleware.PinTaskPluginEndpoint(),
		middleware.TaskPluginEndpointOnly(middleware.ModelRequestRateLimit()),
		middleware.PrepareTaskPluginEndpoint(),
		middleware.Distribute(),
		func(c *gin.Context) {
			controller.RelayTaskPluginEndpoint(c, controller.RelayTask)
		},
	)

	videoV1Router := router.Group("/v1")
	videoV1Router.Use(middleware.RouteTag("relay"))
	videoV1Router.Use(middleware.TokenAuth(), middleware.Distribute())
	{
		videoV1Router.GET("/video/generations/:task_id", controller.RelayTaskFetch)
		videoV1Router.POST("/videos/:video_id/remix", controller.RelayTask)
	}
	// Volcengine-native Seedance routes. The /api namespace is reserved from
	// plugin route registration, so these stay as static routes.
	volcengineSeedanceRouter := router.Group("/api/v3/contents/generations")
	volcengineSeedanceRouter.Use(middleware.RouteTag("relay"))
	volcengineSeedanceRouter.Use(middleware.SystemPerformanceCheck())
	volcengineSeedanceRouter.Use(middleware.TokenAuth())
	{
		volcengineSeedanceRouter.POST("/tasks", middleware.ModelRequestRateLimit(), middleware.Distribute(), func(c *gin.Context) {
			c.Set(string(constant.ContextKeyNativeSeedanceResponse), true)
			controller.RelayTask(c)
		})
		volcengineSeedanceRouter.GET("/tasks/:task_id", func(c *gin.Context) {
			c.Set("relay_mode", relayconstant.RelayModeVideoFetchByID)
			controller.RelayTaskFetch(c)
		})
	}
}
