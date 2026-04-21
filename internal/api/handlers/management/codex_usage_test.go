package management

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	codexauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/codex"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

type stubCodexAuthRefresher struct {
	tokenData *codexauth.CodexTokenData
	err       error
}

func (s stubCodexAuthRefresher) RefreshTokensWithRetry(ctx context.Context, refreshToken string, maxRetries int) (*codexauth.CodexTokenData, error) {
	_ = ctx
	_ = refreshToken
	_ = maxRetries
	if s.err != nil {
		return nil, s.err
	}
	return s.tokenData, nil
}

func TestCodexUsageURL_UsesWhamForChatGPTBackend(t *testing.T) {
	got := codexUsageURL("https://chatgpt.com/backend-api/codex")
	want := "https://chatgpt.com/backend-api/wham/usage"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestCodexUsageURL_AppendsUsageForCustomEndpoint(t *testing.T) {
	got := codexUsageURL("https://example.com/api/codex")
	want := "https://example.com/api/codex/usage"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestListAuthFiles_IncludesCodexUsage(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	var gotAuthHeader string
	var gotAccountHeader string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")
		gotAccountHeader = r.Header.Get("ChatGPT-Account-Id")
		if r.URL.Path != "/backend-api/wham/usage" {
			t.Fatalf("expected usage path %q, got %q", "/backend-api/wham/usage", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"plan_type": "pro",
			"rate_limit": map[string]any{
				"allowed":       true,
				"limit_reached": false,
				"primary_window": map[string]any{
					"used_percent":         42,
					"limit_window_seconds": 300,
					"reset_after_seconds":  0,
					"reset_at":             1700000000,
				},
				"secondary_window": map[string]any{
					"used_percent":         84,
					"limit_window_seconds": 3600,
					"reset_after_seconds":  0,
					"reset_at":             1700003600,
				},
			},
			"credits": map[string]any{
				"has_credits": true,
				"unlimited":   false,
				"balance":     "13.2",
			},
			"additional_rate_limits": []map[string]any{
				{
					"limit_name":      "Research",
					"metered_feature": "codex_other",
					"rate_limit": map[string]any{
						"allowed":       true,
						"limit_reached": false,
						"primary_window": map[string]any{
							"used_percent":         12,
							"limit_window_seconds": 300,
							"reset_after_seconds":  0,
							"reset_at":             1700007200,
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	authDir := t.TempDir()
	fileName := "codex-user.json"
	filePath := filepath.Join(authDir, fileName)
	if err := os.WriteFile(filePath, []byte(`{"type":"codex"}`), 0o600); err != nil {
		t.Fatalf("failed to write auth file: %v", err)
	}

	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	record := &coreauth.Auth{
		ID:       fileName,
		FileName: fileName,
		Provider: "codex",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"path":     filePath,
			"base_url": srv.URL + "/backend-api/codex",
		},
		Metadata: map[string]any{
			"access_token": "access-token",
			"account_id":   "acct_123",
		},
	}
	if _, err := manager.Register(context.Background(), record); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/v0/management/auth-files?include_codex_usage=1", nil)
	ctx.Request = req
	h.ListAuthFiles(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if gotAuthHeader != "Bearer access-token" {
		t.Fatalf("expected authorization header to use access token, got %q", gotAuthHeader)
	}
	if gotAccountHeader != "acct_123" {
		t.Fatalf("expected ChatGPT-Account-Id header %q, got %q", "acct_123", gotAccountHeader)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	filesRaw, ok := payload["files"].([]any)
	if !ok || len(filesRaw) != 1 {
		t.Fatalf("expected 1 auth file entry, got %#v", payload["files"])
	}
	entry, ok := filesRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("expected auth entry object, got %#v", filesRaw[0])
	}
	if _, hasErr := entry["codex_usage_error"]; hasErr {
		t.Fatalf("expected no codex usage error, got %#v", entry["codex_usage_error"])
	}
	usage, ok := entry["codex_usage"].(map[string]any)
	if !ok {
		t.Fatalf("expected codex_usage object, got %#v", entry["codex_usage"])
	}
	if usage["plan_type"] != "pro" {
		t.Fatalf("expected plan_type %q, got %#v", "pro", usage["plan_type"])
	}
	credits, ok := usage["credits"].(map[string]any)
	if !ok {
		t.Fatalf("expected credits object, got %#v", usage["credits"])
	}
	if credits["balance"] != "13.2" {
		t.Fatalf("expected balance %q, got %#v", "13.2", credits["balance"])
	}
	rateLimit, ok := usage["rate_limit"].(map[string]any)
	if !ok {
		t.Fatalf("expected rate_limit object, got %#v", usage["rate_limit"])
	}
	primary, ok := rateLimit["primary_window"].(map[string]any)
	if !ok {
		t.Fatalf("expected primary_window object, got %#v", rateLimit["primary_window"])
	}
	if primary["used_percent"] != float64(42) {
		t.Fatalf("expected primary used_percent 42, got %#v", primary["used_percent"])
	}
	additional, ok := usage["additional_rate_limits"].([]any)
	if !ok || len(additional) != 1 {
		t.Fatalf("expected 1 additional rate limit, got %#v", usage["additional_rate_limits"])
	}
}

func TestListAuthFiles_DefaultSkipsCodexUsage(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"plan_type": "pro"})
	}))
	defer srv.Close()

	authDir := t.TempDir()
	fileName := "codex-user.json"
	filePath := filepath.Join(authDir, fileName)
	if err := os.WriteFile(filePath, []byte(`{"type":"codex"}`), 0o600); err != nil {
		t.Fatalf("failed to write auth file: %v", err)
	}

	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	record := &coreauth.Auth{
		ID:       fileName,
		FileName: fileName,
		Provider: "codex",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"path":     filePath,
			"base_url": srv.URL + "/backend-api/codex",
		},
		Metadata: map[string]any{
			"access_token": "access-token",
			"account_id":   "acct_123",
			"expired":      time.Now().Add(time.Hour).Format(time.RFC3339),
		},
	}
	if _, err := manager.Register(context.Background(), record); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)
	h.ListAuthFiles(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if requestCount != 0 {
		t.Fatalf("expected no usage requests by default, got %d", requestCount)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	filesRaw, ok := payload["files"].([]any)
	if !ok || len(filesRaw) != 1 {
		t.Fatalf("expected 1 auth file entry, got %#v", payload["files"])
	}
	entry, ok := filesRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("expected auth entry object, got %#v", filesRaw[0])
	}
	if _, hasUsage := entry["codex_usage"]; hasUsage {
		t.Fatalf("expected no codex_usage by default, got %#v", entry["codex_usage"])
	}
	if _, hasErr := entry["codex_usage_error"]; hasErr {
		t.Fatalf("expected no codex_usage_error by default, got %#v", entry["codex_usage_error"])
	}
}

func TestListAuthFiles_SkipsCodexUsageWhenTokenNeedsRefresh(t *testing.T) {
	originalFactory := newCodexAuthRefresher
	t.Cleanup(func() { newCodexAuthRefresher = originalFactory })
	newCodexAuthRefresher = func(cfg *config.Config) codexAuthRefresher {
		_ = cfg
		return stubCodexAuthRefresher{err: errors.New("should not refresh for auth-files")}
	}

	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"plan_type": "pro"})
	}))
	defer srv.Close()

	authDir := t.TempDir()
	fileName := "codex-expired.json"
	filePath := filepath.Join(authDir, fileName)
	if err := os.WriteFile(filePath, []byte(`{"type":"codex"}`), 0o600); err != nil {
		t.Fatalf("failed to write auth file: %v", err)
	}

	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	record := &coreauth.Auth{
		ID:       fileName,
		FileName: fileName,
		Provider: "codex",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"path":     filePath,
			"base_url": srv.URL + "/backend-api/codex",
		},
		Metadata: map[string]any{
			"access_token":  "expired-access-token",
			"refresh_token": "refresh-token",
			"expired":       time.Now().Add(-time.Minute).Format(time.RFC3339),
		},
	}
	if _, err := manager.Register(context.Background(), record); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files?include_codex_usage=1", nil)
	h.ListAuthFiles(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if requestCount != 0 {
		t.Fatalf("expected no usage requests, got %d", requestCount)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	filesRaw, ok := payload["files"].([]any)
	if !ok || len(filesRaw) != 1 {
		t.Fatalf("expected 1 auth file entry, got %#v", payload["files"])
	}
	entry, ok := filesRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("expected auth entry object, got %#v", filesRaw[0])
	}
	if _, hasUsage := entry["codex_usage"]; hasUsage {
		t.Fatalf("expected no codex_usage, got %#v", entry["codex_usage"])
	}
	if _, hasErr := entry["codex_usage_error"]; hasErr {
		t.Fatalf("expected no codex_usage_error, got %#v", entry["codex_usage_error"])
	}
}

func TestListAuthFiles_SkipsCodexUsageForDisabledAuth(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"plan_type": "pro"})
	}))
	defer srv.Close()

	authDir := t.TempDir()
	fileName := "codex-disabled.json"
	filePath := filepath.Join(authDir, fileName)
	if err := os.WriteFile(filePath, []byte(`{"type":"codex"}`), 0o600); err != nil {
		t.Fatalf("failed to write auth file: %v", err)
	}

	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	record := &coreauth.Auth{
		ID:       fileName,
		FileName: fileName,
		Provider: "codex",
		Status:   coreauth.StatusDisabled,
		Disabled: true,
		Attributes: map[string]string{
			"path":     filePath,
			"base_url": srv.URL + "/backend-api/codex",
		},
		Metadata: map[string]any{
			"access_token": "active-access-token",
			"expired":      time.Now().Add(time.Hour).Format(time.RFC3339),
		},
	}
	if _, err := manager.Register(context.Background(), record); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files?include_codex_usage=1", nil)
	h.ListAuthFiles(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if requestCount != 0 {
		t.Fatalf("expected no usage requests, got %d", requestCount)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	filesRaw, ok := payload["files"].([]any)
	if !ok || len(filesRaw) != 1 {
		t.Fatalf("expected 1 auth file entry, got %#v", payload["files"])
	}
	entry, ok := filesRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("expected auth entry object, got %#v", filesRaw[0])
	}
	if _, hasUsage := entry["codex_usage"]; hasUsage {
		t.Fatalf("expected no codex_usage, got %#v", entry["codex_usage"])
	}
	if _, hasErr := entry["codex_usage_error"]; hasErr {
		t.Fatalf("expected no codex_usage_error, got %#v", entry["codex_usage_error"])
	}
}

func TestResolveTokenForAuth_RefreshesExpiredCodexAccessToken(t *testing.T) {
	originalFactory := newCodexAuthRefresher
	t.Cleanup(func() { newCodexAuthRefresher = originalFactory })

	newCodexAuthRefresher = func(cfg *config.Config) codexAuthRefresher {
		_ = cfg
		return stubCodexAuthRefresher{
			tokenData: &codexauth.CodexTokenData{
				IDToken:      "new-id-token",
				AccessToken:  "new-access-token",
				RefreshToken: "new-refresh-token",
				AccountID:    "acct_new",
				Email:        "user@example.com",
				Expire:       time.Now().Add(time.Hour).Format(time.RFC3339),
			},
		}
	}

	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	auth := &coreauth.Auth{
		ID:       "codex-user.json",
		FileName: "codex-user.json",
		Provider: "codex",
		Status:   coreauth.StatusActive,
		Metadata: map[string]any{
			"access_token":  "expired-token",
			"refresh_token": "refresh-token",
			"expired":       time.Now().Add(-time.Minute).Format(time.RFC3339),
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, manager)
	token, err := h.resolveTokenForAuth(context.Background(), auth)
	if err != nil {
		t.Fatalf("resolve token: %v", err)
	}
	if token != "new-access-token" {
		t.Fatalf("expected refreshed access token, got %q", token)
	}
	if auth.Metadata["refresh_token"] != "new-refresh-token" {
		t.Fatalf("expected refreshed refresh token, got %#v", auth.Metadata["refresh_token"])
	}
	if auth.Metadata["account_id"] != "acct_new" {
		t.Fatalf("expected refreshed account id, got %#v", auth.Metadata["account_id"])
	}
}

func TestResolveTokenForAuth_KeepsValidCodexAccessToken(t *testing.T) {
	originalFactory := newCodexAuthRefresher
	t.Cleanup(func() { newCodexAuthRefresher = originalFactory })

	newCodexAuthRefresher = func(cfg *config.Config) codexAuthRefresher {
		_ = cfg
		return stubCodexAuthRefresher{err: errors.New("should not refresh")}
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	auth := &coreauth.Auth{
		Provider: "codex",
		Metadata: map[string]any{
			"access_token": "still-valid",
			"expired":      time.Now().Add(time.Hour).Format(time.RFC3339),
		},
	}
	token, err := h.resolveTokenForAuth(context.Background(), auth)
	if err != nil {
		t.Fatalf("resolve token: %v", err)
	}
	if token != "still-valid" {
		t.Fatalf("expected existing access token, got %q", token)
	}
}
