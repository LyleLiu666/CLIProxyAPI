package tui

import (
	"strings"
	"testing"
)

func TestFormatCodexWindow_ShowsRemainingPercent(t *testing.T) {
	window := map[string]any{
		"used_percent":         float64(42),
		"limit_window_seconds": float64(300),
		"reset_at":             float64(1700000000),
	}

	got := formatCodexWindow(window)
	if !strings.Contains(got, "58% left") {
		t.Fatalf("expected remaining percentage in %q", got)
	}
	if !strings.Contains(got, "5m") {
		t.Fatalf("expected window duration in %q", got)
	}
}
