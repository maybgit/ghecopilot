package middleware

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

const (
	// 404 日志文件路径（相对工作目录，与 main.go 的 logs 目录一致）
	notFoundLogPath = "logs/404.log"
	// 响应体捕获上限（兜底），404 响应通常很短，此值仅防极端情况
	notFoundRespCaptureLimit = 10 * 1024
)

var (
	notFoundLogMu   sync.Mutex
	notFoundLogFile *os.File
)

// notFoundLogRecord 一条 404 请求的完整记录（JSONL 每行一条）
type notFoundLogRecord struct {
	Time         string      `json:"time"`
	Method       string      `json:"method"`
	Path         string      `json:"path"`
	Query        string      `json:"query"`
	URL          string      `json:"url"`
	Host         string      `json:"host"`
	RemoteAddr   string      `json:"remote_addr"`
	Headers      http.Header `json:"headers"`
	Body         string      `json:"body"`
	BodyEncoding string      `json:"body_encoding,omitempty"`
	Status       int         `json:"status"`
	ResponseBody string      `json:"response_body"`
}

// bodyCaptureWriter 包装 gin.ResponseWriter，仅在状态码为 404 时捕获响应体。
// 200 的 SSE 流式响应（如 /chat/completions stream）不会写入 buffer，避免无谓内存占用。
type bodyCaptureWriter struct {
	gin.ResponseWriter
	buf *bytes.Buffer
}

func (w *bodyCaptureWriter) Write(b []byte) (int, error) {
	if w.ResponseWriter.Status() == http.StatusNotFound && w.buf.Len() < notFoundRespCaptureLimit {
		w.buf.Write(b[:min(len(b), notFoundRespCaptureLimit-w.buf.Len())])
	}
	return w.ResponseWriter.Write(b)
}

func (w *bodyCaptureWriter) WriteString(s string) (int, error) {
	if w.ResponseWriter.Status() == http.StatusNotFound && w.buf.Len() < notFoundRespCaptureLimit {
		w.buf.WriteString(s[:min(len(s), notFoundRespCaptureLimit-w.buf.Len())])
	}
	return w.ResponseWriter.WriteString(s)
}

// NotFoundLogger 记录所有最终响应状态码为 404 的请求：
// 覆盖路由未匹配(NoRoute)、handler 内部返回 404、上游代理返回 404 三种来源。
// 请求信息（方法/URL/请求头/完整请求体/响应体）以 JSONL 追加写入 logs/404.log。
func NotFoundLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 读取完整请求体并还原，保证后续 handler 可正常读取
		var body []byte
		if c.Request.Body != nil {
			body, _ = io.ReadAll(c.Request.Body)
			c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
		}

		// 2. 包装 ResponseWriter 捕获响应体
		capture := &bytes.Buffer{}
		c.Writer = &bodyCaptureWriter{ResponseWriter: c.Writer, buf: capture}

		c.Next()

		// 3. 仅记录 404
		if c.Writer.Status() != http.StatusNotFound {
			return
		}

		rec := notFoundLogRecord{
			Time:         time.Now().Format(time.RFC3339),
			Method:       c.Request.Method,
			Path:         c.Request.URL.Path,
			Query:        c.Request.URL.RawQuery,
			URL:          c.Request.URL.String(),
			Host:         c.Request.Host,
			RemoteAddr:   c.Request.RemoteAddr,
			Headers:      c.Request.Header,
			Body:         string(body),
			Status:       c.Writer.Status(),
			ResponseBody: capture.String(),
		}
		// 非 UTF-8 内容用 base64 兜底，避免 JSON 乱码
		if !utf8.Valid(body) {
			rec.Body = base64.StdEncoding.EncodeToString(body)
			rec.BodyEncoding = "base64"
		}
		if !utf8.ValidString(rec.ResponseBody) {
			rec.ResponseBody = base64.StdEncoding.EncodeToString(capture.Bytes())
		}

		writeNotFoundLog(rec)
	}
}

// writeNotFoundLog 将记录以 JSONL 追加写入 logs/404.log，失败仅告警不影响请求
func writeNotFoundLog(rec notFoundLogRecord) {
	data, err := json.Marshal(rec)
	if err != nil {
		log.Printf("[404] 序列化失败: %v", err)
		return
	}
	data = append(data, '\n')

	notFoundLogMu.Lock()
	defer notFoundLogMu.Unlock()

	if notFoundLogFile == nil {
		if err := os.MkdirAll(filepath.Dir(notFoundLogPath), 0755); err != nil {
			log.Printf("[404] 创建日志目录失败: %v", err)
			return
		}
		f, err := os.OpenFile(notFoundLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Printf("[404] 打开日志文件失败: %v", err)
			return
		}
		notFoundLogFile = f
	}

	if _, err := notFoundLogFile.Write(data); err != nil {
		log.Printf("[404] 写入日志失败: %v", err)
	}

	log.Printf("[404] %s %s (Host: %s, Remote: %s)", rec.Method, rec.URL, rec.Host, rec.RemoteAddr)
}
