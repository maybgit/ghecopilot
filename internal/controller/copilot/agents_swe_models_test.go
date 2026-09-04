package copilot

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestAgentsSweModelsRoute verifies that GET /agents/swe/models returns the
// model list (same shape as /models) instead of 404.
func TestAgentsSweModelsRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 模型列表现在从上游 /v1/models 动态获取，用 mock 上游服务器
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"test-model-1"},{"id":"test-model-2"}],"object":"list"}`))
	}))
	defer upstream.Close()
	t.Setenv("UPSTREAM_API_BASE_URL", upstream.URL)
	t.Setenv("UPSTREAM_API_KEY", "test-key")
	t.Setenv("COPILOT_AUTO_MODEL", "test-model-1")

	// 测试从仓库根目录运行（部分处理函数依赖工作目录）
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(wd, "..", "..", "..")
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)

	r := gin.New()
	GinApi(r.Group("/"))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/agents/swe/models", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if len(body) == 0 {
		t.Fatal("expected non-empty body")
	}
	t.Logf("response: %s", body)
}
