package management

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/auth/codex"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestBuildCodexAuthRecord_PopulatesPlanType(t *testing.T) {
	authSvc := codex.NewCodexAuth(&config.Config{})

	record, err := buildCodexAuthRecord(authSvc, &codex.CodexAuthBundle{
		TokenData: codex.CodexTokenData{
			IDToken:   fakeCodexIDTokenForManagementTest(t, "plus"),
			AccountID: "acct-management",
			Email:     "codex@example.com",
		},
	})
	if err != nil {
		t.Fatalf("buildCodexAuthRecord() error = %v", err)
	}

	if got := record.Attributes["plan_type"]; got != "plus" {
		t.Fatalf("expected attributes plan_type %q, got %q", "plus", got)
	}
	if got, _ := record.Metadata["plan_type"].(string); got != "plus" {
		t.Fatalf("expected metadata plan_type %q, got %q", "plus", got)
	}
}

func TestAuthPriorityValue_TreatsZeroAsUnset(t *testing.T) {
	priority, ok := authPriorityValue(&coreauth.Auth{
		Attributes: map[string]string{
			"priority": "0",
		},
	})
	if ok {
		t.Fatalf("expected zero priority to be treated as unset, got %d", priority)
	}
}

func fakeCodexIDTokenForManagementTest(t *testing.T, planType string) string {
	t.Helper()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payloadRaw, err := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_plan_type":  planType,
			"chatgpt_account_id": "acct-management",
		},
	})
	if err != nil {
		t.Fatalf("marshal fake codex jwt payload: %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadRaw)
	return header + "." + payload + ".signature"
}
