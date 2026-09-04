package auth

import (
	"encoding/json"
	"ghecopilot/internal/app/github_auth"
	"ghecopilot/internal/cache"
	"ghecopilot/internal/middleware"
	"ghecopilot/internal/response"
	jwtpkg "ghecopilot/pkg/jwt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type getLoginOauthAuthorizeRequest struct {
	ClientId    string `json:"client_id" form:"client_id"`
	Prompt      string `json:"prompt" form:"prompt"`
	RedirectUri string `json:"redirect_uri" form:"redirect_uri"`
	Scope       string `json:"scope" form:"scope"`
	State       string `json:"state" form:"state"`
}

func getLoginOauthAuthorize(ctx *gin.Context) {
	req := getLoginOauthAuthorizeRequest{}
	err := ctx.BindQuery(&req)
	if err != nil {
		response.FailJson(ctx, response.FailStruct{
			Code: -1,
			Msg:  "Invalid request.",
		}, false)
		return
	}

	oauthCode := github_auth.GenDevicesCode(20)
	log.Printf("[getLoginOauthAuthorize] client_id=%q redirect_uri=%q scope=%q state=%q -> code=%q",
		req.ClientId, req.RedirectUri, req.Scope, req.State, oauthCode)
	cai := github_auth.ClientOAuthInfo{
		ClientId: req.ClientId,
		Code:     oauthCode,
		Scope:    req.Scope,
		UserCode: oauthCode, // bridge: store code as userCode so /redirect can find device code entry
	}
	cacheKey := "oauth2_authorize_" + req.ClientId
	caiInfo, _ := json.Marshal(cai)
	err = cache.Set(cacheKey, caiInfo, 300)
	if err != nil {
		response.FailJson(ctx, response.FailStruct{
			Code: -1,
			Msg:  "Internal error.",
		}, false)
		return
	}
	// 用 code 反向映射到 clientId，供 /redirect 使用
	cache.Set("oauth2_code_"+oauthCode, req.ClientId, 300)

	// Redirect to the client's redirect_uri
	if req.RedirectUri == "" {
		// 如果没有 redirect_uri，返回错误而不是重定向循环
		response.FailJson(ctx, response.FailStruct{
			Code: -1,
			Msg:  "redirect_uri is required.",
		}, false)
		return
	}

	// 直接签发 JWT token
	t := time.Now().Add(100 * 365 * 24 * time.Hour)
	tk, _ := jwtpkg.CreateToken(&middleware.UserLoad{
		CardCode:         oauthCode,
		Client:           req.ClientId,
		RegisteredClaims: jwtpkg.CreateStandardClaims(t.Unix(), "user"),
	})

	// 如果 state 是 URL（VS Code 本地回调地址），直接用 JavaScript 跳转过去
	// 绕过 vscode.dev 中转，避免丢失 code
	// 注意：VS 2022 的 state 是会话标识（如 vs.githubsi.xxx），不是 URL，不要处理
	if strings.HasPrefix(req.State, "http://") || strings.HasPrefix(req.State, "https://") {
		redirectURL := req.State
		separator := "?"
		if containsQuery(redirectURL) {
			separator = "&"
		}
		redirectURL += separator + "code=" + url.QueryEscape(oauthCode) +
			"&state=" + url.QueryEscape(req.State) +
			"&access_token=" + url.QueryEscape(tk) +
			"&scope=" + url.QueryEscape(req.Scope) +
			"&token_type=bearer"

		// 同时保存到缓存，以备后续可能的 POST 换取 token
		cai := github_auth.ClientOAuthInfo{
			ClientId: req.ClientId,
			Code:     oauthCode,
			Scope:    req.Scope,
			UserCode: oauthCode,
		}
		caiInfo, _ := json.Marshal(cai)
		cache.Set("copilot.proxy.oauth_"+req.ClientId, caiInfo, 300)

		log.Printf("[getLoginOauthAuthorize] redirecting directly to VS Code local server: %s", redirectURL)
		ctx.Header("Content-Type", "text/html; charset=utf-8")
		ctx.String(http.StatusOK, `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"/>
<meta http-equiv="refresh" content="0; url=`+redirectURL+`">
<title>登录中...</title></head>
<body>
<p>正在跳转到本地认证服务器...</p>
<script>
	window.location.replace("`+redirectURL+`");
</script>
</body>
</html>`)
		return
	}

	browserSessionId := github_auth.GenDevicesCode(64)
	separator := "?"
	if containsQuery(req.RedirectUri) {
		separator = "&"
	}
	redirectURL := req.RedirectUri + separator + "browserSessionId=" + browserSessionId + "&code=" + oauthCode + "&state=" + url.QueryEscape(req.State)
	ctx.Redirect(302, redirectURL)
}

func postLoginOauthAccessTokenForVs2022(ctx *gin.Context) {
	v, exists := ctx.Get("client_auth_info")
	if !exists {
		response.FailJson(ctx, response.FailStruct{
			Code: -1,
			Msg:  "Invalid client id.",
		}, false)
		return
	}
	cliAuthInfo := v.(*github_auth.ClientOAuthInfo)
	t := time.Now().Add(100 * 365 * 24 * time.Hour)
	tk, _ := jwtpkg.CreateToken(&middleware.UserLoad{
		CardCode:         cliAuthInfo.Code,
		Client:           cliAuthInfo.ClientId,
		RegisteredClaims: jwtpkg.CreateStandardClaims(t.Unix(), "user"),
	})
	ctx.JSON(http.StatusOK, gin.H{
		"access_token": tk,
		"scope":        cliAuthInfo.Scope,
		"token_type":   "bearer",
	})
}

func getSiteSha(ctx *gin.Context) {
	ctx.Header("X-GitHub-Request-Id", "C0E1:6A1A:1A1F:2A1D:1A1F:1A1F:1A1F:1A1F")
	ctx.JSON(http.StatusOK, gin.H{})
}

func getLoginConfig(ctx *gin.Context) {
	loginPassword := os.Getenv("LOGIN_PASSWORD")
	ctx.JSON(http.StatusOK, gin.H{
		"is_login_password": loginPassword != "",
	})
}

func getRedirect(ctx *gin.Context) {
	code := ctx.Query("code")
	state := ctx.Query("state")

	if code == "" {
		response.FailJson(ctx, response.FailStruct{
			Code: -1,
			Msg:  "code is required.",
		}, false)
		return
	}

	oauthInfo, err := github_auth.GetOAuthInfoByCode(code)
	if err != nil {
		log.Printf("[getRedirect] GetOAuthInfoByCode(%q) failed: %v", code, err)
		response.FailJson(ctx, response.FailStruct{
			Code: -1,
			Msg:  "Invalid or expired code.",
		}, false)
		return
	}
	log.Printf("[getRedirect] code=%q state=%q clientId=%q", code, state, oauthInfo.ClientId)

	// 桥接: 把 oauthCode 写入 copilot.proxy.<clientId>，
	// 这样 DeviceCodeCheckAuth 轮询 copilot.proxy.<deviceCode> 时 CardCode 非空，
	// 进而能查到 oauth2_authorize_<clientId> 拿到完整 OAuth 信息
	oauthData, _ := json.Marshal(oauthInfo)
	cache.Set("copilot.proxy.oauth_"+oauthInfo.ClientId, oauthData, 300)

	// 如果 state 参数是一个 URL (VS Code 本地回调服务器地址)，则返回 HTML 页面
	// 使用 JavaScript 进行客户端重定向，避免服务器端 302 被浏览器或代理阻止
	// 注意：VS 2022 的 state 是会话标识（如 vs.githubsi.xxx），不是 URL，不要处理
	if strings.HasPrefix(state, "http://") || strings.HasPrefix(state, "https://") {
		redirectURL := state

		// 直接在这里签发 JWT token，通过回调 URL 传给 VS Code 本地服务器
		t := time.Now().Add(100 * 365 * 24 * time.Hour)
		tk, _ := jwtpkg.CreateToken(&middleware.UserLoad{
			CardCode:         oauthInfo.Code,
			Client:           oauthInfo.ClientId,
			RegisteredClaims: jwtpkg.CreateStandardClaims(t.Unix(), "user"),
		})

		if containsQuery(redirectURL) {
			redirectURL += "&code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state) +
				"&access_token=" + url.QueryEscape(tk) + "&scope=" + url.QueryEscape(oauthInfo.Scope) + "&token_type=bearer"
		} else {
			redirectURL += "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state) +
				"&access_token=" + url.QueryEscape(tk) + "&scope=" + url.QueryEscape(oauthInfo.Scope) + "&token_type=bearer"
		}
		log.Printf("[getRedirect] redirecting to VS Code local server: %s", redirectURL)
		ctx.Header("Content-Type", "text/html; charset=utf-8")
		ctx.String(http.StatusOK, `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"/>
<meta http-equiv="refresh" content="0; url=`+redirectURL+`">
<title>登录中...</title></head>
<body>
<p>正在跳转到本地认证服务器...</p>
<script>
	window.location.replace("`+redirectURL+`");
</script>
</body>
</html>`)
		return
	}

	// 兼容旧逻辑：没有 state 时直接返回 JSON
	t := time.Now().Add(100 * 365 * 24 * time.Hour)
	tk, _ := jwtpkg.CreateToken(&middleware.UserLoad{
		CardCode:         oauthInfo.Code,
		Client:           oauthInfo.ClientId,
		RegisteredClaims: jwtpkg.CreateStandardClaims(t.Unix(), "user"),
	})

	ctx.JSON(http.StatusOK, gin.H{
		"access_token": tk,
		"scope":        oauthInfo.Scope,
		"token_type":   "bearer",
	})
}

// containsQuery checks if a URL string already contains a query string.
func containsQuery(rawURL string) bool {
	for i := 0; i < len(rawURL); i++ {
		if rawURL[i] == '?' {
			return true
		}
	}
	return false
}
