package copilot

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"ghecopilot/internal/middleware"
	jwtpkg "ghecopilot/pkg/jwt"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gofrs/uuid"
)

// GetLoginUser 获取登录用户信息
func GetLoginUser(ctx *gin.Context) {
	userDisplayName := "github"
	token, _ := jwtpkg.GetJwtProto(ctx, &middleware.UserLoad{})
	if token != nil && token.UserDisplayName != "" {
		userDisplayName = token.UserDisplayName
	}

	ctx.Header("X-OAuth-Scopes", "gist, read:org, repo, user, workflow, write:public_key")
	requestID := uuid.Must(uuid.NewV4()).String()
	ctx.Header("x-github-request-id", requestID)
	ctx.JSON(http.StatusOK, gin.H{
		"login":               userDisplayName,
		"id":                  9919,
		"node_id":             "DEyOk9yZ2FuaXphdGlvbjk5MTk=",
		"avatar_url":          "https://avatars.githubusercontent.com/u/9919?v=4",
		"gravatar_id":         "",
		"url":                 "https://api.github.com/users/github",
		"html_url":            "https://github.com/github",
		"followers_url":       "https://api.github.com/users/github/followers",
		"following_url":       "https://api.github.com/users/github/following{/other_user}",
		"gists_url":           "https://api.github.com/users/github/gists{/gist_id}",
		"starred_url":         "https://api.github.com/users/github/starred{/owner}{/repo}",
		"subscriptions_url":   "https://api.github.com/users/github/subscriptions",
		"organizations_url":   "https://api.github.com/users/github/orgs",
		"repos_url":           "https://api.github.com/users/github/repos",
		"events_url":          "https://api.github.com/users/github/events{/privacy}",
		"received_events_url": "https://api.github.com/users/github/received_events",
		"type":                "User",
		"site_admin":          false,
		"name":                "GitHub",
		"company":             nil,
		"blog":                "",
		"location":            "San Francisco, CA",
		"email":               nil,
		"hireable":            nil,
		"bio":                 nil,
		"twitter_username":    nil,
		"public_repos":        498,
		"public_gists":        0,
		"followers":           42848,
		"following":           0,
		"created_at":          "2008-05-11T04:37:31Z",
		"updated_at":          "2022-11-29T19:44:55Z",
	})

}

func GetUserOrgs(ctx *gin.Context) {
	ctx.Header("X-OAuth-Scopes", "gist, read:org, repo, user, workflow, write:public_key")
	ctx.JSON(http.StatusOK, []interface{}{})
}

// generateTrackingID 生成模拟的 analytics_tracking_id
func generateTrackingID() string {
	// 生成一个随机字符串并计算其 MD5
	randomStr := fmt.Sprintf("%d%d", time.Now().UnixNano(), rand.Int())
	hash := md5.Sum([]byte(randomStr))
	return hex.EncodeToString(hash[:])
}

// generateAssignedDate 生成模拟的 assigned_date
func generateAssignedDate() string {
	// 生成最近30天内的随机时间
	now := time.Now()
	daysAgo := rand.Intn(30)
	randomTime := now.AddDate(0, 0, -daysAgo)

	// 随机增加小时和分钟
	randomHour := rand.Intn(24)
	randomMinute := rand.Intn(60)
	randomTime = randomTime.Add(time.Duration(randomHour) * time.Hour)
	randomTime = randomTime.Add(time.Duration(randomMinute) * time.Minute)

	// 返回格式化的时间字符串
	return randomTime.Format(time.RFC3339)
}

// GetCopilotInternalManagedSettings 获取 Copilot 托管设置
// VS Code Copilot 扩展启动时会请求此接口获取组织级托管设置
//
// 注意：响应体是"裸对象"，没有 settings 包装层。客户端会把整个 body 当作
// IManagedSettingsResponse 解析（见 microsoft/vscode managedSettings.ts）。
// 空对象 {} 表示"无组织策略"，是成功状态，不会导致 MCP 被禁用。
//
// MCP 的启用/禁用由 /copilot_internal/v2/token 返回的 token 中的 mcp 标志控制
// （mcp=1 启用 / mcp=0 禁用），而非此接口。此接口仅承载 per-server 的
// allowedMcpServers / deniedMcpServers / allowManagedMcpServersOnly 等策略。
// 这里返回 {} 表示"启用 MCP 且允许所有服务器"。
func GetCopilotInternalManagedSettings(ctx *gin.Context) {
	requestID := uuid.Must(uuid.NewV4()).String()
	ctx.Header("x-github-request-id", requestID)
	ctx.JSON(http.StatusOK, gin.H{})
}

// GetMcpRegistry 获取 MCP 注册表
// 当 token 中 mcp=1 时，客户端会请求此接口获取 MCP 注册表访问策略。
// registry_access=allow_all 表示允许所有 MCP 服务器。
func GetMcpRegistry(ctx *gin.Context) {
	requestID := uuid.Must(uuid.NewV4()).String()
	ctx.Header("x-github-request-id", requestID)
	ctx.JSON(http.StatusOK, gin.H{
		"mcp_registries": []interface{}{
			gin.H{
				"url":             "https://api.github.com/copilot/mcp",
				"registry_access": "allow_all",
			},
		},
	})
}

// GetCopilotInternalUser 获取 Copilot 内部用户信息
func GetCopilotInternalUser(ctx *gin.Context) {
	requestID := uuid.Must(uuid.NewV4()).String()
	ctx.Header("x-github-request-id", requestID)

	userDisplayName := "github"
	token, _ := jwtpkg.GetJwtProto(ctx, &middleware.UserLoad{})
	if token != nil && token.UserDisplayName != "" {
		userDisplayName = token.UserDisplayName
	}

	ctx.JSON(http.StatusOK, gin.H{
		"login":                   userDisplayName,
		"access_type_sku":         "free_educational",
		"copilot_plan":            "individual",
		"analytics_tracking_id":   generateTrackingID(),
		"assigned_date":           generateAssignedDate(),
		"can_signup_for_limited":  false,
		"chat_enabled":            true,
		"organization_login_list": []interface{}{},
		"organization_list":       []interface{}{},
	})
}
