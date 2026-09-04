package copilot

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"ghecopilot/internal/app/github_auth"

	"github.com/gin-gonic/gin"
	"github.com/gofrs/uuid"
)

// GetDisguiseCopilotInternalV2Token 返回伪装的token
func GetDisguiseCopilotInternalV2Token(ctx *gin.Context) {
	requestID := uuid.Must(uuid.NewV4()).String()
	ctx.Header("x-github-request-id", requestID)

	trackingId, _ := uuid.NewV4()
	now := time.Now().Unix()
	dcAt, _ := strconv.Atoi(os.Getenv("COPILOT_TOKEN_TTL_SECONDS"))
	expiresAt := now + int64(dcAt)
	sku := "copilot_for_business_seat"

	copilotToken := github_auth.JsonMap2SignToken(map[string]interface{}{
		"tid":  trackingId,
		"exp":  expiresAt,
		"sku":  sku,
		"st":   "dotcom",
		"chat": 1,
		"u":    "github",
		// mcp=1 启用 MCP 服务器访问。VS Code workbench 要求该标志显式为 1 才启用 MCP，
		// 缺失或为 0 都会显示"你的组织已禁用MCP服务器访问权限"。
		"mcp": 1,
	})

	endpoints := make(map[string]interface{})
	endpoints["api"] = os.Getenv("COPILOT_API_BASE_URL")
	endpoints["origin-tracker"] = "https://origin-tracker.individual.githubcopilot.com"
	endpoints["proxy"] = os.Getenv("COPILOT_PROXY_BASE_URL")
	endpoints["telemetry"] = os.Getenv("COPILOT_TELEMETRY_BASE_URL")

	gout := gin.H{
		"annotations_enabled":                      true,
		"chat_enabled":                             true,
		"chat_jetbrains_enabled":                   true,
		"code_quote_enabled":                       true,
		"code_review_enabled":                      false,
		"codesearch":                               true,
		"copilot_ide_agent_chat_gpt4_small_prompt": false,
		"copilotignore_enabled":                    false,
		"endpoints":                                endpoints,
		"expires_at":                               expiresAt,
		"individual":                               true,
		"nes_enabled":                              false,
		"prompt_8k":                                true,
		"public_suggestions":                       "disabled",
		"refresh_in":                               1500,
		"sku":                                      sku,
		"snippy_load_test_enabled":                 false,
		"telemetry":                                "disabled",
		"token":                                    copilotToken,
		"tracking_id":                              trackingId,
		"intellij_editor_fetcher":                  false,
		"vsc_electron_fetcher":                     false,
		"vs_editor_fetcher":                        false,
		"vsc_panel_v2":                             false,
		"xcode":                                    true,
		"xcode_chat":                               true,
		"limited_user_quotas":                      nil,
		"limited_user_reset_date":                  nil,
		"vsc_electron_fetcher_v2":                  false,
	}
	ctx.JSON(http.StatusOK, gout)
}
