package middleware

import (
	"encoding/json"
	"fmt"
	"ghecopilot/internal/app/github_auth"
	"ghecopilot/internal/cache"
	"ghecopilot/internal/response"
	jwtpkg "ghecopilot/pkg/jwt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type OAuthCheck struct {
	ClientId   string `json:"client_id" form:"client_id"`
	DeviceCode string `json:"device_code" form:"device_code"`
	Code       string `json:"code" form:"code"`
	GrantType  string `json:"grant_type" form:"grant_type"`
}

func DeviceCodeCheckAuth(ctx *gin.Context) {
	checkInfo := &OAuthCheck{}
	if err := ctx.ShouldBind(&checkInfo); err != nil {
		response.FailJson(ctx, response.FailStruct{
			Code: -1,
			Msg:  "Invalid client id.",
		}, false)
		ctx.Abort()
		return
	}

	log.Printf("[DeviceCodeCheckAuth] POST /login/oauth/access_token - client_id=%q device_code=%q code=%q grant_type=%q",
		checkInfo.ClientId, checkInfo.DeviceCode, checkInfo.Code, checkInfo.GrantType)

	// 处理 OAuth 授权码流程 (VS Code GitHub Enterprise 登录)
	// VS Code 发送 code (authorization_code) 而不是 device_code
	if checkInfo.DeviceCode == "" && checkInfo.Code != "" {
		oauthCodeInfo, err := github_auth.GetOAuthCodeInfoByClientIdAndCode(checkInfo.ClientId, checkInfo.Code)
		if err == nil {
			log.Printf("[DeviceCodeCheckAuth] OAuth code found via GetOAuthCodeInfoByClientIdAndCode")
			ctx.Set("client_auth_info", oauthCodeInfo)
			ctx.Next()
			return
		}
		log.Printf("[DeviceCodeCheckAuth] GetOAuthCodeInfoByClientIdAndCode failed: %v", err)
	}

	// 设备码流程：尝试通过 device_code 查找已完成的授权
	if checkInfo.DeviceCode != "" {
		info, err := github_auth.GetClientAuthInfoByDeviceCode(checkInfo.DeviceCode)
		if err == nil && info != nil && info.CardCode != "" {
			ctx.Set("client_auth_info", info)
			ctx.Next()
			return
		}
	}

	// 桥接: 尝试从 OAuth 缓存查找是否已通过 /redirect 登录
	if checkInfo.ClientId != "" {
		cacheKey := "copilot.proxy.oauth_" + checkInfo.ClientId
		oauthData, err := cache.Get(cacheKey)
		if err == nil && oauthData != nil {
			if data, ok := oauthData.([]byte); ok {
				oauthInfo := &github_auth.ClientOAuthInfo{}
				if json.Unmarshal(data, oauthInfo) == nil {
					log.Printf("[DeviceCodeCheckAuth] OAuth code found via bridge cache %s", cacheKey)
					ctx.Set("client_auth_info", oauthInfo)
					ctx.Next()
					return
				}
			}
		} else if err != nil {
			log.Printf("[DeviceCodeCheckAuth] Bridge cache miss for %s: %v", cacheKey, err)
		}
	}

	// 未完成授权，返回 pending
	ctx.JSON(http.StatusOK, gin.H{
		"error":             "authorization_pending",
		"error_description": "The authorization request is still pending.",
		"error_uri":         "https://docs.github.com/developers/apps/authorizing-oauth-apps#error-codes-for-the-device-flow",
	})
	ctx.Abort()
}

func AuthCodeFlowCheckAuth(ctx *gin.Context) {
	checkInfoClient := &github_auth.ClientOAuthInfo{}
	err := ctx.Bind(&checkInfoClient)
	if err != nil {
		response.FailJson(ctx, response.FailStruct{
			Code: -1,
			Msg:  "Invalid client id.",
		}, false)
		ctx.Abort()
		return
	}
	oauthCodeInfo, err := github_auth.GetOAuthCodeInfoByClientIdAndCode(checkInfoClient.ClientId, checkInfoClient.Code)
	if err != nil {
		response.FailJson(ctx, response.FailStruct{
			Code: -1,
			Msg:  "Invalid client id.",
		}, false)
		ctx.Abort()
		return
	}

	ctx.Set("client_auth_info", oauthCodeInfo)
	ctx.Next()
}

func AccessTokenCheckAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Request.Header.Get("Authorization")
		if token == "" {
			response.FailJsonAndStatusCode(c, http.StatusForbidden, response.NoAccess, false)
			c.Abort()
			return
		}
		last := strings.Index(token, " ")
		if len(token) < last || last == -1 {
			response.FailJsonAndStatusCode(c, http.StatusForbidden, response.TokenWrongful, false)
			c.Abort()
			return
		}
		token = token[last+1:]
		chk, jwter, err := jwtpkg.CheckToken(token, &UserLoad{}, "user")
		if err != nil {
			errmsg := response.TokenWrongful
			errmsg.Msg = "令牌验证错误"
			response.FailJsonAndStatusCode(c, http.StatusForbidden, errmsg, true, err.Error())
			c.Abort()
			return
		}
		if !chk {
			response.FailJsonAndStatusCode(c, http.StatusForbidden, response.NoAccess, true, "破损令牌")
			c.Abort()
			return
		}
		chs := true
		issuerStr := ""
		issuerStr, err = jwter.GetIssuer()
		if err != nil {
			chs = false
			c.Abort()
			return
		}
		if issuerStr != "" && issuerStr != "user" {
			chs = false
			c.Abort()
			return
		}
		if !chs {
			errmsg := response.TokenWrongful
			errmsg.Msg = "签名错误"
			response.FailJsonAndStatusCode(c, http.StatusForbidden, errmsg, true, err.Error())
			c.Abort()
			return
		}
		c.Set("token", jwter)
		c.Set("tokenStr", token)
		c.Set("token.issuer", issuerStr)
		c.Next()
	}
}

func TokenCheckAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Request.Header.Get("Authorization")
		if token == "" {
			response.FailJsonAndStatusCode(c, http.StatusUnauthorized, response.TokenWrongful, false)
			c.Abort()
			return
		}
		last := strings.Index(token, " ")
		if len(token) < last || last == -1 {
			response.FailJsonAndStatusCode(c, http.StatusUnauthorized, response.TokenWrongful, false)
			c.Abort()
			return
		}
		token = token[last+1:]
		parsedToken := parseAuthorizationToken(token)
		// 校验exp是否过期
		expired, err := isExpired(parsedToken["exp"])
		if err != nil {
			response.FailJsonAndStatusCode(c, http.StatusUnauthorized, response.TokenWrongful, false)
			c.Abort()
			return
		} else {
			if expired {
				response.FailJsonAndStatusCode(c, http.StatusUnauthorized, response.TokenOverdue, false)
				c.Abort()
				return
			}
		}
		rawToken := github_auth.JsonMap2Token(map[string]interface{}{
			"tid":  parsedToken["tid"],
			"exp":  parsedToken["exp"],
			"sku":  parsedToken["sku"],
			"st":   parsedToken["st"],
			"chat": parsedToken["chat"],
			"u":    parsedToken["u"],
		})
		sign := "1:" + github_auth.Token2Sign(rawToken)
		if sign != parsedToken["8kp"] {
			response.FailJsonAndStatusCode(c, http.StatusUnauthorized, response.TokenWrongful, false)
			c.Abort()
			return
		}
		c.Next()
	}
}

func parseAuthorizationToken(token string) map[string]string {
	result := make(map[string]string)
	pairs := strings.Split(token, ";")

	for _, pair := range pairs {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			key := kv[0]
			value := kv[1]

			if key == "tid" || key == "exp" || key == "sku" || key == "st" || key == "8kp" || key == "chat" || key == "u" {
				result[key] = value
			}
		}
	}

	return result
}

func isExpired(expStr string) (bool, error) {
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return false, fmt.Errorf("invalid exp timestamp: %v", err)
	}

	now := time.Now().Unix()
	return now > exp, nil
}
