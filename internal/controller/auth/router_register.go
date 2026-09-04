package auth

import (
	"ghecopilot/internal/middleware"
	"strings"

	"github.com/gin-gonic/gin"
)

func GinApi(g *gin.RouterGroup) {
	g.GET("/help", getHelpPage)
	// 启动设备代码登录流程
	g.POST("/login/device/code", postLoginDeviceCode)
	g.POST("/login/device", postLoginDevice)
	g.GET("/login/device", getLoginDevice)
	g.POST("/login/oauth/access_token", func(ctx *gin.Context) {
		if strings.Contains(ctx.Request.UserAgent(), "VSTeamExplorer") {
			middleware.AuthCodeFlowCheckAuth(ctx)
		} else {
			middleware.DeviceCodeCheckAuth(ctx)
		}
	}, func(ctx *gin.Context) {
		if strings.Contains(ctx.Request.UserAgent(), "VSTeamExplorer") {
			postLoginOauthAccessTokenForVs2022(ctx)
		} else {
			postLoginOauthAccessToken(ctx)
		}
	})

	// oauth2 登录
	g.GET("/login/oauth/authorize", getLoginOauthAuthorize)

	// OAuth 回调：从浏览器验证链接获取 token
	g.GET("/redirect", getRedirect)
	g.GET("/callback", getRedirect)

	// enterprise 验证
	g.GET("/site/sha", getSiteSha)

	// 获取登录页面配置
	g.GET("/login/config", getLoginConfig)

	// GitHub模拟登录获取 ghu_token
	g.GET("/github/login/device/code", getGithubLoginDevice)
	g.POST("/github/login/device/code", getDeviceCode)
	g.POST("/github/login/ghu-token", getGhuToken)
}
