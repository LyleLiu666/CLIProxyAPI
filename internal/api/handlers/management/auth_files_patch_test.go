package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

type recordingExecutor struct {
	provider string
}

func (e *recordingExecutor) Identifier() string {
	return e.provider
}

func (e *recordingExecutor) Execute(ctx context.Context, auth *coreauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	_ = ctx
	_ = auth
	_ = req
	_ = opts
	return cliproxyexecutor.Response{}, nil
}

func (e *recordingExecutor) ExecuteStream(ctx context.Context, auth *coreauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	_ = ctx
	_ = auth
	_ = req
	_ = opts
	return nil, nil
}

func (e *recordingExecutor) Refresh(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	_ = ctx
	return auth, nil
}

func (e *recordingExecutor) CountTokens(ctx context.Context, auth *coreauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	_ = ctx
	_ = auth
	_ = req
	_ = opts
	return cliproxyexecutor.Response{}, nil
}

func (e *recordingExecutor) HttpRequest(ctx context.Context, auth *coreauth.Auth, req *http.Request) (*http.Response, error) {
	_ = ctx
	_ = auth
	_ = req
	return nil, nil
}

func TestPatchAuthFileFields_UpdatesRuntimePriorityImmediately(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	authDir := t.TempDir()
	highPath := filepath.Join(authDir, "high.json")
	lowPath := filepath.Join(authDir, "low.json")
	if err := os.WriteFile(highPath, []byte(`{"type":"claude"}`), 0o600); err != nil {
		t.Fatalf("write high auth file: %v", err)
	}
	if err := os.WriteFile(lowPath, []byte(`{"type":"claude","priority":8}`), 0o600); err != nil {
		t.Fatalf("write low auth file: %v", err)
	}

	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	manager.RegisterExecutor(&recordingExecutor{provider: "claude"})

	high := &coreauth.Auth{
		ID:       "high.json",
		FileName: "high.json",
		Provider: "claude",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"path": highPath,
		},
		Metadata: map[string]any{
			"type": "claude",
		},
	}
	low := &coreauth.Auth{
		ID:       "low.json",
		FileName: "low.json",
		Provider: "claude",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"path":     lowPath,
			"priority": "8",
		},
		Metadata: map[string]any{
			"type":     "claude",
			"priority": 8,
		},
	}
	if _, err := manager.Register(context.Background(), high); err != nil {
		t.Fatalf("register high auth: %v", err)
	}
	if _, err := manager.Register(context.Background(), low); err != nil {
		t.Fatalf("register low auth: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPatch, "/v0/management/auth-files", strings.NewReader(`{"name":"high.json","priority":10}`))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	h.PatchAuthFileFields(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected patch status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	updated, ok := manager.GetByID("high.json")
	if !ok {
		t.Fatal("expected updated auth to exist")
	}
	if got := updated.Attributes["priority"]; got != "10" {
		t.Fatalf("expected runtime priority attribute %q, got %q", "10", got)
	}
	if got := updated.Metadata["priority"]; got != 10 {
		t.Fatalf("expected persisted priority %d, got %#v", 10, got)
	}

	var selectedAuthID string
	_, err := manager.Execute(
		context.Background(),
		[]string{"claude"},
		cliproxyexecutor.Request{},
		cliproxyexecutor.Options{
			Metadata: map[string]any{
				cliproxyexecutor.SelectedAuthCallbackMetadataKey: func(id string) {
					selectedAuthID = id
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("execute after patch: %v", err)
	}
	if selectedAuthID != "high.json" {
		t.Fatalf("expected patched higher-priority auth to be selected, got %q", selectedAuthID)
	}
}

func TestListAuthFiles_IncludesEditableFieldValues(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	authDir := t.TempDir()
	fileName := "editable.json"
	filePath := filepath.Join(authDir, fileName)
	if err := os.WriteFile(filePath, []byte(`{"type":"claude","priority":10}`), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	auth := &coreauth.Auth{
		ID:       fileName,
		FileName: fileName,
		Provider: "claude",
		Status:   coreauth.StatusActive,
		Prefix:   "team-a",
		ProxyURL: "http://127.0.0.1:8080",
		Attributes: map[string]string{
			"path":     filePath,
			"priority": "10",
		},
		Metadata: map[string]any{
			"type":     "claude",
			"priority": 10,
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)
	ctx.Request = req
	h.ListAuthFiles(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	files, ok := payload["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("expected a single auth file entry, got %#v", payload["files"])
	}
	entry, ok := files[0].(map[string]any)
	if !ok {
		t.Fatalf("expected auth file entry object, got %#v", files[0])
	}
	if got := entry["prefix"]; got != "team-a" {
		t.Fatalf("expected prefix %q, got %#v", "team-a", got)
	}
	if got := entry["proxy_url"]; got != "http://127.0.0.1:8080" {
		t.Fatalf("expected proxy_url %q, got %#v", "http://127.0.0.1:8080", got)
	}
	if got := entry["priority"]; got != float64(10) {
		t.Fatalf("expected priority %v, got %#v", float64(10), got)
	}
}
