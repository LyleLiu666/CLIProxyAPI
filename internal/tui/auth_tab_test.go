package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFormatCodexWindow_ShowsRemainingPercent(t *testing.T) {
	window := map[string]any{
		"used_percent":         float64(42),
		"limit_window_seconds": float64(300),
		"reset_at":             float64(1700000000),
	}

	got := formatCodexWindow(window)
	if !strings.Contains(got, "58% left") {
		t.Fatalf("expected remaining percentage in %q", got)
	}
	if !strings.Contains(got, "5m") {
		t.Fatalf("expected window duration in %q", got)
	}
}

func TestAuthTabFetchFiles_DefaultSkipsCodexUsage(t *testing.T) {
	var gotPath string
	var gotInclude string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotInclude = r.URL.Query().Get("include_codex_usage")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files": []map[string]any{},
		})
	}))
	defer srv.Close()

	client := NewClient(0, "")
	client.baseURL = srv.URL

	model := newAuthTabModel(client)
	msg := model.fetchFiles()
	filesMsg, ok := msg.(authFilesMsg)
	if !ok {
		t.Fatalf("expected authFilesMsg, got %T", msg)
	}
	if filesMsg.err != nil {
		t.Fatalf("expected no fetch error, got %v", filesMsg.err)
	}
	if gotPath != "/v0/management/auth-files" {
		t.Fatalf("expected auth-files path, got %q", gotPath)
	}
	if gotInclude != "" {
		t.Fatalf("expected default auth tab fetch to skip codex usage, got %q", gotInclude)
	}
}

func TestAuthTabFetchFilesWithCodexUsage_RequestsCodexUsage(t *testing.T) {
	var gotPath string
	var gotInclude string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotInclude = r.URL.Query().Get("include_codex_usage")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files": []map[string]any{},
		})
	}))
	defer srv.Close()

	client := NewClient(0, "")
	client.baseURL = srv.URL

	model := newAuthTabModel(client)
	msg := model.fetchFilesWithCodexUsage()
	filesMsg, ok := msg.(authFilesMsg)
	if !ok {
		t.Fatalf("expected authFilesMsg, got %T", msg)
	}
	if filesMsg.err != nil {
		t.Fatalf("expected no fetch error, got %v", filesMsg.err)
	}
	if gotPath != "/v0/management/auth-files" {
		t.Fatalf("expected auth-files path, got %q", gotPath)
	}
	if gotInclude != "1" {
		t.Fatalf("expected include_codex_usage=1, got %q", gotInclude)
	}
}
