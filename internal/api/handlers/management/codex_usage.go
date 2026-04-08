package management

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	codexauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/codex"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

const (
	defaultCodexBaseURL      = "https://chatgpt.com/backend-api/codex"
	codexUsageRequestTimeout = 5 * time.Second
	managementCodexUserAgent = "codex_cli_rs/0.101.0"
)

type codexAuthRefresher interface {
	RefreshTokensWithRetry(ctx context.Context, refreshToken string, maxRetries int) (*codexauth.CodexTokenData, error)
}

var newCodexAuthRefresher = func(cfg *config.Config) codexAuthRefresher {
	return codexauth.NewCodexAuth(cfg)
}

func codexUsageURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultCodexBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if idx := strings.Index(strings.ToLower(baseURL), "/backend-api"); idx >= 0 {
		prefix := baseURL[:idx+len("/backend-api")]
		return prefix + "/wham/usage"
	}
	return baseURL + "/usage"
}

func shouldFetchCodexUsage(auth *coreauth.Auth) bool {
	if auth == nil || !strings.EqualFold(strings.TrimSpace(auth.Provider), "codex") {
		return false
	}
	if auth.Attributes != nil && strings.TrimSpace(auth.Attributes["api_key"]) != "" {
		return false
	}
	if auth.Metadata == nil {
		return false
	}
	return codexAccessTokenForAuth(auth) != ""
}

func (h *Handler) enrichAuthFileEntry(ctx context.Context, auth *coreauth.Auth, entry gin.H) {
	if entry == nil || !shouldFetchCodexUsage(auth) {
		return
	}

	usage, err := h.fetchCodexUsage(ctx, auth)
	if err != nil {
		entry["codex_usage_error"] = err.Error()
		log.WithError(err).Debugf("management auth files: failed to fetch codex usage for %s", auth.ID)
		return
	}
	if len(usage) > 0 {
		entry["codex_usage"] = usage
	}
}

func (h *Handler) fetchCodexUsage(ctx context.Context, auth *coreauth.Auth) (map[string]any, error) {
	if auth == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, codexUsageRequestTimeout)
	defer cancel()

	token, err := h.resolveTokenForAuth(ctx, auth)
	if err != nil {
		return nil, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("codex access token missing")
	}

	baseURL := strings.TrimSpace(authAttribute(auth, "base_url"))
	usageURL := codexUsageURL(baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, usageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", managementCodexUserAgent)

	accountID := strings.TrimSpace(stringValue(auth.Metadata, "account_id"))
	if accountID == "" {
		accountID = strings.TrimSpace(stringValue(auth.Metadata, "chatgpt_account_id"))
	}
	if accountID != "" {
		req.Header.Set("ChatGPT-Account-Id", accountID)
	}

	httpClient := &http.Client{
		Timeout:   codexUsageRequestTimeout,
		Transport: h.apiCallTransport(auth),
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("response body close error: %v", errClose)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("codex usage request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode codex usage payload: %w", err)
	}
	return payload, nil
}

func codexAccessTokenForAuth(auth *coreauth.Auth) string {
	if auth == nil || auth.Metadata == nil {
		return ""
	}
	if v := strings.TrimSpace(stringValue(auth.Metadata, "access_token")); v != "" {
		return v
	}
	if raw, ok := auth.Metadata["token"].(map[string]any); ok && raw != nil {
		return strings.TrimSpace(stringValue(raw, "access_token"))
	}
	return ""
}

func codexTokenNeedsRefresh(metadata map[string]any) bool {
	const skew = 30 * time.Second

	if metadata == nil {
		return true
	}
	if expStr, ok := metadata["expired"].(string); ok {
		if ts, errParse := time.Parse(time.RFC3339, strings.TrimSpace(expStr)); errParse == nil {
			return !ts.After(time.Now().Add(skew))
		}
	}
	return true
}
