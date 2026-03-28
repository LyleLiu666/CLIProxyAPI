package executor

import (
	"context"
	"fmt"
	"testing"
	"time"

	internalusage "github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestParseOpenAIUsageChatCompletions(t *testing.T) {
	data := []byte(`{"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":5}}}`)
	detail := parseOpenAIUsage(data)
	if detail.InputTokens != 1 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 1)
	}
	if detail.OutputTokens != 2 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 2)
	}
	if detail.TotalTokens != 3 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 3)
	}
	if detail.CachedTokens != 4 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 4)
	}
	if detail.ReasoningTokens != 5 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 5)
	}
}

func TestParseOpenAIUsageResponses(t *testing.T) {
	data := []byte(`{"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30,"input_tokens_details":{"cached_tokens":7},"output_tokens_details":{"reasoning_tokens":9}}}`)
	detail := parseOpenAIUsage(data)
	if detail.InputTokens != 10 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 10)
	}
	if detail.OutputTokens != 20 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 20)
	}
	if detail.TotalTokens != 30 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 30)
	}
	if detail.CachedTokens != 7 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 7)
	}
	if detail.ReasoningTokens != 9 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 9)
	}
}

func TestUsageReporterBuildRecordIncludesLatency(t *testing.T) {
	reporter := &usageReporter{
		provider:    "openai",
		model:       "gpt-5.4",
		requestedAt: time.Now().Add(-1500 * time.Millisecond),
	}

	record := reporter.buildRecord(usage.Detail{TotalTokens: 3}, false)
	if record.Latency < time.Second {
		t.Fatalf("latency = %v, want >= 1s", record.Latency)
	}
	if record.Latency > 3*time.Second {
		t.Fatalf("latency = %v, want <= 3s", record.Latency)
	}
}

func TestUsageReporterPublishZeroTokenSuccessStillPublishesRecord(t *testing.T) {
	wasEnabled := internalusage.StatisticsEnabled()
	internalusage.SetStatisticsEnabled(true)
	defer internalusage.SetStatisticsEnabled(wasEnabled)

	apiKey := fmt.Sprintf("zero-token-api-%d", time.Now().UnixNano())
	model := fmt.Sprintf("zero-token-model-%d", time.Now().UnixNano())
	reporter := &usageReporter{
		provider:    "openai",
		model:       model,
		apiKey:      apiKey,
		requestedAt: time.Now(),
	}

	reporter.publish(context.Background(), usage.Detail{})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := internalusage.GetRequestStatistics().Snapshot()
		apiStats, ok := snapshot.APIs[apiKey]
		if ok {
			modelStats, ok := apiStats.Models[model]
			if ok && modelStats.TotalRequests == 1 {
				if modelStats.TotalTokens != 0 {
					t.Fatalf("total tokens = %d, want 0", modelStats.TotalTokens)
				}
				if len(modelStats.Details) != 1 {
					t.Fatalf("details len = %d, want 1", len(modelStats.Details))
				}
				if modelStats.Details[0].Failed {
					t.Fatal("expected successful zero-token request to remain successful")
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("expected zero-token successful request to be recorded")
}
