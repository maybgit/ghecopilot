package auth

import (
	_ "embed"
	"fmt"
	"ghecopilot/internal/app/github_auth"
	"ghecopilot/internal/middleware"
	"ghecopilot/internal/response"
	jwtpkg "ghecopilot/pkg/jwt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

type postLoginDeviceCodeRequest struct {
	ClientId string `json:"client_id" form:"client_id"`
}

type postLoginDeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`      // 设备代码
	UserCode        string `json:"user_code"`        // 用户代码
	VerificationUrl string `json:"verification_uri"` // 验证地址
	ExpiresIn       int    `json:"expires_in"`       // 过期时间
	Interval        int    `json:"interval"`         // 间隔时间
}

type loginDeviceRequestInfo struct {
	Code            string `json:"code"`
	Authorization   string `json:"authorization"`
	DisplayUserName string `json:"displayUserName,omitempty"`
	Password        string `json:"password"`
}

func postLoginDeviceCode(ctx *gin.Context) {
	cli := postLoginDeviceCodeRequest{}
	if err := ctx.ShouldBind(&cli); err != nil {
		response.FailJson(ctx, response.FailStruct{
			Code: -1,
			Msg:  "Invalid client id.",
		}, false)
		return
	}

	if cli.ClientId == "" {
		response.FailJson(ctx, response.FailStruct{
			Code: -1,
			Msg:  "Client id is required.",
		}, false)
		return
	}

	uid, devid, err := github_auth.BindClientToCode(cli.ClientId, 1800)
	if err != nil {
		response.FailJson(ctx, response.FailStruct{
			Code: -1,
			Msg:  err.Error(),
		}, false)
		return
	}
	ctx.JSON(http.StatusOK, postLoginDeviceCodeResponse{
		DeviceCode:      devid,
		UserCode:        uid,
		VerificationUrl: fmt.Sprintf("%s/login/device?user_code=%s", os.Getenv("COPILOT_MAIN_BASE_URL"), uid),
		ExpiresIn:       1800,
		Interval:        5,
	})
}

func postLoginOauthAccessToken(ctx *gin.Context) {
	v, exists := ctx.Get("client_auth_info")
	if !exists {
		ctx.JSON(http.StatusOK, gin.H{
			"error":             "authorization_pending",
			"error_description": "The authorization request is still pending.",
			"error_uri":         "https://docs.github.com/developers/apps/authorizing-oauth-apps#error-codes-for-the-device-flow",
		})
		return
	}

	var cardCode string
	var clientId string
	var displayUserName string
	var scope string

	// 处理两种可能的类型: ClientAuthInfo (设备码流程) 和 ClientOAuthInfo (OAuth授权码流程)
	switch info := v.(type) {
	case *github_auth.ClientAuthInfo:
		clientId = info.ClientId
		displayUserName = info.DisplayUserName
		u, err := github_auth.GetClientAuthInfo(info.UserCode)
		if err != nil {
			ctx.JSON(http.StatusOK, gin.H{
				"error":             "access_denied",
				"error_description": "You must make a new request for a device code.",
				"error_uri":         "https://docs.github.com/developers/apps/authorizing-oauth-apps#error-codes-for-the-device-flow",
			})
			return
		}
		cardCode = u.CardCode
		_ = github_auth.RemoveClientAuthInfoByDeviceCode(info.ClientId)
	case *github_auth.ClientOAuthInfo:
		clientId = info.ClientId
		cardCode = info.Code
		scope = info.Scope
		log.Printf("[postLoginOauthAccessToken] OAuth code flow: clientId=%s scope=%s", clientId, scope)
	default:
		ctx.JSON(http.StatusOK, gin.H{
			"error":             "authorization_pending",
			"error_description": "The authorization request is still pending.",
			"error_uri":         "https://docs.github.com/developers/apps/authorizing-oauth-apps#error-codes-for-the-device-flow",
		})
		return
	}

	t := time.Now().Add(100 * 365 * 24 * time.Hour)
	tk, _ := jwtpkg.CreateToken(&middleware.UserLoad{
		UserDisplayName:  displayUserName,
		CardCode:         cardCode,
		Client:           clientId,
		RegisteredClaims: jwtpkg.CreateStandardClaims(t.Unix(), "user"),
	})

	ctx.JSON(http.StatusOK, gin.H{
		"access_token": tk,
		"scope":        scope,
		"token_type":   "bearer",
	})
	log.Printf("[postLoginOauthAccessToken] success: access_token issued for client=%s scope=%s", clientId, scope)
}

func postLoginDevice(ctx *gin.Context) {
	var info loginDeviceRequestInfo
	if err := response.BindStruct(ctx, &info); err != nil {
		response.FailJson(ctx, response.FailStruct{
			Code: 422,
			Msg:  "请求参数错误",
		}, false)
		return
	}
	// 验证密码
	loginPassword := os.Getenv("LOGIN_PASSWORD")
	if loginPassword != "" && info.Password != loginPassword {
		response.FailJson(ctx, response.FailStruct{
			Code: 422,
			Msg:  "访问密码错误",
		}, false)
		return
	}
	// 检查code是否存在
	authInfo, err := github_auth.GetClientAuthInfo(info.Code)
	if err != nil {
		response.FailJson(ctx, response.FailStruct{
			Code: 422,
			Msg:  "授权码填写错误",
		}, false)
		return
	}

	err = github_auth.UpdateClientAuthStatusByDeviceCode(authInfo.DeviceCode, info.Authorization, info.DisplayUserName)
	if err != nil {
		response.FailJson(ctx, response.FailStruct{
			Code: 500,
			Msg:  "系统异常, 请稍后再试",
		}, false)
		return
	}
	response.SuccessJson(ctx, "ok")
}

func getLoginDevice(ctx *gin.Context) {
	ctx.Header("Content-Type", "text/html; charset=utf-8")
	ctx.HTML(http.StatusOK, "code.html", gin.H{})
}

func getHelpPage(ctx *gin.Context) {
	ctx.Header("Content-Type", "text/html; charset=utf-8")
	ctx.HTML(http.StatusOK, "help.html", gin.H{})
}
