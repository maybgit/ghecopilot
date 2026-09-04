package copilot

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/gofrs/uuid"
)

// EmbeddingsAPIRequest 表示嵌入API的请求结构
type EmbeddingsAPIRequest struct {
	Input      []string `json:"input" binding:"required"`
	Model      string   `json:"model,omitempty"`
	Dimensions int      `json:"dimensions,omitempty"`
}

// HandleEmbeddings 处理嵌入请求的HTTP处理器
func HandleEmbeddings(c *gin.Context) {
	requestID := uuid.Must(uuid.NewV4()).String()
	c.Header("x-github-request-id", requestID)

	// 解析请求体
	var req EmbeddingsAPIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[embeddings] %s failed to bind JSON: %v", requestID, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	log.Printf("[embeddings] %s request: input_count=%d model=%q dimensions=%d",
		requestID, len(req.Input), req.Model, req.Dimensions)

	// Proxy is a gateway to upstream — always use configured upstream model, ignore client's model field
	client, err := NewEmbeddingClient(0)
	if err != nil {
		log.Printf("[embeddings] %s failed to create embedding client: %v", requestID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get embeddings using the client context for cancellation support.
	resp, err := client.GetEmbeddings(c.Request.Context(), req.Input)
	if err != nil {
		log.Printf("[embeddings] %s GetEmbeddings failed: %v", requestID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[embeddings] %s success: data_count=%d model=%q", requestID, len(resp.Data), resp.Model)
	c.JSON(http.StatusOK, resp)
}

// EmbeddingModels 获取可用的嵌入模型列表
func EmbeddingModels(c *gin.Context) {
	modelName := os.Getenv("EMBEDDING_MODEL")
	if modelName == "" {
		modelName = "bge-m3"
	}

	requestID := uuid.Must(uuid.NewV4()).String()
	c.Header("x-github-request-id", requestID)
	c.JSON(http.StatusOK, gin.H{
		"data": []gin.H{
			{"id": modelName, "object": "model", "owned_by": "openai", "permission": []string{}},
		},
		"object": "list",
	})
}
