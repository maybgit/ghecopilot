package copilot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"ghecopilot/pkg/httpclient"
)

const contentTypeJSON = "application/json"

// EmbeddingRequest 表示向嵌入API发送的请求
type EmbeddingRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions"`
}

// EmbeddingResponse 表示从嵌入API接收的响应
type EmbeddingResponse struct {
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Object string          `json:"object"`
	Usage  Usage           `json:"usage"`
}

// EmbeddingData 表示单个嵌入数据
type EmbeddingData struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
	Object    string    `json:"object"`
}

// Usage 表示API使用情况
type Usage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// 移除未使用的类型
// Parameters 和 EmbeddingsRequest, EmbeddingsResponse 已被移除

// EmbeddingClient 封装了与嵌入API交互的功能
type EmbeddingClient struct {
	apiURL      string
	apiKey      string
	model       string
	dimensions  int
	httpClient  *http.Client
	clientMutex sync.RWMutex
}

// NewEmbeddingClient 创建一个新的嵌入客户端
func NewEmbeddingClient(dimensions int) (*EmbeddingClient, error) {
	apiURL := os.Getenv("UPSTREAM_API_BASE_URL") + "/v1/embeddings"
	apiKey := os.Getenv("UPSTREAM_API_KEY")

	if apiURL == "" || apiKey == "" {
		return nil, fmt.Errorf("UPSTREAM_API_BASE_URL or UPSTREAM_API_KEY environment variable not set")
	}

	if os.Getenv("EMBEDDING_MODEL") == "" {
		return nil, fmt.Errorf("EMBEDDING_MODEL environment variable not set")
	}

	if dimensions == 0 {
		dimensions, _ = strconv.Atoi(os.Getenv("EMBEDDING_DIMENSION_SIZE"))
	}

	return &EmbeddingClient{
		apiURL:     apiURL,
		apiKey:     apiKey,
		model:      os.Getenv("EMBEDDING_MODEL"),
		dimensions: dimensions,
		httpClient: httpclient.Client(),
	}, nil
}

// GetEmbedding 获取单个文本的嵌入
func (c *EmbeddingClient) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	resp, err := c.GetEmbeddings(ctx, []string{text})
	if err != nil {
		return nil, err
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}

	return resp.Data[0].Embedding, nil
}

// GetEmbeddings 批量获取多个文本的嵌入
func (c *EmbeddingClient) GetEmbeddings(ctx context.Context, texts []string) (*EmbeddingResponse, error) {
	c.clientMutex.RLock()
	dimensions := c.dimensions
	apiKey := c.apiKey
	model := c.model
	c.clientMutex.RUnlock()

	// When dimensions=0, use the model's default dimension (most models use 1536)
	if dimensions == 0 {
		dimensions = 1536
	}

	log.Printf("[embeddings] upstream request: model=%q input_count=%d dimensions=%d api_url=%s",
		model, len(texts), dimensions, c.apiURL)

	// Log first N input strings for debugging (avoid logging very long inputs)
	logInputs := make([]string, len(texts))
	for i, t := range texts {
		const maxLen = 200
		if len(t) > maxLen {
			logInputs[i] = t[:maxLen] + "..."
		} else {
			logInputs[i] = t
		}
	}
	//log.Printf("[embeddings] upstream input[0..%d]: %v", len(logInputs)-1, logInputs)

	reqBody := EmbeddingRequest{
		Model:      model,
		Input:      texts,
		Dimensions: dimensions,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("[embeddings] failed to marshal request: %v", err)
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("[embeddings] failed to create request: %v", err)
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", contentTypeJSON)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	log.Printf("[embeddings] sending request to upstream: %s", req.URL.String())
	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("[embeddings] HTTP request failed after %v: %v", time.Since(start).Round(time.Millisecond), err)
		return nil, fmt.Errorf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	log.Printf("[embeddings] upstream responded: status=%d elapsed=%v", resp.StatusCode, time.Since(start).Round(time.Millisecond))

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[embeddings] failed to read response body: %v", err)
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[embeddings] upstream error: status=%d body=%s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var embeddingResp EmbeddingResponse
	if err := json.Unmarshal(body, &embeddingResp); err != nil {
		log.Printf("[embeddings] failed to unmarshal response body=%s: %v", string(body), err)
		return nil, fmt.Errorf("failed to unmarshal response: %v", err)
	}

	log.Printf("[embeddings] upstream success: data_count=%d model=%q usage=%+v",
		len(embeddingResp.Data), embeddingResp.Model, embeddingResp.Usage)
	return &embeddingResp, nil
}
