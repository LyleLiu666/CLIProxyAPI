package registry

import (
	"fmt"
	"strings"
	"testing"
)

func benchmarkRegistryWithModels(tb testing.TB, modelCount int) (*ModelRegistry, string, string) {
	tb.Helper()

	r := newTestModelRegistry()
	models := make([]*ModelInfo, 0, modelCount)
	for i := 0; i < modelCount; i++ {
		models = append(models, &ModelInfo{ID: fmt.Sprintf("model-%04d", i)})
	}
	r.RegisterClient("client-1", "OpenAI", models)
	return r, "client-1", fmt.Sprintf("model-%04d", modelCount-1)
}

func BenchmarkModelRegistryClientSupportsModel(b *testing.B) {
	const modelCount = 1024

	b.Run("indexed-exact-hit", func(b *testing.B) {
		r, clientID, modelID := benchmarkRegistryWithModels(b, modelCount)

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if !r.ClientSupportsModel(clientID, modelID) {
				b.Fatalf("expected indexed exact lookup to find %s", modelID)
			}
		}
	})

	b.Run("indexed-normalized-hit", func(b *testing.B) {
		r, clientID, modelID := benchmarkRegistryWithModels(b, modelCount)
		query := " " + strings.ToUpper(modelID) + " "

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if !r.ClientSupportsModel(clientID, query) {
				b.Fatalf("expected indexed normalized lookup to find %s", modelID)
			}
		}
	})

	b.Run("indexed-miss", func(b *testing.B) {
		r, clientID, _ := benchmarkRegistryWithModels(b, modelCount)

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if r.ClientSupportsModel(clientID, "missing-model") {
				b.Fatal("expected indexed miss to return false")
			}
		}
	})

	b.Run("fallback-normalized-hit", func(b *testing.B) {
		r, clientID, modelID := benchmarkRegistryWithModels(b, modelCount)
		r.clientModelSets = nil
		query := " " + strings.ToUpper(modelID) + " "

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if !r.ClientSupportsModel(clientID, query) {
				b.Fatalf("expected fallback normalized lookup to find %s", modelID)
			}
		}
	})
}
