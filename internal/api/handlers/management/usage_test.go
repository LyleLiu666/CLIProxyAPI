package management

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestGetUsageStatisticsOmitsDetailsByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)

	stats := usage.NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey: "test-key",
		Model:  "gpt-5.4",
		Detail: coreusage.Detail{TotalTokens: 30},
	})

	h := &Handler{usageStats: stats}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/v0/management/usage", nil)

	h.GetUsageStatistics(ctx)

	var payload struct {
		Usage usage.StatisticsSnapshot `json:"usage"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got := len(payload.Usage.APIs["test-key"].Models["gpt-5.4"].Details); got != 0 {
		t.Fatalf("default details len = %d, want 0", got)
	}
}

func TestGetUsageStatisticsCanIncludeLimitedDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	stats := usage.NewRequestStatistics()
	for index := 0; index < 3; index++ {
		stats.Record(context.Background(), coreusage.Record{
			APIKey:      "test-key",
			Model:       "gpt-5.4",
			RequestedAt: time.Date(2026, 3, 20, 12, index, 0, 0, time.UTC),
			Detail:      coreusage.Detail{TotalTokens: int64((index + 1) * 10)},
		})
	}

	h := &Handler{usageStats: stats}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/v0/management/usage?include_details=1&details_limit=1", nil)

	h.GetUsageStatistics(ctx)

	var payload struct {
		Usage usage.StatisticsSnapshot `json:"usage"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	details := payload.Usage.APIs["test-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
	if got := details[0].Tokens.TotalTokens; got != 30 {
		t.Fatalf("latest detail total tokens = %d, want 30", got)
	}
}
