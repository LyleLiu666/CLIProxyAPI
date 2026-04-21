package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetAuthFiles_DefaultRequestSkipsCodexUsage(t *testing.T) {
	var gotInclude string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotInclude = r.URL.Query().Get("include_codex_usage")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files": []map[string]any{},
		})
	}))
	defer srv.Close()

	client := NewClient(0, "")
	client.baseURL = srv.URL

	if _, err := client.GetAuthFiles(); err != nil {
		t.Fatalf("GetAuthFiles() error = %v", err)
	}
	if gotInclude != "" {
		t.Fatalf("expected default auth-files request to skip codex usage, got %q", gotInclude)
	}
}

func TestGetAuthFilesWithCodexUsage_OptsInExplicitly(t *testing.T) {
	var gotInclude string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotInclude = r.URL.Query().Get("include_codex_usage")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files": []map[string]any{},
		})
	}))
	defer srv.Close()

	client := NewClient(0, "")
	client.baseURL = srv.URL

	if _, err := client.GetAuthFilesWithCodexUsage(); err != nil {
		t.Fatalf("GetAuthFilesWithCodexUsage() error = %v", err)
	}
	if gotInclude != "1" {
		t.Fatalf("expected explicit auth-files request to include codex usage, got %q", gotInclude)
	}
}
