package executor

import (
	"net/http"
	"testing"
)

func BenchmarkUpstreamLoggingAggregation(b *testing.B) {
	const chunkCount = 64
	chunk := []byte(`{"type":"response.delta","delta":"abcdefghijklmnopqrstuvwxyz0123456789"}`)

	b.Run("single-attempt", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, ctx, cfg := newRequestLogContext()
			recordAPIRequest(ctx, cfg, upstreamRequestLog{
				URL:      "https://example.com/v1/messages",
				Method:   http.MethodPost,
				Headers:  http.Header{"Content-Type": {"application/json"}},
				Body:     []byte(`{"stream":true}`),
				Provider: "claude",
			})
			recordAPIResponseMetadata(ctx, cfg, http.StatusOK, http.Header{"Content-Type": {"text/event-stream"}})
			for chunkIndex := 0; chunkIndex < chunkCount; chunkIndex++ {
				appendAPIResponseChunk(ctx, cfg, chunk)
			}
		}
	})

	b.Run("multi-attempt-retry", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, ctx, cfg := newRequestLogContext()
			for attemptIndex := 0; attemptIndex < 4; attemptIndex++ {
				recordAPIRequest(ctx, cfg, upstreamRequestLog{
					URL:      "https://example.com/v1/messages",
					Method:   http.MethodPost,
					Headers:  http.Header{"Content-Type": {"application/json"}},
					Body:     []byte(`{"stream":true}`),
					Provider: "claude",
				})
				recordAPIResponseMetadata(ctx, cfg, http.StatusOK, http.Header{"Content-Type": {"text/event-stream"}})
				for chunkIndex := 0; chunkIndex < chunkCount/4; chunkIndex++ {
					appendAPIResponseChunk(ctx, cfg, chunk)
				}
			}
		}
	})
}
