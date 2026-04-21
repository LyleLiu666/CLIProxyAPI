package chat_completions

import (
	"context"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertCodexResponseToOpenAI_StreamSetsModelFromResponseCreated(t *testing.T) {
	ctx := context.Background()
	var param any

	modelName := "gpt-5.3-codex"

	out := ConvertCodexResponseToOpenAI(ctx, modelName, nil, nil, []byte(`data: {"type":"response.created","response":{"id":"resp_123","created_at":1700000000,"model":"gpt-5.3-codex"}}`), &param)
	if len(out) != 0 {
		t.Fatalf("expected no output for response.created, got %d chunks", len(out))
	}

	out = ConvertCodexResponseToOpenAI(ctx, modelName, nil, nil, []byte(`data: {"type":"response.output_text.delta","delta":"hello"}`), &param)
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}

	gotModel := gjson.GetBytes(out[0], "model").String()
	if gotModel != modelName {
		t.Fatalf("expected model %q, got %q", modelName, gotModel)
	}
}

func TestConvertCodexResponseToOpenAI_FirstChunkUsesRequestModelName(t *testing.T) {
	ctx := context.Background()
	var param any

	modelName := "gpt-5.3-codex"

	out := ConvertCodexResponseToOpenAI(ctx, modelName, nil, nil, []byte(`data: {"type":"response.output_text.delta","delta":"hello"}`), &param)
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}

	gotModel := gjson.GetBytes(out[0], "model").String()
	if gotModel != modelName {
		t.Fatalf("expected model %q, got %q", modelName, gotModel)
	}
}

func TestConvertCodexResponseToOpenAI_ToolCallChunkOmitsNullContentFields(t *testing.T) {
	ctx := context.Background()
	var param any

	out := ConvertCodexResponseToOpenAI(ctx, "gpt-5.4", nil, nil, []byte(`data: {"type":"response.output_item.added","item":{"type":"function_call","call_id":"call_123","name":"websearch"}}`), &param)
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}

	if gjson.GetBytes(out[0], "choices.0.delta.content").Exists() {
		t.Fatalf("expected content to be omitted, got %s", string(out[0]))
	}
	if gjson.GetBytes(out[0], "choices.0.delta.reasoning_content").Exists() {
		t.Fatalf("expected reasoning_content to be omitted, got %s", string(out[0]))
	}
	if !gjson.GetBytes(out[0], "choices.0.delta.tool_calls").Exists() {
		t.Fatalf("expected tool_calls to exist, got %s", string(out[0]))
	}
}

func TestConvertCodexResponseToOpenAI_ToolCallArgumentsDeltaOmitsNullContentFields(t *testing.T) {
	ctx := context.Background()
	var param any

	out := ConvertCodexResponseToOpenAI(ctx, "gpt-5.4", nil, nil, []byte(`data: {"type":"response.output_item.added","item":{"type":"function_call","call_id":"call_123","name":"websearch"}}`), &param)
	if len(out) != 1 {
		t.Fatalf("expected tool call announcement chunk, got %d", len(out))
	}

	out = ConvertCodexResponseToOpenAI(ctx, "gpt-5.4", nil, nil, []byte(`data: {"type":"response.function_call_arguments.delta","delta":"{\"query\":\"OpenAI\"}"}`), &param)
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}

	if gjson.GetBytes(out[0], "choices.0.delta.content").Exists() {
		t.Fatalf("expected content to be omitted, got %s", string(out[0]))
	}
	if gjson.GetBytes(out[0], "choices.0.delta.reasoning_content").Exists() {
		t.Fatalf("expected reasoning_content to be omitted, got %s", string(out[0]))
	}
	if !gjson.GetBytes(out[0], "choices.0.delta.tool_calls.0.function.arguments").Exists() {
		t.Fatalf("expected tool call arguments delta to exist, got %s", string(out[0]))
	}
}

func TestConvertCodexResponseToOpenAINonStream_UsesSSETranscriptDeltas(t *testing.T) {
	raw := []byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_123\",\"created_at\":1776774745,\"model\":\"gpt-5.4-mini-2026-03-17\"}}\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"pong\"}\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_123\",\"created_at\":1776774745,\"model\":\"gpt-5.4-mini-2026-03-17\",\"status\":\"completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":18,\"total_tokens\":28,\"input_tokens_details\":{\"cached_tokens\":0},\"output_tokens_details\":{\"reasoning_tokens\":11}}}}\n")

	out := ConvertCodexResponseToOpenAINonStream(context.Background(), "", nil, nil, raw, nil)
	if len(out) == 0 {
		t.Fatal("expected non-empty output")
	}

	if got := gjson.GetBytes(out, "choices.0.message.content").String(); got != "pong" {
		t.Fatalf("choices.0.message.content = %q, want %q", got, "pong")
	}
	if got := gjson.GetBytes(out, "choices.0.finish_reason").String(); got != "stop" {
		t.Fatalf("choices.0.finish_reason = %q, want %q", got, "stop")
	}
	if got := gjson.GetBytes(out, "usage.prompt_tokens").Int(); got != 10 {
		t.Fatalf("usage.prompt_tokens = %d, want %d", got, 10)
	}
	if got := gjson.GetBytes(out, "usage.completion_tokens").Int(); got != 18 {
		t.Fatalf("usage.completion_tokens = %d, want %d", got, 18)
	}
}

func TestConvertCodexResponseToOpenAINonStream_UsesJSONTranscriptDeltas(t *testing.T) {
	raw := []byte("{\"type\":\"response.created\",\"response\":{\"id\":\"resp_456\",\"created_at\":1776774746,\"model\":\"gpt-5.4-mini-2026-03-17\"}}\n" +
		"{\"type\":\"response.output_text.delta\",\"delta\":\"pong\"}\n" +
		"{\"type\":\"response.completed\",\"response\":{\"id\":\"resp_456\",\"created_at\":1776774746,\"model\":\"gpt-5.4-mini-2026-03-17\",\"status\":\"completed\"}}\n")

	out := ConvertCodexResponseToOpenAINonStream(context.Background(), "", nil, nil, raw, nil)
	if len(out) == 0 {
		t.Fatal("expected non-empty output")
	}

	if got := gjson.GetBytes(out, "choices.0.message.content").String(); got != "pong" {
		t.Fatalf("choices.0.message.content = %q, want %q", got, "pong")
	}
	if got := gjson.GetBytes(out, "id").String(); got != "resp_456" {
		t.Fatalf("id = %q, want %q", got, "resp_456")
	}
}
