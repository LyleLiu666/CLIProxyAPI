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
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	internalusage "github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
)

func TestCodexExecuteRecordsRequestWhenCompletedResponseHasNoUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	wasEnabled := internalusage.StatisticsEnabled()
	internalusage.SetStatisticsEnabled(true)
	defer internalusage.SetStatisticsEnabled(wasEnabled)

	model := fmt.Sprintf("gpt-5-codex-no-usage-%d", time.Now().UnixNano())
	apiKey := fmt.Sprintf("codex-no-usage-key-%d", time.Now().UnixNano())

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
	ginCtx.Set("apiKey", apiKey)
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
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("expected successful Codex response without usage to be recorded")
}

func TestCodexExecuteStreamRecordsFailureWhenStreamClosesBeforeCompletion(t *testing.T) {
	gin.SetMode(gin.TestMode)

	wasEnabled := internalusage.StatisticsEnabled()
	internalusage.SetStatisticsEnabled(true)
	defer internalusage.SetStatisticsEnabled(wasEnabled)

	model := fmt.Sprintf("gpt-5-codex-incomplete-%d", time.Now().UnixNano())
	apiKey := fmt.Sprintf("codex-incomplete-key-%d", time.Now().UnixNano())

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
	ginCtx.Set("apiKey", apiKey)
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

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := internalusage.GetRequestStatistics().Snapshot()
		apiStats, ok := snapshot.APIs[apiKey]
		if ok {
			modelStats, ok := apiStats.Models[model]
			if ok && modelStats.TotalRequests == 1 {
				if snapshot.FailureCount == 0 || !modelStats.Details[0].Failed {
					t.Fatalf("expected incomplete Codex stream to be recorded as failure; snapshot=%+v detail=%+v", snapshot, modelStats.Details[0])
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("expected incomplete Codex stream to be recorded")
}
