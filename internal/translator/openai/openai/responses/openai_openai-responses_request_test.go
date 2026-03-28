package responses

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAIResponsesRequestToOpenAIChatCompletions_StreamRequestsIncludeUsage(t *testing.T) {
	in := []byte(`{"model":"gpt-4.1","input":"hello"}`)

	out := ConvertOpenAIResponsesRequestToOpenAIChatCompletions("gpt-4.1", in, true)

	if !gjson.GetBytes(out, "stream").Bool() {
		t.Fatalf("stream = %v, want true", gjson.GetBytes(out, "stream").Bool())
	}
	if !gjson.GetBytes(out, "stream_options.include_usage").Bool() {
		t.Fatalf("stream_options.include_usage = %v, want true", gjson.GetBytes(out, "stream_options.include_usage").Bool())
	}
}

func TestConvertOpenAIResponsesRequestToOpenAIChatCompletions_NonStreamDoesNotSetIncludeUsage(t *testing.T) {
	in := []byte(`{"model":"gpt-4.1","input":"hello"}`)

	out := ConvertOpenAIResponsesRequestToOpenAIChatCompletions("gpt-4.1", in, false)

	if gjson.GetBytes(out, "stream_options.include_usage").Exists() {
		t.Fatalf("stream_options.include_usage should not exist for non-stream requests: %s", out)
	}
}
