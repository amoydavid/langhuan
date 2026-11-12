package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestCapabilitiesHandler 验证 C01 能力发现：稳定特性列表 + 版本策略可发现。
func TestCapabilitiesHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/capabilities", capabilities)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body struct {
		Service  string   `json:"service"`
		Features []string `json:"features"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Service != "langhuan" {
		t.Fatalf("service = %s", body.Service)
	}
	required := []string{
		FeatureSearchMultiKB,
		FeatureSearchDocumentIDsFilter,
		FeatureDocumentAIUseEnabled,
		FeatureSourceAnchorPDFV2,
		FeatureRevisionAssetBearer,
		FeatureDocumentRetry,
	}
	seen := make(map[string]bool, len(body.Features))
	for _, feature := range body.Features {
		seen[feature] = true
	}
	for _, feature := range required {
		if !seen[feature] {
			t.Fatalf("capabilities missing feature %s: %v", feature, body.Features)
		}
	}
}
