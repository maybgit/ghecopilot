package copilot

import (
	"ghecopilot/internal/middleware"

	"github.com/gin-gonic/gin"
)

// GinApi 注册路由
func GinApi(g *gin.RouterGroup) {
	// 基础路由
	setupBasicRoutes(g)

	// 用户相关路由
	setupUserRoutes(g)

	// Copilot相关路由
	setupCopilotRoutes(g)

	// API v3相关路由
	setupV3Routes(g)
}

// setupBasicRoutes 设置基础路由
func setupBasicRoutes(g *gin.RouterGroup) {
	// 在 gin 路由匹配前保存完整路径，用于 *model-id 通配符无法捕获多段路径的场景
	g.Use(func(c *gin.Context) {
		c.Set("full_request_path", c.Request.URL.Path)
		c.Next()
	})
	g.Any("/models", GetModelsFromJson)
	g.Any("/models/*model-id", GetModel)
	g.Any("/auto", GetModel)
	g.Any("/_ping", GetPing)
	g.POST("/telemetry", PostTelemetry)
	g.Any("/agents", GetAgents)
	g.Any("/agents/swe/models", GetModelsFromJson)
	g.Any("/copilot_internal/user", GetCopilotInternalUser)
	g.Any("/copilot_internal/managed_settings", GetCopilotInternalManagedSettings)
	g.Any("/copilot/mcp_registry", GetMcpRegistry)
	g.Any("/embeddings/models", EmbeddingModels)
}

// setupUserRoutes 设置用户相关路由
func setupUserRoutes(g *gin.RouterGroup) {
	authMiddleware := middleware.AccessTokenCheckAuth()

	userGroup := g.Group("")
	userGroup.Use(authMiddleware)
	{
		userGroup.GET("/user", GetLoginUser)
		userGroup.GET("/user/orgs", GetUserOrgs)
		userGroup.GET("/api/v3/user", GetLoginUser)
		userGroup.GET("/api/v3/user/orgs", GetUserOrgs)
		userGroup.GET("/teams/:teamID/memberships/:username", GetMembership)
		userGroup.POST("/chunks", HandleChunks)
	}
}

// setupCopilotRoutes 设置Copilot相关路由
func setupCopilotRoutes(g *gin.RouterGroup) {
	tokenMiddleware := middleware.TokenCheckAuth()

	// Copilot token endpoint
	g.GET("/copilot_internal/v2/token",
		middleware.AccessTokenCheckAuth(),
		GetDisguiseCopilotInternalV2Token)
	g.OPTIONS("/copilot_internal/v2/token",
		middleware.AccessTokenCheckAuth(),
		GetDisguiseCopilotInternalV2Token)

	// Agent routes: GET /agents/:name has no middleware
	g.GET("/agents/:name", GetAgentDefinition)

	// Token 认证路由
	g.POST("/embeddings", tokenMiddleware, HandleEmbeddings)
}

// setupV3Routes 设置API v3相关路由
func setupV3Routes(g *gin.RouterGroup) {
	g.GET("/api/v3/meta", V3meta)
	g.GET("/api/v3/", Cliv3)
	g.GET("/", Cliv3)
	// Copilot 客户端登录时会请求带 /api/v3 前缀的内部用户信息接口
	g.Any("/api/v3/copilot_internal/user", GetCopilotInternalUser)
}
