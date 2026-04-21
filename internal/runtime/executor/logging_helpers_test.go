package executor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func newRequestLogContext() (*gin.Context, context.Context, *config.Config) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ctx := context.WithValue(context.Background(), "gin", ginCtx)
	cfg := &config.Config{SDKConfig: config.SDKConfig{RequestLog: true}}
	return ginCtx, ctx, cfg
}

func TestUpstreamLogAggregationAcrossAttempts(t *testing.T) {
	ginCtx, ctx, cfg := newRequestLogContext()

	recordAPIRequest(ctx, cfg, upstreamRequestLog{
		URL:      "https://example.com/one",
		Method:   http.MethodPost,
		Headers:  http.Header{"X-Test": {"one"}},
		Body:     []byte(`{"prompt":"hello"}`),
		Provider: "claude",
	})
	recordAPIResponseMetadata(ctx, cfg, http.StatusOK, http.Header{"Content-Type": {"application/json"}})
	appendAPIResponseChunk(ctx, cfg, []byte(`{"ok":1}`))
	appendAPIResponseChunk(ctx, cfg, []byte(`{"ok":2}`))

	recordAPIRequest(ctx, cfg, upstreamRequestLog{
		URL:      "https://example.com/two",
		Method:   http.MethodPost,
		Headers:  http.Header{"X-Test": {"two"}},
		Body:     []byte(`{"prompt":"retry"}`),
		Provider: "claude",
	})
	recordAPIResponseMetadata(ctx, cfg, http.StatusBadGateway, nil)
	recordAPIResponseError(ctx, cfg, errors.New("upstream boom"))

	rawRequest, exists := ginCtx.Get(apiRequestKey)
	if !exists {
		t.Fatal("expected aggregated API request to be stored in gin context")
	}
	requestBytes, ok := rawRequest.([]byte)
	if !ok {
		t.Fatalf("aggregated API request type = %T, want []byte", rawRequest)
	}
	requestText := string(requestBytes)
	if strings.Count(requestText, "=== API REQUEST 1 ===") != 1 {
		t.Fatalf("expected one first request section, got %q", requestText)
	}
	if strings.Count(requestText, "=== API REQUEST 2 ===") != 1 {
		t.Fatalf("expected one second request section, got %q", requestText)
	}
	if !strings.Contains(requestText, "https://example.com/one") || !strings.Contains(requestText, "https://example.com/two") {
		t.Fatalf("aggregated request missing attempt URLs: %q", requestText)
	}

	rawResponse, exists := ginCtx.Get(apiResponseKey)
	if !exists {
		t.Fatal("expected aggregated API response to be stored in gin context")
	}
	responseBytes, ok := rawResponse.([]byte)
	if !ok {
		t.Fatalf("aggregated API response type = %T, want []byte", rawResponse)
	}
	responseText := string(responseBytes)
	if strings.Count(responseText, "=== API RESPONSE 1 ===") != 1 {
		t.Fatalf("expected one first response section, got %q", responseText)
	}
	if strings.Count(responseText, "=== API RESPONSE 2 ===") != 1 {
		t.Fatalf("expected one second response section, got %q", responseText)
	}
	if strings.Count(responseText, `{"ok":1}`) != 1 || strings.Count(responseText, `{"ok":2}`) != 1 {
		t.Fatalf("aggregated response duplicated stream chunks: %q", responseText)
	}
	if !strings.Contains(responseText, "\n\n=== API RESPONSE 2 ===\n") {
		t.Fatalf("aggregated response missing blank-line separator between attempts: %q", responseText)
	}
	if !strings.Contains(responseText, "Error: upstream boom") {
		t.Fatalf("aggregated response missing upstream error: %q", responseText)
	}
}
