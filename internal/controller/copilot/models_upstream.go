package copilot

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"ghecopilot/pkg/httpclient"
)

// modelTemplate 单个模型的完整 JSON 模板（与 gen-models-json.ps1 生成的结构一致）。
// id/name/version/capabilities.family 由 applyModelTemplate 通过 sjson 动态写入，
// 以后需要手动调整某个属性时，直接改模板或加一行 sjson.Set 即可
const modelTemplate = `{
  "preview": false,
  "model_picker_enabled": true,
  "name": "",
  "is_chat_fallback": false,
  "billing": {
    "token_prices": {
      "output_price": 200000000000,
      "cache_price": 2500000000,
      "input_price": 25000000000,
      "batch_size": 1000000
    }
  },
  "object": "model",
  "serviceType": "InstantApplyChat",
  "model_picker_category": "versatile",
  "supported_endpoints": [
    "/chat/completions"
  ],
  "is_chat_default": false,
  "policy": {
    "state": "enabled",
    "terms": ""
  },
  "id": "",
  "model_picker_price_category": "low",
  "version": "",
  "capabilities": {
    "supports": {
      "structured_outputs": true,
      "streaming": true,
      "reasoning_effort": [
        "low",
        "medium",
        "high"
      ],
      "vision": true,
      "parallel_tool_calls": true,
      "tool_calls": true
    },
    "tokenizer": "o200k_base",
    "family": "",
    "limits": {
      "max_context_window_tokens": 870400,
      "max_prompt_tokens": 870400,
      "vision": {
        "max_prompt_images": 1,
        "supported_media_types": [
          "image/jpeg",
          "image/png",
          "image/webp",
          "image/gif"
        ],
        "max_prompt_image_size": 3145728
      },
      "max_output_tokens": 8192
    },
    "object": "model_capabilities",
    "type": "chat"
  },
  "vendor": "ModelScope"
}`

// fetchUpstreamModels 从上游 /v1/models 获取模型 ID 列表（带 30s 缓存）
func fetchUpstreamModels() ([]string, error) {
	baseURL := strings.TrimRight(os.Getenv("UPSTREAM_API_BASE_URL"), "/")
	apiKey := os.Getenv("UPSTREAM_API_KEY")
	if baseURL == "" || apiKey == "" {
		return nil, fmt.Errorf("UPSTREAM_API_BASE_URL or UPSTREAM_API_KEY environment variable not set")
	}

	req, err := http.NewRequest(http.MethodGet, baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := httpclient.Client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream /v1/models returned status %d: %s", resp.StatusCode, string(body))
	}

	// gjson 提取 id 列表
	idResult := gjson.GetBytes(body, "data.#.id")
	items := make([]string, 0, len(idResult.Array()))
	for _, idItem := range idResult.Array() {
		if id := idItem.String(); id != "" {
			items = append(items, id)
		}
	}

	return items, nil
}

// modelListCache 模型列表缓存，首次访问时构建，之后一直复用
var modelListCache []byte

// BuildModelList 拉取上游模型列表并构建完整的模型 JSON 列表
func BuildModelList() ([]byte, error) {
	if modelListCache != nil {
		return modelListCache, nil
	}

	var copilotChatDefaultModel = os.Getenv("COPILOT_CHAT_DEFAULT_MODEL")
	var copilotChatFallbackModel = os.Getenv("COPILOT_CHAT_FALLBACK_MODEL")

	ids, err := fetchUpstreamModels()
	if err != nil {
		return nil, err
	}

	// 同时输出两种结构：
	//   - "data"   : OpenAI 风格，聊天模型选择器（_fetchModels 读取 .data）使用
	//   - "models" : CAPI 风格，instant-apply / code-mapper 拉取器
	//                （instantApplyModels getter 读取 .models 并 .filter）使用
	// 两者内容一致，避免客户端因缺少 .models 而报
	// "Cannot read properties of undefined (reading 'filter')"
	list := []byte(`{"data":[],"models":[],"object":"list"}`)
	idx := 0
	for _, id := range ids {
		item, err := applyModelTemplate(id)
		if err != nil {
			log.Printf("[model] 构建模型 %s 失败: %v", id, err)
			continue
		}

		item, _ = sjson.SetBytes(item, "is_chat_default", id == copilotChatDefaultModel)
		item, _ = sjson.SetBytes(item, "is_chat_fallback", id == copilotChatFallbackModel)

		// 按索引追加到 data 与 models 数组（SetRawBytes 保留原始 JSON 结构）
		list, _ = sjson.SetRawBytes(list, fmt.Sprintf("data.%d", idx), item)
		list, _ = sjson.SetRawBytes(list, fmt.Sprintf("models.%d", idx), item)
		idx++
	}

	modelListCache = list
	return list, nil

}

// applyModelTemplate 用 sjson 将模型 ID 写入模板，返回完整模型 JSON
func applyModelTemplate(modelID string) ([]byte, error) {
	item := []byte(modelTemplate)
	var err error
	item, err = sjson.SetBytes(item, "id", modelID)
	if err != nil {
		return nil, err
	}
	item, err = sjson.SetBytes(item, "name", modelID)
	if err != nil {
		return nil, err
	}
	item, err = sjson.SetBytes(item, "version", modelID)
	if err != nil {
		return nil, err
	}
	item, err = sjson.SetBytes(item, "capabilities.family", modelID)
	if err != nil {
		return nil, err
	}
	return item, nil
}
