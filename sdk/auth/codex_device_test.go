package auth

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	internalcodex "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/codex"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestCodexBuildAuthRecord_PopulatesPlanTypeAttribute(t *testing.T) {
	authenticator := &CodexAuthenticator{}
	authSvc := internalcodex.NewCodexAuth(&internalconfig.Config{})

	record, err := authenticator.buildAuthRecord(authSvc, &internalcodex.CodexAuthBundle{
		TokenData: internalcodex.CodexTokenData{
			IDToken: fakeCodexIDTokenForAuthTest(t, "plus"),
			Email:   "codex@example.com",
		},
	})
	if err != nil {
		t.Fatalf("buildAuthRecord() error = %v", err)
	}

	if record.Attributes["plan_type"] != "plus" {
		t.Fatalf("expected plan_type %q, got %q", "plus", record.Attributes["plan_type"])
	}
}

func fakeCodexIDTokenForAuthTest(t *testing.T, planType string) string {
	t.Helper()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payloadRaw, err := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_plan_type":  planType,
			"chatgpt_account_id": "acct-test",
		},
	})
	if err != nil {
		t.Fatalf("marshal fake codex jwt payload: %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadRaw)
	return header + "." + payload + ".signature"
}
