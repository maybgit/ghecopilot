package router

import (
	"bytes"
	"fmt"
	authApi "ghecopilot/internal/controller/auth"
	"ghecopilot/internal/controller/copilot"
	"ghecopilot/internal/middleware"
	"ghecopilot/static"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// var uuidRegex = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// modelEntry 一条模型映射记录
type modelEntry struct {
	model  string
	expiry time.Time
}

// modelCache 带 TTL 的并发安全缓存，后台 goroutine 定期清理过期项
type modelCache struct {
	mu      sync.Mutex
	entries map[string]modelEntry
	ttl     time.Duration
	max     int // 条目上限，防止异常刷 UUID 导致内存膨胀
}

func newModelCache(ttl time.Duration, max int) *modelCache {
	// time.NewTicker 要求 d > 0, 提前防御
	if ttl <= 0 {
		panic("modelCache: ttl must be positive")
	}
	c := &modelCache{
		entries: make(map[string]modelEntry),
		ttl:     ttl,
		max:     max,
	}
	// 每 ttl/2 启动一次清理，清理成本很低
	go func() {
		ticker := time.NewTicker(ttl / 2)
		defer ticker.Stop()
		for range ticker.C {
			c.cleanup()
		}
	}()
	return c
}

// getOrSet 返回该 key 已记录的 model；没有则记录 curr 并返回 curr
// 命中时滑动续期，保证长会话中途不丢记录
func (c *modelCache) getOrSet(key, curr string) (string, bool) {
	// 空 key（请求无 Baggage 头）或空模型直接跳过不存储,
	// 避免所有无 Baggage 的请求共享同一条记录互相覆盖
	if key == "" || curr == "" {
		return curr, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if e, has := c.entries[key]; has {
		e.expiry = now.Add(c.ttl) // 访问即续期
		c.entries[key] = e
		return e.model, true
	}
	// 达到上限时丢弃任一条目（map 遍历顺序随机, O(1) 近似淘汰,
	// 正常稳态条目数远小于 max, 此分支几乎不会触发, 纯兜底）
	if c.max > 0 && len(c.entries) >= c.max {
		for k := range c.entries {
			delete(c.entries, k)
			break
		}
	}
	c.entries[key] = modelEntry{model: curr, expiry: now.Add(c.ttl)}
	return curr, false
}

func (c *modelCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, e := range c.entries {
		if now.After(e.expiry) {
			delete(c.entries, k)
		}
	}
}

func ignoreLanguage(str string) string {
	var filtered []string
	var ignore bool
	br := "\n"
	lines := strings.Split(str, br)
	for _, v := range lines {
		if strings.HasPrefix(v, "```<language>") || strings.HasPrefix(v, "```csharp") {
			ignore = true
			continue
		}

		if ignore && strings.HasPrefix(v, "```") {
			ignore = false
			continue
		}
		filtered = append(filtered, v)
	}
	str = strings.Join(filtered, br)
	return str
}

// mapModels 以 interactionId 为键记录模型，30 分钟无访问自动过期
var mapModels = newModelCache(30*time.Minute, 100000)

// 基于Host的反向代理中间件，代理ollama请求到另一个兼容的openai服务
func OpenAIProxy(r *gin.Engine) {
	proxyKey := os.Getenv("UPSTREAM_API_KEY")
	proxyTarget := os.Getenv("UPSTREAM_API_BASE_URL")
	if proxyTarget == "" {
		return
	}

	proxyURL, err := url.Parse(proxyTarget)
	if err != nil {
		panic("UPSTREAM_API_BASE_URL 配置无效: " + err.Error())
	}

	// 从环境变量读取对话服务模型参数
	copilotAutoModel := os.Getenv("COPILOT_AUTO_MODEL")
	copilotChatDefaultModel := os.Getenv("COPILOT_CHAT_DEFAULT_MODEL")
	repPenalty, _ := strconv.ParseFloat(os.Getenv("CHAT_REPETITION_PENALTY"), 64)
	temperature, _ := strconv.ParseFloat(os.Getenv("CHAT_TEMPERATURE"), 64)
	topP, _ := strconv.ParseFloat(os.Getenv("CHAT_TOP_P"), 64)
	customParamsModels := os.Getenv("CHAT_CUSTOM_PARAMS_MODELS") + ","

	log.Printf("[CONFIG] CHAT_REPETITION_PENALTY=%f, CHAT_TEMPERATURE=%f, CHAT_TOP_P=%f, COPILOT_AUTO_MODEL: %s", repPenalty, temperature, topP, copilotAutoModel)

	proxy := &httputil.ReverseProxy{
		Rewrite: func(preq *httputil.ProxyRequest) {
			// 路由到目标 URL
			preq.SetURL(proxyURL)
			preq.Out.Header.Set("Authorization", "Bearer "+proxyKey)

			// 保留原始 Host（与 NewSingleHostReverseProxy 行为一致）
			host := preq.In.Header.Get("X-Forwarded-Host")
			if host != "" {
				preq.Out.Host = host
			} else {
				preq.Out.Host = preq.In.Host
			}

			// 清洗路径：去除多余的反斜杠
			preq.Out.URL.Path = path.Clean(preq.Out.URL.Path)
			preq.Out.URL.RawPath = ""
			if preq.Out.URL.Path == "/chat/completions" {
				preq.Out.URL.Path = "/v1/chat/completions"
			}
		},
		// 处理客户端断开连接时的 panic，避免 recovery 中间件打印无用日志
		// 当客户端（如 VS Code Copilot）断开连接时，ReverseProxy 会触发 panic(http.ErrAbortHandler)
		// 这是正常行为，应该静默忽略
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if err == http.ErrAbortHandler {
				return
			}
			log.Printf("[PROXY ERROR] Proxy error: %v", err)
		},
	}

	// 在路由注册前拦截ollama的请求
	r.Use(func(c *gin.Context) {
		// log.Println("In Authorization: ", c.Request.Header.Get("Authorization"))
		host := c.Request.Host
		if host == "localhost:11434" ||
			(host == "api.githubcopilot.com" && (c.Request.URL.Path == "/responses" || c.Request.URL.Path == "/chat/completions")) ||
			(host == "api.my.ghe.com" && strings.HasPrefix(c.Request.URL.Path, "/chat/completions")) {

			// 更改tools edit_file 描述description
			if strings.HasSuffix(c.Request.URL.Path, "/chat/completions") {
				interactionId := c.Request.Header.Get("X-Interaction-Id")

				body, _ := io.ReadAll(c.Request.Body)

				// 移除message数组0中带```的符号
				content0 := gjson.GetBytes(body, `messages.0.content`)
				if content0.Exists() {
					new_content0 := ignoreLanguage(content0.Str)
					body, _ = sjson.SetBytes(body, `messages.0.content`, new_content0)
				}

				// 移除 edit_file 工具中带```的符号
				editFileDesc := gjson.GetBytes(body, `tools.#(function.name=="edit_file").function.parameters.properties.code.description`)
				if editFileDesc.Exists() {
					new_editFileDesc := ignoreLanguage(editFileDesc.Str)
					body, _ = sjson.SetBytes(body, `tools.#(function.name=="edit_file").function.parameters.properties.code.description`, new_editFileDesc)
				}

				model := gjson.GetBytes(body, "model").Str
				log.Printf("X-Interaction-Id: %s, request model: %s", interactionId, model)

				// 如果当前model=auto，就设置为环境变量指定的model
				if model == "auto" || model == "" {
					model = copilotAutoModel
					body, _ = sjson.SetBytes(body, "model", copilotAutoModel)
					log.Printf("X-Interaction-Id: %s, set model: %s", interactionId, copilotAutoModel)
				}

				// copilot cli 不选择任何模型，default 情况下会传可能在models不存在的模型
				// 比如claude-sonnet-4等，这里判断下，不存在设置为环境变量COPILOT_CHAT_DEFAULT_MODEL设置的模型，不然提交到上游报503错误，提示模型不存在
				jsonData, _ := copilot.BuildModelList()
				item := gjson.GetBytes(jsonData, `data.#(id=="`+model+`")`)
				if !item.Exists() {
					log.Printf("X-Interaction-Id: %s, %s does not exist, set model: %s", interactionId, model, copilotChatDefaultModel)
					model = copilotChatDefaultModel
					body, _ = sjson.SetBytes(body, "model", copilotChatDefaultModel)
				}

				// 同一请求多轮agent保持模型一致，有的时候，你指定的是本地部署的模型，比如Qwen3.8-27B随便用，token完全自由
				// 但在同一请求多轮agent任务的时候，有的时候会切换到其它的收费模型，可能你并不想自动切换使用收费的模型
				if firstModel, has := mapModels.getOrSet(interactionId, model); has && model != firstModel {
					body, _ = sjson.SetBytes(body, "model", firstModel)
					log.Printf("X-Interaction-Id: %s, model: %s，firstModel: %s，set model: %s", interactionId, model, firstModel, firstModel)
				}

				// 模型是CHAT_CUSTOM_PARAMS_MODELS变量指定的模型时才设置相关的参数
				if strings.Contains(customParamsModels, model+",") {
					// body, _ = sjson.SetBytes(body, "reasoning_effort", "xhigh")
					// body, _ = sjson.SetBytes(body, "enable_thinking", true)

					if repPenalty > 0 {
						body, _ = sjson.SetBytes(body, "repetition_penalty", repPenalty)
					}

					if temperature > 0 {
						body, _ = sjson.SetBytes(body, "temperature", temperature)
					}

					if topP > 0 {
						body, _ = sjson.SetBytes(body, "top_p", topP)
					}
				}

				c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
				c.Request.ContentLength = int64(len(body))
				c.Request.Header.Del("Transfer-Encoding")

				logDir := fmt.Sprintf("logs/%s", interactionId)
				os.MkdirAll(logDir, 0755)

				uid := uuid.NewString()
				s14 := time.Now().Format("20060102150405")
				reqJsonFileName := fmt.Sprintf("%s/%s-%s.json", logDir, s14, uid)
				go func() {
					os.WriteFile(reqJsonFileName, body, 0644)
				}()
			}

			log.Printf("[PROXY] %s => %s%s", host, proxyTarget, c.Request.URL.Path)
			// ReverseProxy 在客户端断开连接时会 panic(http.ErrAbortHandler)，
			// 这是 net/http 表示"安静中止"的专用信号，本应被 net/http server 静默恢复。
			// 但 gin 的 Recovery 中间件夹在中间会先捕获它并打印 panic 堆栈，
			// 因此在此处静默 recover 该特定 panic，其余 panic 继续向上抛出。
			func() {
				defer func() {
					if err := recover(); err != nil && err != http.ErrAbortHandler {
						panic(err)
					}
				}()
				proxy.ServeHTTP(c.Writer, c.Request)
			}()
			c.Abort()
		} else {
			log.Printf("[LOCAL] 本地处理: %s %s (Host: %s)", c.Request.Method, c.Request.URL.Path, c.Request.Host)
			c.Next()
		}
	})
}

func NewHTTPRouter(r *gin.Engine) {
	OpenAIProxy(r)

	rootRouter := r.Group("/")
	tmpl := template.Must(template.New("").ParseFS(static.Public, "public/*.html"))
	r.SetHTMLTemplate(tmpl)

	apiRouter := r.Group("/api")

	rootRouter.Use(middleware.Cors())
	apiRouter.Use(middleware.Cors())

	authApi.GinApi(rootRouter)
	copilot.GinApi(rootRouter)

}
