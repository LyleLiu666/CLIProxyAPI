package registry

import "testing"

func TestGetStaticModelDefinitionsByChannel_CodexUsesCurrentCatalog(t *testing.T) {
	models := GetStaticModelDefinitionsByChannel("codex")
	if len(models) == 0 {
		t.Fatal("expected codex static models")
	}

	ids := make(map[string]struct{}, len(models))
	for _, model := range models {
		if model == nil {
			continue
		}
		ids[model.ID] = struct{}{}
	}

	if _, ok := ids["gpt-5.4-mini"]; !ok {
		t.Fatal("expected codex static catalog to include gpt-5.4-mini")
	}
	if _, ok := ids["gpt-5.1-codex-max"]; ok {
		t.Fatal("expected codex static catalog to exclude legacy gpt-5.1-codex-max")
	}
}
