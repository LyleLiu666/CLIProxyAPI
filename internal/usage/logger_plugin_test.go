package usage

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestRequestStatisticsRecordIncludesLatency(t *testing.T) {
	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "test-key",
		Model:       "gpt-5.4",
		RequestedAt: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC),
		Latency:     1500 * time.Millisecond,
		Detail: coreusage.Detail{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	})

	snapshot := stats.Snapshot()
	details := snapshot.APIs["test-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
	if details[0].LatencyMs != 1500 {
		t.Fatalf("latency_ms = %d, want 1500", details[0].LatencyMs)
	}
}

func TestRequestStatisticsMergeSnapshotDedupIgnoresLatency(t *testing.T) {
	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	first := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"test-key": {
				Models: map[string]ModelSnapshot{
					"gpt-5.4": {
						Details: []RequestDetail{{
							Timestamp: timestamp,
							LatencyMs: 0,
							Source:    "user@example.com",
							AuthIndex: "0",
							Tokens: TokenStats{
								InputTokens:  10,
								OutputTokens: 20,
								TotalTokens:  30,
							},
						}},
					},
				},
			},
		},
	}
	second := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"test-key": {
				Models: map[string]ModelSnapshot{
					"gpt-5.4": {
						Details: []RequestDetail{{
							Timestamp: timestamp,
							LatencyMs: 2500,
							Source:    "user@example.com",
							AuthIndex: "0",
							Tokens: TokenStats{
								InputTokens:  10,
								OutputTokens: 20,
								TotalTokens:  30,
							},
						}},
					},
				},
			},
		},
	}

	result := stats.MergeSnapshot(first)
	if result.Added != 1 || result.Skipped != 0 {
		t.Fatalf("first merge = %+v, want added=1 skipped=0", result)
	}

	result = stats.MergeSnapshot(second)
	if result.Added != 0 || result.Skipped != 1 {
		t.Fatalf("second merge = %+v, want added=0 skipped=1", result)
	}

	snapshot := stats.Snapshot()
	details := snapshot.APIs["test-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
}

func TestRequestStatisticsSeparatesBucketsByRequestPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stats := NewRequestStatistics()

	ctxA, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctxA.Request = httptest.NewRequest("POST", "/team-a/v1/chat/completions", nil)
	ctxA.Set("gin", ctxA)

	ctxB, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctxB.Request = httptest.NewRequest("POST", "/team-b/v1/chat/completions", nil)
	ctxB.Set("gin", ctxB)

	stats.Record(context.WithValue(context.Background(), "gin", ctxA), coreusage.Record{
		APIKey: "shared-client-key",
		Model:  "gpt-5.4",
		Detail: coreusage.Detail{TotalTokens: 10},
	})
	stats.Record(context.WithValue(context.Background(), "gin", ctxB), coreusage.Record{
		APIKey: "shared-client-key",
		Model:  "gpt-5.4",
		Detail: coreusage.Detail{TotalTokens: 20},
	})

	snapshot := stats.Snapshot()
	if len(snapshot.APIs) != 2 {
		t.Fatalf("api bucket len = %d, want 2", len(snapshot.APIs))
	}

	for bucket, api := range snapshot.APIs {
		if api.TotalRequests != 1 {
			t.Fatalf("bucket %q total requests = %d, want 1", bucket, api.TotalRequests)
		}
	}
}
