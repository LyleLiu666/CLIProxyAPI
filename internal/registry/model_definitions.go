// Package registry provides model definitions and lookup helpers for various AI providers.
// Static model metadata is loaded from the embedded models.json file and can be refreshed from network.
package registry

import (
	"strings"
)

// staticModelsJSON mirrors the top-level structure of models.json.
type staticModelsJSON struct {
	Claude      []*ModelInfo `json:"claude"`
	Gemini      []*ModelInfo `json:"gemini"`
	Vertex      []*ModelInfo `json:"vertex"`
	GeminiCLI   []*ModelInfo `json:"gemini-cli"`
	AIStudio    []*ModelInfo `json:"aistudio"`
	CodexFree   []*ModelInfo `json:"codex-free"`
	CodexTeam   []*ModelInfo `json:"codex-team"`
	CodexPlus   []*ModelInfo `json:"codex-plus"`
	CodexPro    []*ModelInfo `json:"codex-pro"`
	Qwen        []*ModelInfo `json:"qwen"`
	IFlow       []*ModelInfo `json:"iflow"`
	Kimi        []*ModelInfo `json:"kimi"`
	Antigravity []*ModelInfo `json:"antigravity"`
}

// GetClaudeModels returns the standard Claude model definitions.
func GetClaudeModels() []*ModelInfo {
	return cloneModelInfos(getModels().Claude)
}

// GetGeminiModels returns the standard Gemini model definitions.
func GetGeminiModels() []*ModelInfo {
	return cloneModelInfos(getModels().Gemini)
}

// GetGeminiVertexModels returns Gemini model definitions for Vertex AI.
func GetGeminiVertexModels() []*ModelInfo {
	return cloneModelInfos(getModels().Vertex)
}

// GetGeminiCLIModels returns Gemini model definitions for the Gemini CLI.
func GetGeminiCLIModels() []*ModelInfo {
	return cloneModelInfos(getModels().GeminiCLI)
}

// GetAIStudioModels returns model definitions for AI Studio.
func GetAIStudioModels() []*ModelInfo {
	return cloneModelInfos(getModels().AIStudio)
}

// GetCodexFreeModels returns model definitions for the Codex free plan tier.
func GetCodexFreeModels() []*ModelInfo {
	return filterModelInfosByID(codexModelCatalog(),
		"gpt-5.2",
		"gpt-5.3-codex",
		"gpt-5.4",
		"gpt-5.4-mini",
	)
}

// GetCodexAllModels returns the combined Codex model catalog used for API key auths.
func GetCodexAllModels() []*ModelInfo {
	return codexModelCatalog()
}

// GetCodexTeamModels returns model definitions for the Codex team plan tier.
func GetCodexTeamModels() []*ModelInfo {
	return filterModelInfosByID(codexModelCatalog(),
		"gpt-5.2",
		"gpt-5.3-codex",
		"gpt-5.4",
		"gpt-5.4-mini",
	)
}

// GetCodexPlusModels returns model definitions for the Codex plus plan tier.
func GetCodexPlusModels() []*ModelInfo {
	return filterModelInfosByID(codexModelCatalog(),
		"gpt-5.2",
		"gpt-5.3-codex",
		"gpt-5.3-codex-spark",
		"gpt-5.4",
		"gpt-5.4-mini",
	)
}

// GetCodexProModels returns model definitions for the Codex pro plan tier.
func GetCodexProModels() []*ModelInfo {
	return filterModelInfosByID(codexModelCatalog(),
		"gpt-5.2",
		"gpt-5.3-codex",
		"gpt-5.3-codex-spark",
		"gpt-5.4",
		"gpt-5.4-mini",
	)
}

// GetQwenModels returns the standard Qwen model definitions.
func GetQwenModels() []*ModelInfo {
	return cloneModelInfos(getModels().Qwen)
}

// GetIFlowModels returns the standard iFlow model definitions.
func GetIFlowModels() []*ModelInfo {
	return cloneModelInfos(getModels().IFlow)
}

// GetKimiModels returns the standard Kimi (Moonshot AI) model definitions.
func GetKimiModels() []*ModelInfo {
	return cloneModelInfos(getModels().Kimi)
}

// GetAntigravityModels returns the standard Antigravity model definitions.
func GetAntigravityModels() []*ModelInfo {
	return cloneModelInfos(getModels().Antigravity)
}

// cloneModelInfos returns a shallow copy of the slice with each element deep-cloned.
func cloneModelInfos(models []*ModelInfo) []*ModelInfo {
	if len(models) == 0 {
		return nil
	}
	out := make([]*ModelInfo, len(models))
	for i, m := range models {
		out[i] = cloneModelInfo(m)
	}
	return out
}

func filterModelInfosByID(models []*ModelInfo, ids ...string) []*ModelInfo {
	if len(models) == 0 || len(ids) == 0 {
		return nil
	}

	allowed := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id != "" {
			allowed[id] = struct{}{}
		}
	}

	filtered := make([]*ModelInfo, 0, len(ids))
	for _, model := range models {
		if model == nil {
			continue
		}
		if _, ok := allowed[model.ID]; ok {
			filtered = append(filtered, cloneModelInfo(model))
		}
	}
	return filtered
}

func codexModelCatalog() []*ModelInfo {
	data := getModels()
	groups := [][]*ModelInfo{
		data.CodexFree,
		data.CodexTeam,
		data.CodexPlus,
		data.CodexPro,
	}

	seen := make(map[string]struct{})
	catalog := make([]*ModelInfo, 0)
	for _, group := range groups {
		for _, model := range group {
			if model == nil || model.ID == "" {
				continue
			}
			if _, ok := seen[model.ID]; ok {
				continue
			}
			seen[model.ID] = struct{}{}
			catalog = append(catalog, cloneModelInfo(model))
		}
	}

	if _, ok := seen["gpt-5.4-mini"]; !ok {
		catalog = append(catalog, &ModelInfo{
			ID:                  "gpt-5.4-mini",
			Object:              "model",
			Created:             1773705600,
			OwnedBy:             "openai",
			Type:                "openai",
			DisplayName:         "GPT 5.4 Mini",
			Version:             "gpt-5.4-mini",
			Description:         "GPT-5.4 mini brings the strengths of GPT-5.4 to a faster, more efficient model designed for high-volume workloads.",
			ContextLength:       400000,
			MaxCompletionTokens: 128000,
			SupportedParameters: []string{"tools"},
			Thinking:            &ThinkingSupport{Levels: []string{"low", "medium", "high", "xhigh"}},
		})
	}

	return catalog
}

// GetStaticModelDefinitionsByChannel returns static model definitions for a given channel/provider.
// It returns nil when the channel is unknown.
//
// Supported channels:
//   - claude
//   - gemini
//   - vertex
//   - gemini-cli
//   - aistudio
//   - codex
//   - qwen
//   - iflow
//   - kimi
//   - antigravity
func GetStaticModelDefinitionsByChannel(channel string) []*ModelInfo {
	key := strings.ToLower(strings.TrimSpace(channel))
	switch key {
	case "claude":
		return GetClaudeModels()
	case "gemini":
		return GetGeminiModels()
	case "vertex":
		return GetGeminiVertexModels()
	case "gemini-cli":
		return GetGeminiCLIModels()
	case "aistudio":
		return GetAIStudioModels()
	case "codex":
		return GetCodexProModels()
	case "qwen":
		return GetQwenModels()
	case "iflow":
		return GetIFlowModels()
	case "kimi":
		return GetKimiModels()
	case "antigravity":
		return GetAntigravityModels()
	default:
		return nil
	}
}

// LookupStaticModelInfo searches all static model definitions for a model by ID.
// Returns nil if no matching model is found.
func LookupStaticModelInfo(modelID string) *ModelInfo {
	if modelID == "" {
		return nil
	}

	data := getModels()
	allModels := [][]*ModelInfo{
		data.Claude,
		data.Gemini,
		data.Vertex,
		data.GeminiCLI,
		data.AIStudio,
		codexModelCatalog(),
		data.Qwen,
		data.IFlow,
		data.Kimi,
		data.Antigravity,
	}
	for _, models := range allModels {
		for _, m := range models {
			if m != nil && m.ID == modelID {
				return cloneModelInfo(m)
			}
		}
	}

	return nil
}
