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

func TestParseOpenAIStreamUsageIgnoresNullUsage(t *testing.T) {
	line := []byte(`data: {"id":"chatcmpl-null","choices":[{"delta":{"content":"ok"},"index":0}],"usage":null}`)
	if detail, ok := parseOpenAIStreamUsage(line); ok {
		t.Fatalf("expected null usage to be ignored, got ok=%t detail=%+v", ok, detail)
	}
}

func TestParseOpenAIStreamUsageSupportsResponsesFields(t *testing.T) {
	line := []byte(`data: {"usage":{"input_tokens":320,"output_tokens":1,"input_tokens_details":{"cached_tokens":2},"output_tokens_details":{"reasoning_tokens":5}}}`)
	detail, ok := parseOpenAIStreamUsage(line)
	if !ok {
		t.Fatal("expected responses-style stream usage to be parsed")
	}
	if detail.InputTokens != 320 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 320)
	}
	if detail.OutputTokens != 1 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 1)
	}
	if detail.CachedTokens != 2 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 2)
	}
	if detail.ReasoningTokens != 5 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 5)
	}
}

func TestUsageReporterStreamIgnoresNullUsageUntilFinalUsage(t *testing.T) {
	wasEnabled := internalusage.StatisticsEnabled()
	internalusage.SetStatisticsEnabled(true)
	defer internalusage.SetStatisticsEnabled(wasEnabled)

	apiKey := fmt.Sprintf("stream-null-usage-api-%d", time.Now().UnixNano())
	model := fmt.Sprintf("stream-null-usage-model-%d", time.Now().UnixNano())
	reporter := &usageReporter{
		provider:    "qwen",
		model:       model,
		apiKey:      apiKey,
		requestedAt: time.Now(),
	}

	lines := [][]byte{
		[]byte(`data: {"id":"chatcmpl-null","choices":[{"delta":{"content":"o"},"index":0}],"usage":null}`),
		[]byte(`data: {"id":"chatcmpl-final","choices":[{"delta":{},"index":0,"finish_reason":"stop"}],"usage":{"prompt_tokens":13,"completion_tokens":1,"total_tokens":14}}`),
	}

	for _, line := range lines {
		if detail, ok := parseOpenAIStreamUsage(line); ok {
			reporter.publish(context.Background(), detail)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := internalusage.GetRequestStatistics().Snapshot()
		apiStats, ok := snapshot.APIs[apiKey]
		if !ok {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		modelStats, ok := apiStats.Models[model]
		if !ok {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if modelStats.TotalRequests != 1 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if modelStats.TotalTokens != 14 {
			t.Fatalf("total tokens = %d, want 14", modelStats.TotalTokens)
		}
		if len(modelStats.Details) != 1 {
			t.Fatalf("details len = %d, want 1", len(modelStats.Details))
		}
		if modelStats.Details[0].Tokens.TotalTokens != 14 {
			t.Fatalf("detail total tokens = %d, want 14", modelStats.Details[0].Tokens.TotalTokens)
		}
		return
	}

	t.Fatal("expected final stream usage to be recorded")
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
