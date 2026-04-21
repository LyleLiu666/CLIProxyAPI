// Package openai provides response translation functionality for Codex to OpenAI API compatibility.
// This package handles the conversion of Codex API responses into OpenAI Chat Completions-compatible
// JSON format, transforming streaming events and non-streaming responses into the format
// expected by OpenAI API clients. It supports both streaming and non-streaming modes,
// handling text content, tool calls, reasoning content, and usage metadata appropriately.
package chat_completions

import (
	"bytes"
	"context"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var (
	dataTag = []byte("data:")
)

// ConvertCliToOpenAIParams holds parameters for response conversion.
type ConvertCliToOpenAIParams struct {
	ResponseID                string
	CreatedAt                 int64
	Model                     string
	FunctionCallIndex         int
	HasReceivedArgumentsDelta bool
	HasToolCallAnnounced      bool
}

// ConvertCodexResponseToOpenAI translates a single chunk of a streaming response from the
// Codex API format to the OpenAI Chat Completions streaming format.
// It processes various Codex event types and transforms them into OpenAI-compatible JSON responses.
// The function handles text content, tool calls, reasoning content, and usage metadata, outputting
// responses that match the OpenAI API format. It supports incremental updates for streaming responses.
//
// Parameters:
//   - ctx: The context for the request, used for cancellation and timeout handling
//   - modelName: The name of the model being used for the response
//   - rawJSON: The raw JSON response from the Codex API
//   - param: A pointer to a parameter object for maintaining state between calls
//
// Returns:
//   - []string: A slice of strings, each containing an OpenAI-compatible JSON response
func ConvertCodexResponseToOpenAI(_ context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []string {
	if *param == nil {
		*param = &ConvertCliToOpenAIParams{
			Model:                     modelName,
			CreatedAt:                 0,
			ResponseID:                "",
			FunctionCallIndex:         -1,
			HasReceivedArgumentsDelta: false,
			HasToolCallAnnounced:      false,
		}
	}

	if !bytes.HasPrefix(rawJSON, dataTag) {
		return []string{}
	}
	rawJSON = bytes.TrimSpace(rawJSON[5:])

	// Initialize the OpenAI SSE template.
	template := `{"id":"","object":"chat.completion.chunk","created":12345,"model":"model","choices":[{"index":0,"delta":{"role":null,"content":null,"reasoning_content":null,"tool_calls":null},"finish_reason":null,"native_finish_reason":null}]}`

	rootResult := gjson.ParseBytes(rawJSON)

	typeResult := rootResult.Get("type")
	dataType := typeResult.String()
	if dataType == "response.created" {
		(*param).(*ConvertCliToOpenAIParams).ResponseID = rootResult.Get("response.id").String()
		(*param).(*ConvertCliToOpenAIParams).CreatedAt = rootResult.Get("response.created_at").Int()
		(*param).(*ConvertCliToOpenAIParams).Model = rootResult.Get("response.model").String()
		return []string{}
	}

	// Extract and set the model version.
	if modelResult := gjson.GetBytes(rawJSON, "model"); modelResult.Exists() {
		template, _ = sjson.Set(template, "model", modelResult.String())
	}

	template, _ = sjson.Set(template, "created", (*param).(*ConvertCliToOpenAIParams).CreatedAt)

	// Extract and set the response ID.
	template, _ = sjson.Set(template, "id", (*param).(*ConvertCliToOpenAIParams).ResponseID)

	// Extract and set usage metadata (token counts).
	if usageResult := gjson.GetBytes(rawJSON, "response.usage"); usageResult.Exists() {
		if outputTokensResult := usageResult.Get("output_tokens"); outputTokensResult.Exists() {
			template, _ = sjson.Set(template, "usage.completion_tokens", outputTokensResult.Int())
		}
		if totalTokensResult := usageResult.Get("total_tokens"); totalTokensResult.Exists() {
			template, _ = sjson.Set(template, "usage.total_tokens", totalTokensResult.Int())
		}
		if inputTokensResult := usageResult.Get("input_tokens"); inputTokensResult.Exists() {
			template, _ = sjson.Set(template, "usage.prompt_tokens", inputTokensResult.Int())
		}
		if cachedTokensResult := usageResult.Get("input_tokens_details.cached_tokens"); cachedTokensResult.Exists() {
			template, _ = sjson.Set(template, "usage.prompt_tokens_details.cached_tokens", cachedTokensResult.Int())
		}
		if reasoningTokensResult := usageResult.Get("output_tokens_details.reasoning_tokens"); reasoningTokensResult.Exists() {
			template, _ = sjson.Set(template, "usage.completion_tokens_details.reasoning_tokens", reasoningTokensResult.Int())
		}
	}

	if dataType == "response.reasoning_summary_text.delta" {
		if deltaResult := rootResult.Get("delta"); deltaResult.Exists() {
			template, _ = sjson.Set(template, "choices.0.delta.role", "assistant")
			template, _ = sjson.Set(template, "choices.0.delta.reasoning_content", deltaResult.String())
		}
	} else if dataType == "response.reasoning_summary_text.done" {
		template, _ = sjson.Set(template, "choices.0.delta.role", "assistant")
		template, _ = sjson.Set(template, "choices.0.delta.reasoning_content", "\n\n")
	} else if dataType == "response.output_text.delta" {
		if deltaResult := rootResult.Get("delta"); deltaResult.Exists() {
			template, _ = sjson.Set(template, "choices.0.delta.role", "assistant")
			template, _ = sjson.Set(template, "choices.0.delta.content", deltaResult.String())
		}
	} else if dataType == "response.completed" {
		finishReason := "stop"
		if (*param).(*ConvertCliToOpenAIParams).FunctionCallIndex != -1 {
			finishReason = "tool_calls"
		}
		template, _ = sjson.Set(template, "choices.0.finish_reason", finishReason)
		template, _ = sjson.Set(template, "choices.0.native_finish_reason", finishReason)
	} else if dataType == "response.output_item.added" {
		itemResult := rootResult.Get("item")
		if !itemResult.Exists() || itemResult.Get("type").String() != "function_call" {
			return []string{}
		}

		// Increment index for this new function call item.
		(*param).(*ConvertCliToOpenAIParams).FunctionCallIndex++
		(*param).(*ConvertCliToOpenAIParams).HasReceivedArgumentsDelta = false
		(*param).(*ConvertCliToOpenAIParams).HasToolCallAnnounced = true

		functionCallItemTemplate := `{"index":0,"id":"","type":"function","function":{"name":"","arguments":""}}`
		functionCallItemTemplate, _ = sjson.Set(functionCallItemTemplate, "index", (*param).(*ConvertCliToOpenAIParams).FunctionCallIndex)
		functionCallItemTemplate, _ = sjson.Set(functionCallItemTemplate, "id", itemResult.Get("call_id").String())

		// Restore original tool name if it was shortened.
		name := itemResult.Get("name").String()
		rev := buildReverseMapFromOriginalOpenAI(originalRequestRawJSON)
		if orig, ok := rev[name]; ok {
			name = orig
		}
		functionCallItemTemplate, _ = sjson.Set(functionCallItemTemplate, "function.name", name)
		functionCallItemTemplate, _ = sjson.Set(functionCallItemTemplate, "function.arguments", "")

		template, _ = sjson.Set(template, "choices.0.delta.role", "assistant")
		template, _ = sjson.SetRaw(template, "choices.0.delta.tool_calls", `[]`)
		template, _ = sjson.SetRaw(template, "choices.0.delta.tool_calls.-1", functionCallItemTemplate)

	} else if dataType == "response.function_call_arguments.delta" {
		(*param).(*ConvertCliToOpenAIParams).HasReceivedArgumentsDelta = true

		deltaValue := rootResult.Get("delta").String()
		functionCallItemTemplate := `{"index":0,"function":{"arguments":""}}`
		functionCallItemTemplate, _ = sjson.Set(functionCallItemTemplate, "index", (*param).(*ConvertCliToOpenAIParams).FunctionCallIndex)
		functionCallItemTemplate, _ = sjson.Set(functionCallItemTemplate, "function.arguments", deltaValue)

		template, _ = sjson.SetRaw(template, "choices.0.delta.tool_calls", `[]`)
		template, _ = sjson.SetRaw(template, "choices.0.delta.tool_calls.-1", functionCallItemTemplate)

	} else if dataType == "response.function_call_arguments.done" {
		if (*param).(*ConvertCliToOpenAIParams).HasReceivedArgumentsDelta {
			// Arguments were already streamed via delta events; nothing to emit.
			return []string{}
		}

		// Fallback: no delta events were received, emit the full arguments as a single chunk.
		fullArgs := rootResult.Get("arguments").String()
		functionCallItemTemplate := `{"index":0,"function":{"arguments":""}}`
		functionCallItemTemplate, _ = sjson.Set(functionCallItemTemplate, "index", (*param).(*ConvertCliToOpenAIParams).FunctionCallIndex)
		functionCallItemTemplate, _ = sjson.Set(functionCallItemTemplate, "function.arguments", fullArgs)

		template, _ = sjson.SetRaw(template, "choices.0.delta.tool_calls", `[]`)
		template, _ = sjson.SetRaw(template, "choices.0.delta.tool_calls.-1", functionCallItemTemplate)

	} else if dataType == "response.output_item.done" {
		itemResult := rootResult.Get("item")
		if !itemResult.Exists() || itemResult.Get("type").String() != "function_call" {
			return []string{}
		}

		if (*param).(*ConvertCliToOpenAIParams).HasToolCallAnnounced {
			// Tool call was already announced via output_item.added; skip emission.
			(*param).(*ConvertCliToOpenAIParams).HasToolCallAnnounced = false
			return []string{}
		}

		// Fallback path: model skipped output_item.added, so emit complete tool call now.
		(*param).(*ConvertCliToOpenAIParams).FunctionCallIndex++

		functionCallItemTemplate := `{"index":0,"id":"","type":"function","function":{"name":"","arguments":""}}`
		functionCallItemTemplate, _ = sjson.Set(functionCallItemTemplate, "index", (*param).(*ConvertCliToOpenAIParams).FunctionCallIndex)

		template, _ = sjson.SetRaw(template, "choices.0.delta.tool_calls", `[]`)
		functionCallItemTemplate, _ = sjson.Set(functionCallItemTemplate, "id", itemResult.Get("call_id").String())

		// Restore original tool name if it was shortened.
		name := itemResult.Get("name").String()
		rev := buildReverseMapFromOriginalOpenAI(originalRequestRawJSON)
		if orig, ok := rev[name]; ok {
			name = orig
		}
		functionCallItemTemplate, _ = sjson.Set(functionCallItemTemplate, "function.name", name)

		functionCallItemTemplate, _ = sjson.Set(functionCallItemTemplate, "function.arguments", itemResult.Get("arguments").String())
		template, _ = sjson.Set(template, "choices.0.delta.role", "assistant")
		template, _ = sjson.SetRaw(template, "choices.0.delta.tool_calls.-1", functionCallItemTemplate)

	} else {
		return []string{}
	}

	return []string{template}
}

// ConvertCodexResponseToOpenAINonStream converts a non-streaming Codex response to a non-streaming OpenAI response.
// This function processes the complete Codex response and transforms it into a single OpenAI-compatible
// JSON response. It handles message content, tool calls, reasoning content, and usage metadata, combining all
// the information into a single response that matches the OpenAI API format.
//
// Parameters:
//   - ctx: The context for the request, used for cancellation and timeout handling
//   - modelName: The name of the model being used for the response (unused in current implementation)
//   - rawJSON: The raw JSON response from the Codex API
//   - param: A pointer to a parameter object for the conversion (unused in current implementation)
//
// Returns:
//   - string: An OpenAI-compatible JSON response containing all message content and metadata
func ConvertCodexResponseToOpenAINonStream(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) string {
	responseResult, contentText, reasoningText, toolCalls, ok := parseCodexNonStreamTranscript(rawJSON, originalRequestRawJSON)
	if !ok {
		return ""
	}

	unixTimestamp := time.Now().Unix()

	template := `{"id":"","object":"chat.completion","created":123456,"model":"model","choices":[{"index":0,"message":{"role":"assistant","content":null,"reasoning_content":null,"tool_calls":null},"finish_reason":null,"native_finish_reason":null}]}`

	// Extract and set the model version.
	if modelResult := responseResult.Get("model"); modelResult.Exists() {
		template, _ = sjson.Set(template, "model", modelResult.String())
	}

	// Extract and set the creation timestamp.
	if createdAtResult := responseResult.Get("created_at"); createdAtResult.Exists() {
		template, _ = sjson.Set(template, "created", createdAtResult.Int())
	} else {
		template, _ = sjson.Set(template, "created", unixTimestamp)
	}

	// Extract and set the response ID.
	if idResult := responseResult.Get("id"); idResult.Exists() {
		template, _ = sjson.Set(template, "id", idResult.String())
	}

	// Extract and set usage metadata (token counts).
	if usageResult := responseResult.Get("usage"); usageResult.Exists() {
		if outputTokensResult := usageResult.Get("output_tokens"); outputTokensResult.Exists() {
			template, _ = sjson.Set(template, "usage.completion_tokens", outputTokensResult.Int())
		}
		if totalTokensResult := usageResult.Get("total_tokens"); totalTokensResult.Exists() {
			template, _ = sjson.Set(template, "usage.total_tokens", totalTokensResult.Int())
		}
		if inputTokensResult := usageResult.Get("input_tokens"); inputTokensResult.Exists() {
			template, _ = sjson.Set(template, "usage.prompt_tokens", inputTokensResult.Int())
		}
		if cachedTokensResult := usageResult.Get("input_tokens_details.cached_tokens"); cachedTokensResult.Exists() {
			template, _ = sjson.Set(template, "usage.prompt_tokens_details.cached_tokens", cachedTokensResult.Int())
		}
		if reasoningTokensResult := usageResult.Get("output_tokens_details.reasoning_tokens"); reasoningTokensResult.Exists() {
			template, _ = sjson.Set(template, "usage.completion_tokens_details.reasoning_tokens", reasoningTokensResult.Int())
		}
	}

	// Set content and reasoning content if found.
	if contentText != "" {
		template, _ = sjson.Set(template, "choices.0.message.content", contentText)
		template, _ = sjson.Set(template, "choices.0.message.role", "assistant")
	}

	if reasoningText != "" {
		template, _ = sjson.Set(template, "choices.0.message.reasoning_content", reasoningText)
		template, _ = sjson.Set(template, "choices.0.message.role", "assistant")
	}

	// Add tool calls if any.
	if len(toolCalls) > 0 {
		template, _ = sjson.SetRaw(template, "choices.0.message.tool_calls", `[]`)
		for _, toolCall := range toolCalls {
			template, _ = sjson.SetRaw(template, "choices.0.message.tool_calls.-1", toolCall)
		}
		template, _ = sjson.Set(template, "choices.0.message.role", "assistant")
	}

	// Extract and set the finish reason based on status
	if statusResult := responseResult.Get("status"); statusResult.Exists() {
		status := statusResult.String()
		if status == "completed" {
			template, _ = sjson.Set(template, "choices.0.finish_reason", "stop")
			template, _ = sjson.Set(template, "choices.0.native_finish_reason", "stop")
		}
	}

	return template
}

type codexToolCallState struct {
	Arguments strings.Builder
	CallID    string
	Name      string
}

func parseCodexNonStreamTranscript(rawJSON, originalRequestRawJSON []byte) (gjson.Result, string, string, []string, bool) {
	rev := buildReverseMapFromOriginalOpenAI(originalRequestRawJSON)
	var completedResponse gjson.Result
	var transcriptContent strings.Builder
	var transcriptReasoning strings.Builder
	var toolStates []*codexToolCallState

	lines := bytes.Split(rawJSON, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if bytes.HasPrefix(line, dataTag) {
			line = bytes.TrimSpace(line[len(dataTag):])
		}
		if len(line) == 0 || bytes.Equal(line, []byte("[DONE]")) || !gjson.ValidBytes(line) {
			continue
		}

		root := gjson.ParseBytes(line)
		switch root.Get("type").String() {
		case "response.output_text.delta":
			transcriptContent.WriteString(root.Get("delta").String())
		case "response.reasoning_summary_text.delta":
			transcriptReasoning.WriteString(root.Get("delta").String())
		case "response.output_item.added":
			item := root.Get("item")
			if item.Get("type").String() == "function_call" {
				toolStates = upsertCodexToolCall(toolStates, item, rev)
			}
		case "response.function_call_arguments.delta":
			if len(toolStates) == 0 {
				toolStates = append(toolStates, &codexToolCallState{})
			}
			toolStates[len(toolStates)-1].Arguments.WriteString(root.Get("delta").String())
		case "response.function_call_arguments.done":
			if len(toolStates) == 0 {
				toolStates = append(toolStates, &codexToolCallState{})
			}
			if toolStates[len(toolStates)-1].Arguments.Len() == 0 {
				toolStates[len(toolStates)-1].Arguments.WriteString(root.Get("arguments").String())
			}
		case "response.output_item.done":
			item := root.Get("item")
			if item.Get("type").String() == "function_call" {
				toolStates = upsertCodexToolCall(toolStates, item, rev)
			}
		case "response.completed":
			completedResponse = root.Get("response")
		}
	}

	if !completedResponse.Exists() {
		return gjson.Result{}, "", "", nil, false
	}

	contentText, reasoningText, toolCalls := extractCodexResponseOutput(completedResponse, rev)
	if contentText == "" {
		contentText = transcriptContent.String()
	}
	if reasoningText == "" {
		reasoningText = transcriptReasoning.String()
	}
	if len(toolCalls) == 0 {
		toolCalls = buildCodexToolCalls(toolStates)
	}

	return completedResponse, contentText, reasoningText, toolCalls, true
}

func extractCodexResponseOutput(responseResult gjson.Result, rev map[string]string) (string, string, []string) {
	outputResult := responseResult.Get("output")
	if !outputResult.IsArray() {
		return "", "", nil
	}

	var contentBuilder strings.Builder
	var reasoningBuilder strings.Builder
	var toolCalls []string

	outputResult.ForEach(func(_, outputItem gjson.Result) bool {
		switch outputItem.Get("type").String() {
		case "reasoning":
			if summary := outputItem.Get("summary"); summary.Exists() {
				appendCodexTextParts(&reasoningBuilder, summary)
			}
			if reasoningBuilder.Len() == 0 {
				appendCodexTextParts(&reasoningBuilder, outputItem.Get("content"))
			}
		case "message":
			appendCodexTextParts(&contentBuilder, outputItem.Get("content"))
		case "function_call":
			functionCallTemplate := `{"id":"","type":"function","function":{"name":"","arguments":""}}`
			if callID := outputItem.Get("call_id"); callID.Exists() {
				functionCallTemplate, _ = sjson.Set(functionCallTemplate, "id", callID.String())
			}
			if name := outputItem.Get("name").String(); name != "" {
				if original, ok := rev[name]; ok {
					name = original
				}
				functionCallTemplate, _ = sjson.Set(functionCallTemplate, "function.name", name)
			}
			if args := outputItem.Get("arguments"); args.Exists() {
				functionCallTemplate, _ = sjson.Set(functionCallTemplate, "function.arguments", args.String())
			}
			toolCalls = append(toolCalls, functionCallTemplate)
		}
		return true
	})

	return contentBuilder.String(), reasoningBuilder.String(), toolCalls
}

func appendCodexTextParts(builder *strings.Builder, parts gjson.Result) {
	if builder == nil || !parts.Exists() {
		return
	}
	if parts.IsArray() {
		parts.ForEach(func(_, part gjson.Result) bool {
			if text := extractCodexText(part); text != "" {
				builder.WriteString(text)
			}
			return true
		})
		return
	}
	if text := extractCodexText(parts); text != "" {
		builder.WriteString(text)
	}
}

func extractCodexText(part gjson.Result) string {
	if !part.Exists() {
		return ""
	}
	if text := part.Get("text"); text.Exists() {
		partType := part.Get("type").String()
		if partType == "" || partType == "output_text" || partType == "summary_text" || partType == "text" {
			return text.String()
		}
	}
	if part.Type == gjson.String {
		return part.String()
	}
	return ""
}

func upsertCodexToolCall(states []*codexToolCallState, item gjson.Result, rev map[string]string) []*codexToolCallState {
	callID := item.Get("call_id").String()
	name := item.Get("name").String()
	if original, ok := rev[name]; ok {
		name = original
	}

	for _, state := range states {
		if state != nil && state.CallID == callID && callID != "" {
			if state.Name == "" {
				state.Name = name
			}
			if state.Arguments.Len() == 0 {
				state.Arguments.WriteString(item.Get("arguments").String())
			}
			return states
		}
	}

	state := &codexToolCallState{CallID: callID, Name: name}
	state.Arguments.WriteString(item.Get("arguments").String())
	return append(states, state)
}

func buildCodexToolCalls(states []*codexToolCallState) []string {
	if len(states) == 0 {
		return nil
	}

	toolCalls := make([]string, 0, len(states))
	for _, state := range states {
		if state == nil {
			continue
		}
		functionCallTemplate := `{"id":"","type":"function","function":{"name":"","arguments":""}}`
		if state.CallID != "" {
			functionCallTemplate, _ = sjson.Set(functionCallTemplate, "id", state.CallID)
		}
		if state.Name != "" {
			functionCallTemplate, _ = sjson.Set(functionCallTemplate, "function.name", state.Name)
		}
		if args := state.Arguments.String(); args != "" {
			functionCallTemplate, _ = sjson.Set(functionCallTemplate, "function.arguments", args)
		}
		toolCalls = append(toolCalls, functionCallTemplate)
	}
	if len(toolCalls) == 0 {
		return nil
	}
	return toolCalls
}

// buildReverseMapFromOriginalOpenAI builds a map of shortened tool name -> original tool name
// from the original OpenAI-style request JSON using the same shortening logic.
func buildReverseMapFromOriginalOpenAI(original []byte) map[string]string {
	tools := gjson.GetBytes(original, "tools")
	rev := map[string]string{}
	if tools.IsArray() && len(tools.Array()) > 0 {
		var names []string
		arr := tools.Array()
		for i := 0; i < len(arr); i++ {
			t := arr[i]
			if t.Get("type").String() != "function" {
				continue
			}
			fn := t.Get("function")
			if !fn.Exists() {
				continue
			}
			if v := fn.Get("name"); v.Exists() {
				names = append(names, v.String())
			}
		}
		if len(names) > 0 {
			m := buildShortNameMap(names)
			for orig, short := range m {
				rev[short] = orig
			}
		}
	}
	return rev
}
