package helps

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
			RecordAPIRequest(ctx, cfg, UpstreamRequestLog{
				URL:      "https://example.com/v1/messages",
				Method:   http.MethodPost,
				Headers:  http.Header{"Content-Type": {"application/json"}},
				Body:     []byte(`{"stream":true}`),
				Provider: "claude",
			})
			RecordAPIResponseMetadata(ctx, cfg, http.StatusOK, http.Header{"Content-Type": {"text/event-stream"}})
			for chunkIndex := 0; chunkIndex < chunkCount; chunkIndex++ {
				AppendAPIResponseChunk(ctx, cfg, chunk)
			}
		}
	})

	b.Run("multi-attempt-retry", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, ctx, cfg := newRequestLogContext()
			for attemptIndex := 0; attemptIndex < 4; attemptIndex++ {
				RecordAPIRequest(ctx, cfg, UpstreamRequestLog{
					URL:      "https://example.com/v1/messages",
					Method:   http.MethodPost,
					Headers:  http.Header{"Content-Type": {"application/json"}},
					Body:     []byte(`{"stream":true}`),
					Provider: "claude",
				})
				RecordAPIResponseMetadata(ctx, cfg, http.StatusOK, http.Header{"Content-Type": {"text/event-stream"}})
				for chunkIndex := 0; chunkIndex < chunkCount/4; chunkIndex++ {
					AppendAPIResponseChunk(ctx, cfg, chunk)
				}
			}
		}
	})
}
