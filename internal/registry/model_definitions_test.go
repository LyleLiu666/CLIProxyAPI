package registry

import "testing"

func TestWithXAIBuiltinsIncludesVideoPreviewModel(t *testing.T) {
	models := WithXAIBuiltins(nil)

	for _, model := range models {
		if model == nil {
			continue
		}
		if model.ID == xaiBuiltinVideo15PreviewModelID {
			return
		}
	}

	t.Fatalf("expected xAI builtin model %s", xaiBuiltinVideo15PreviewModelID)
}

func TestGetCodexAllModelsReturnsDeduplicatedPlanUnion(t *testing.T) {
	models := GetCodexAllModels()
	if len(models) == 0 {
		t.Fatal("expected codex all models")
	}

	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		if model == nil || model.ID == "" {
			t.Fatalf("expected populated model, got %#v", model)
		}
		if _, exists := seen[model.ID]; exists {
			t.Fatalf("expected de-duplicated models, got duplicate %q", model.ID)
		}
		seen[model.ID] = struct{}{}
	}

	if _, ok := seen[codexBuiltinImageModelID]; !ok {
		t.Fatalf("expected codex builtin model %s", codexBuiltinImageModelID)
	}
}
