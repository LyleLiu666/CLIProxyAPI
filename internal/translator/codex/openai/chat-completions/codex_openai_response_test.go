package chat_completions

import (
	"context"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertCodexResponseToOpenAINonStream_UsesSSETranscriptDeltas(t *testing.T) {
	raw := []byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_123\",\"created_at\":1776774745,\"model\":\"gpt-5.4-mini-2026-03-17\"}}\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"pong\"}\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_123\",\"created_at\":1776774745,\"model\":\"gpt-5.4-mini-2026-03-17\",\"status\":\"completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":18,\"total_tokens\":28,\"input_tokens_details\":{\"cached_tokens\":0},\"output_tokens_details\":{\"reasoning_tokens\":11}}}}\n")

	out := ConvertCodexResponseToOpenAINonStream(context.Background(), "", nil, nil, raw, nil)
	if out == "" {
		t.Fatal("expected non-empty output")
	}

	if got := gjson.Get(out, "choices.0.message.content").String(); got != "pong" {
		t.Fatalf("choices.0.message.content = %q, want %q", got, "pong")
	}
	if got := gjson.Get(out, "choices.0.finish_reason").String(); got != "stop" {
		t.Fatalf("choices.0.finish_reason = %q, want %q", got, "stop")
	}
	if got := gjson.Get(out, "usage.prompt_tokens").Int(); got != 10 {
		t.Fatalf("usage.prompt_tokens = %d, want %d", got, 10)
	}
	if got := gjson.Get(out, "usage.completion_tokens").Int(); got != 18 {
		t.Fatalf("usage.completion_tokens = %d, want %d", got, 18)
	}
}

func TestConvertCodexResponseToOpenAINonStream_UsesJSONTranscriptDeltas(t *testing.T) {
	raw := []byte("{\"type\":\"response.created\",\"response\":{\"id\":\"resp_456\",\"created_at\":1776774746,\"model\":\"gpt-5.4-mini-2026-03-17\"}}\n" +
		"{\"type\":\"response.output_text.delta\",\"delta\":\"pong\"}\n" +
		"{\"type\":\"response.completed\",\"response\":{\"id\":\"resp_456\",\"created_at\":1776774746,\"model\":\"gpt-5.4-mini-2026-03-17\",\"status\":\"completed\"}}\n")

	out := ConvertCodexResponseToOpenAINonStream(context.Background(), "", nil, nil, raw, nil)
	if out == "" {
		t.Fatal("expected non-empty output")
	}

	if got := gjson.Get(out, "choices.0.message.content").String(); got != "pong" {
		t.Fatalf("choices.0.message.content = %q, want %q", got, "pong")
	}
	if got := gjson.Get(out, "id").String(); got != "resp_456" {
		t.Fatalf("id = %q, want %q", got, "resp_456")
	}
}
