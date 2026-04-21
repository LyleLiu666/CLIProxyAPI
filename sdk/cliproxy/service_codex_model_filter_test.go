package cliproxy

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func TestRegisterModelsForAuth_CodexOAuthUsesPlanSpecificCatalog(t *testing.T) {
	service := &Service{
		cfg: &config.Config{},
	}
	auth := &coreauth.Auth{
		ID:       "codex-oauth-model-filter",
		Provider: "codex",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"auth_kind": "oauth",
		},
		Metadata: map[string]any{
			"email":    "codex-oauth@example.com",
			"id_token": fakeCodexIDToken(t, "plus"),
		},
	}

	reg := registry.GetGlobalRegistry()
	reg.UnregisterClient(auth.ID)
	t.Cleanup(func() {
		reg.UnregisterClient(auth.ID)
	})

	service.registerModelsForAuth(auth)

	models := reg.GetModelsForClient(auth.ID)
	if len(models) == 0 {
		t.Fatal("expected codex oauth auth to register models")
	}

	ids := collectRegisteredModelIDs(models)

	for _, blocked := range []string{
		"gpt-5",
		"gpt-5-codex",
		"gpt-5-codex-mini",
		"gpt-5.1",
		"gpt-5.1-codex",
		"gpt-5.1-codex-mini",
		"gpt-5.1-codex-max",
		"gpt-5.2-codex",
	} {
		if _, ok := ids[blocked]; ok {
			t.Fatalf("expected ChatGPT-backed codex auth to hide unsupported model %q", blocked)
		}
	}

	for _, allowed := range []string{"gpt-5.2", "gpt-5.3-codex", "gpt-5.3-codex-spark", "gpt-5.4", "gpt-5.4-mini"} {
		if _, ok := ids[allowed]; !ok {
			t.Fatalf("expected ChatGPT-backed codex auth to keep model %q", allowed)
		}
	}
}

func TestRegisterModelsForAuth_CodexOAuthTeamHidesSpark(t *testing.T) {
	service := &Service{
		cfg: &config.Config{},
	}
	auth := &coreauth.Auth{
		ID:       "codex-oauth-team-model-filter",
		Provider: "codex",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"auth_kind": "oauth",
		},
		Metadata: map[string]any{
			"email":    "codex-team@example.com",
			"id_token": fakeCodexIDToken(t, "team"),
		},
	}

	reg := registry.GetGlobalRegistry()
	reg.UnregisterClient(auth.ID)
	t.Cleanup(func() {
		reg.UnregisterClient(auth.ID)
	})

	service.registerModelsForAuth(auth)

	ids := collectRegisteredModelIDs(reg.GetModelsForClient(auth.ID))
	if _, ok := ids["gpt-5.3-codex"]; !ok {
		t.Fatal("expected team plan to keep gpt-5.3-codex")
	}
	if _, ok := ids["gpt-5.3-codex-spark"]; ok {
		t.Fatal("expected team plan to hide gpt-5.3-codex-spark")
	}
}

func TestRegisterModelsForAuth_CodexAPIKeyKeepsFullOpenAICatalog(t *testing.T) {
	service := &Service{
		cfg: &config.Config{},
	}
	auth := &coreauth.Auth{
		ID:       "codex-apikey-model-filter",
		Provider: "codex",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"auth_kind": "apikey",
			"api_key":   "sk-codex-test",
		},
	}

	reg := registry.GetGlobalRegistry()
	reg.UnregisterClient(auth.ID)
	t.Cleanup(func() {
		reg.UnregisterClient(auth.ID)
	})

	service.registerModelsForAuth(auth)

	models := reg.GetModelsForClient(auth.ID)
	if len(models) == 0 {
		t.Fatal("expected codex api key auth to register models")
	}

	ids := collectRegisteredModelIDs(models)
	for _, expected := range []string{"gpt-5-codex", "gpt-5.1-codex-max", "gpt-5.4-mini"} {
		if _, ok := ids[expected]; !ok {
			t.Fatalf("expected codex api key auth to keep model %q", expected)
		}
	}
}

func fakeCodexIDToken(t *testing.T, planType string) string {
	t.Helper()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payloadRaw, err := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_plan_type":  planType,
			"chatgpt_account_id": "acct-chatgpt",
		},
	})
	if err != nil {
		t.Fatalf("marshal fake codex jwt payload: %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadRaw)
	return header + "." + payload + ".signature"
}

func collectRegisteredModelIDs(models []*registry.ModelInfo) map[string]struct{} {
	ids := make(map[string]struct{}, len(models))
	for _, model := range models {
		if model == nil {
			continue
		}
		id := strings.ToLower(strings.TrimSpace(model.ID))
		if id == "" {
			continue
		}
		ids[id] = struct{}{}
	}
	return ids
}
