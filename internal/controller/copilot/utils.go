package copilot

import (
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gofrs/uuid"
)

type Pong struct {
	Now    int    `json:"now"`
	Status string `json:"status"`
	Ns1    string `json:"ns1"`
}

// GetPing 模拟ping接口
func GetPing(ctx *gin.Context) {
	requestID := uuid.Must(uuid.NewV4()).String()
	ctx.Header("x-github-request-id", requestID)

	ctx.JSON(http.StatusOK, Pong{
		Now:    time.Now().Second(),
		Status: "ok",
		Ns1:    "200 OK",
	})
}

// GetModelsSession 返回自动模式下的会话模型信息（本地，非 GitHub 官方）
func GetModelsSession(ctx *gin.Context) {
	var copilotAutoModel = os.Getenv("COPILOT_AUTO_MODEL")
	requestID := uuid.Must(uuid.NewV4()).String()
	ctx.Header("x-github-request-id", requestID)
	ctx.JSON(http.StatusOK, gin.H{
		"available_models": []string{copilotAutoModel},
		"selected_model":   copilotAutoModel,
		"session_token":    "",
		"expires_at":       0,
		"discounted_costs": gin.H{
			copilotAutoModel: 0.1,
		},
	})
}
