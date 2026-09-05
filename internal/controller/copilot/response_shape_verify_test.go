package copilot

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// TestModelsResponseShape verifies /models returns BOTH "data" (OpenAI) and
// "models" (CAPI) arrays, and that each model carries a serviceType so the
// instant-apply/code-mapper fetcher can .filter() without crashing.
func TestModelsResponseShape(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 重置包级缓存，避免受同包其它测试影响
	modelListCache = nil

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m-1"},{"id":"m-2"}],"object":"list"}`))
	}))
	defer upstream.Close()
	t.Setenv("UPSTREAM_API_BASE_URL", upstream.URL)
	t.Setenv("UPSTREAM_API_KEY", "k")
	t.Setenv("COPILOT_AUTO_MODEL", "m-1")

	wd, _ := os.Getwd()
	root := filepath.Join(wd, "..", "..", "..")
	_ = os.Chdir(root)
	defer os.Chdir(wd)

	r := gin.New()
	GinApi(r.Group("/"))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/models", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()

	if !gjson.Get(body, "data").IsArray() {
		t.Errorf("missing data array: %s", body)
	}
	if !gjson.Get(body, "models").IsArray() {
		t.Errorf("missing models array: %s", body)
	}
	if gjson.Get(body, "models.0.serviceType").String() != "InstantApplyChat" {
		t.Errorf("models[0].serviceType = %q, want InstantApplyChat", gjson.Get(body, "models.0.serviceType").String())
	}
	if gjson.Get(body, "models.0.id").String() != "m-1" {
		t.Errorf("models[0].id = %q, want m-1", gjson.Get(body, "models.0.id").String())
	}
}

// TestEmbeddingModelsResponseShape verifies /embeddings/models returns a
// "models" array so doGetAvailableTypes' `for..of r.models` does not throw.
func TestEmbeddingModelsResponseShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("EMBEDDING_MODEL", "bge-m3")

	r := gin.New()
	GinApi(r.Group("/"))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/embeddings/models", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !gjson.Get(body, "models").IsArray() {
		t.Errorf("missing models array: %s", body)
	}
	if gjson.Get(body, "models.0.id").String() != "bge-m3" {
		t.Errorf("models[0].id = %q, want bge-m3", gjson.Get(body, "models.0.id").String())
	}
	if !strings.Contains(body, `"active":true`) {
		t.Errorf("models[0] missing active:true: %s", body)
	}
}
