package logger

import (
	"bytes"
	"strings"
	"testing"
)

func TestLevels(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"debug", LevelDebug},
		{"warn", LevelWarn},
		{"warning", LevelWarn},
		{"error", LevelError},
		{"info", LevelInfo},
		{"", LevelInfo},
		{"bogus", LevelInfo},
	}
	var sink bytes.Buffer
	for _, tt := range tests {
		l := New(&sink, tt.in)
		if l.level != tt.want {
			t.Errorf("level(%q) = %d, want %d", tt.in, l.level, tt.want)
		}
	}
}

func TestOutput(t *testing.T) {
	var sb strings.Builder
	l := New(&sb, "info")
	l.Debug("hidden message", "k", "v") // below level -> skipped
	l.Info("server started", "address", ":8080")
	l.Warn("careful", "detail", "has spaces")
	l.Error("boom")

	out := sb.String()
	for _, want := range []string{
		"INFO server started address=:8080",
		`WARN careful detail="has spaces"`,
		"ERROR boom",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hidden") {
		t.Error("debug message should be suppressed at info level")
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, ln := range lines {
		if strings.Count(ln, "\n") != 0 || len(ln) == 0 {
			t.Errorf("bad line: %q", ln)
		}
	}
}
