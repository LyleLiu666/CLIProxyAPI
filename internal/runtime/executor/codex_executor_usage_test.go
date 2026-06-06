package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

type codexUsageCapturePlugin struct {
	records chan coreusage.Record
}

func (p *codexUsageCapturePlugin) HandleUsage(_ context.Context, record coreusage.Record) {
	select {
	case p.records <- record:
	default:
	}
}

func newCodexUsageCapture(t *testing.T) *codexUsageCapturePlugin {
	t.Helper()

	plugin := &codexUsageCapturePlugin{records: make(chan coreusage.Record, 16)}
	coreusage.RegisterNamedPlugin(fmt.Sprintf("codex-usage-test-%d", time.Now().UnixNano()), plugin)
	return plugin
}

func waitForCodexUsageRecord(t *testing.T, plugin *codexUsageCapturePlugin, apiKey, model string) coreusage.Record {
	t.Helper()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case record := <-plugin.records:
			if record.APIKey == apiKey && record.Model == model {
				return record
			}
		case <-deadline:
			t.Fatalf("timed out waiting for usage record apiKey=%q model=%q", apiKey, model)
		}
	}
}

func TestCodexExecuteRecordsRequestWhenCompletedResponseHasNoUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	model := fmt.Sprintf("gpt-5-codex-no-usage-%d", time.Now().UnixNano())
	apiKey := fmt.Sprintf("codex-no-usage-key-%d", time.Now().UnixNano())
	usageCapture := newCodexUsageCapture(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("path = %q, want /responses", r.URL.Path)
		}
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"created_at\":1776774745,\"model\":%q}}\n", model)
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"pong\"}\n"))
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"created_at\":1776774745,\"model\":%q,\"status\":\"completed\"}}\n", model)
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Set("userApiKey", apiKey)
	ctx := context.WithValue(context.Background(), "gin", ginCtx)

	executor := NewCodexExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Provider: "codex",
		Attributes: map[string]string{
			"api_key":  "upstream-token",
			"base_url": server.URL,
		},
	}
	payload := []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"ping"}]}`, model))

	if _, err := executor.Execute(ctx, auth, cliproxyexecutor.Request{
		Model:   model,
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Stream:       false,
	}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	record := waitForCodexUsageRecord(t, usageCapture, apiKey, model)
	if record.Failed {
		t.Fatalf("expected successful usage record, got failure: %+v", record.Fail)
	}
	if record.Detail.TotalTokens != 0 {
		t.Fatalf("total tokens = %d, want 0", record.Detail.TotalTokens)
	}
}

func TestCodexExecuteStreamRecordsFailureWhenStreamClosesBeforeCompletion(t *testing.T) {
	gin.SetMode(gin.TestMode)

	model := fmt.Sprintf("gpt-5-codex-incomplete-%d", time.Now().UnixNano())
	apiKey := fmt.Sprintf("codex-incomplete-key-%d", time.Now().UnixNano())
	usageCapture := newCodexUsageCapture(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("path = %q, want /responses", r.URL.Path)
		}
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"created_at\":1776774745,\"model\":%q}}\n", model)
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n"))
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Set("userApiKey", apiKey)
	ctx := context.WithValue(context.Background(), "gin", ginCtx)

	executor := NewCodexExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Provider: "codex",
		Attributes: map[string]string{
			"api_key":  "upstream-token",
			"base_url": server.URL,
		},
	}
	payload := []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"ping"}]}`, model))

	stream, err := executor.ExecuteStream(ctx, auth, cliproxyexecutor.Request{
		Model:   model,
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Stream:       true,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	for range stream.Chunks {
	}

	record := waitForCodexUsageRecord(t, usageCapture, apiKey, model)
	if !record.Failed {
		t.Fatalf("expected incomplete Codex stream to be recorded as failure; record=%+v", record)
	}
}
