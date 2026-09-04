package copilot

import (
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gofrs/uuid"
	"github.com/tidwall/gjson"
)

// GetModel 根据 ID 获取单个模型的详细信息，从上游接口动态获取模型数据
func GetModel(ctx *gin.Context) {
	requestID := uuid.Must(uuid.NewV4()).String()
	ctx.Header("x-github-request-id", requestID)

	// 从完整路径提取模型 ID，保留多段路径（如 Qwen/Qwen3.6-35B-A3B 中的 Qwen/ 前缀）
	path := ctx.GetString("full_request_path")
	if path == "" {
		path = ctx.Request.URL.Path
	}
	modelID := path
	if idx := strings.Index(path, "/models/"); idx != -1 {
		modelID = path[idx+len("/models/"):]
	} else if idx := strings.LastIndex(modelID, "/"); idx != -1 {
		modelID = modelID[idx+1:]
	}

	// /models/session POST → 返回自动模式会话模型信息
	if modelID == "session" || modelID == "auto" {
		GetModelsSession(ctx)
		return
	}

	// URL 解码
	if decoded, err := url.QueryUnescape(modelID); err == nil {
		modelID = decoded
	}

	// 动态构建模型列表并查找
	jsonData, err := BuildModelList()
	if err != nil {
		log.Printf("[model] <<< RESPONSE id=%s status=500 err=build_model_list_failed: %v", requestID, err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "无法获取模型列表数据"})
		return
	}

	// gjson 按 id 查找（遍历比较，避免 # 语法对特殊字符的解析问题）
	ids := gjson.GetBytes(jsonData, "data.#.id").Array()

	// 先精确匹配完整 ID（如 Qwen/Qwen3.6-35B-A3B）
	for i, idItem := range ids {
		if idItem.String() == modelID {
			writeModelItem(ctx, jsonData, i)
			return
		}
	}

	// 再按 ID 末段匹配，兼容带/不带组织前缀的请求
	// （如请求 Qwen3.6-35B-A3B 时匹配 Qwen/Qwen3.6-35B-A3B，反之亦然）
	requested := lastIDSegment(modelID)
	for i, idItem := range ids {
		if lastIDSegment(idItem.String()) == requested {
			writeModelItem(ctx, jsonData, i)
			return
		}
	}

	ctx.JSON(http.StatusNotFound, gin.H{"error": "model not found: " + modelID})
	log.Printf("[model] <<< RESPONSE id=%s status=404 err=model_not_found requested=%s", requestID, modelID)
}

// lastIDSegment 返回模型 ID 的最后一段（去掉 / 前的组织前缀）
func lastIDSegment(id string) string {
	if idx := strings.LastIndex(id, "/"); idx != -1 {
		return id[idx+1:]
	}
	return id
}

// writeModelItem 将 jsonData 中 data[i] 的模型 JSON 写回响应
func writeModelItem(ctx *gin.Context, jsonData []byte, i int) {
	item := gjson.GetBytes(jsonData, "data."+strconv.Itoa(i))
	ctx.Header("Content-Type", "application/json")
	_, _ = ctx.Writer.Write([]byte(item.Raw))
}

// GetModelsFromJson 获取模型列表，从上游接口动态获取模型数据
func GetModelsFromJson(ctx *gin.Context) {
	requestID := uuid.Must(uuid.NewV4()).String()
	ctx.Header("x-github-request-id", requestID)

	jsonData, err := BuildModelList()
	if err != nil {
		log.Printf("[model] >>> REQUEST id=%s status=500 err=build_model_list_failed: %v", requestID, err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "无法获取模型列表数据"})
		return
	}

	ctx.Header("Content-Type", "application/json")
	ctx.Data(http.StatusOK, "application/json", jsonData)
}
