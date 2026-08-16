package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Natsume-kkk/prompt-pane/internal/provider"
)

func TestDecodePrompt(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "hook-user-prompt-submit.json"))
	if err != nil {
		t.Fatal(err)
	}
	event, err := DecodeHook(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if event.Kind != provider.PromptSubmitted || event.Prompt == nil || event.Prompt.ID != "turn_1" || event.Prompt.Text != "中文\nsecond line" {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestDecodeSessionLifecycleFixtures(t *testing.T) {
	tests := []struct {
		name   string
		kind   provider.Kind
		source string
	}{
		{name: "hook-session-start.json", kind: provider.SessionStarted, source: provider.SessionSourceResume},
		{name: "hook-session-start-clear.json", kind: provider.SessionStarted, source: provider.SessionSourceClear},
		{name: "hook-session-start-compact.json", kind: provider.SessionStarted, source: provider.SessionSourceCompact},
		{name: "hook-session-end.json", kind: provider.SessionEnded},
	}
	for _, test := range tests {
		data, err := os.ReadFile(filepath.Join("testdata", test.name))
		if err != nil {
			t.Fatal(err)
		}
		event, err := DecodeHook(strings.NewReader(string(data)))
		if err != nil || event.Kind != test.kind || event.SessionID != "thr_synthetic" {
			t.Fatalf("%s: %#v, %v", test.name, event, err)
		}
		if test.kind == provider.SessionStarted && event.Source != test.source {
			t.Fatalf("%s: source = %q, want %q", test.name, event.Source, test.source)
		}
	}
}

func TestSessionStartIgnoresTranscriptPath(t *testing.T) {
	input := `{"session_id":"thr_123","hook_event_name":"SessionStart","source":"startup","transcript_path":"C:\\synthetic\\current.jsonl"}`
	event, err := DecodeHook(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if event.Source != "startup" {
		t.Fatalf("startup source = %q", event.Source)
	}
}

func TestSessionStartAcceptsOfficialSources(t *testing.T) {
	for _, source := range []string{
		provider.SessionSourceStartup,
		provider.SessionSourceResume,
		provider.SessionSourceClear,
		provider.SessionSourceCompact,
	} {
		input := `{"session_id":"thr_123","hook_event_name":"SessionStart","source":"` + source + `"}`
		event, err := DecodeHook(strings.NewReader(input))
		if err != nil || event.Source != source {
			t.Fatalf("source %q: event = %#v, err = %v", source, event, err)
		}
	}
}

func TestSessionStartRejectsUnsupportedSource(t *testing.T) {
	input := `{"session_id":"thr_123","hook_event_name":"SessionStart","source":"fork"}`
	if _, err := DecodeHook(strings.NewReader(input)); err == nil {
		t.Fatal("expected unsupported source error")
	}
}

func TestDecodeHookRejectsPromptWithoutLeakingIt(t *testing.T) {
	secret := "never-print-this"
	_, err := DecodeHook(strings.NewReader(`{"session_id":"thr_123","hook_event_name":"UserPromptSubmit","prompt":"` + secret + `"}`))
	if err == nil {
		t.Fatal("expected validation error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("error leaked prompt")
	}
}

func TestDecodeHookRejectsOversizedInput(t *testing.T) {
	_, err := DecodeHook(strings.NewReader(strings.Repeat("x", MaxHookInput+1)))
	if err == nil {
		t.Fatal("expected size error")
	}
}

func TestDecodeHookRejectsTrailingJSON(t *testing.T) {
	input := `{"session_id":"thr_123","hook_event_name":"SessionStart"}{"extra":true}`
	if _, err := DecodeHook(strings.NewReader(input)); err == nil {
		t.Fatal("expected trailing JSON error")
	}
}

func TestDecodeStopReadsOnlyCurrentTranscriptMetrics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "current.jsonl")
	transcript := strings.Join([]string{
		`{"timestamp":"2026-08-16T01:00:00Z","type":"session_meta","payload":{"id":"thr_exact"}}`,
		`{"timestamp":"2026-08-16T01:01:00Z","type":"turn_context","payload":{"model":"gpt-5.4","effort":"high"}}`,
		`{"timestamp":"2026-08-16T01:02:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"output_tokens":250,"total_tokens":1250},"last_token_usage":{"input_tokens":32000},"model_context_window":128000},"rate_limits":{"limit_id":"codex","primary":{"used_percent":25,"window_minutes":300,"resets_at":9999999999},"secondary":{"used_percent":50,"window_minutes":10080,"resets_at":9999999999}}}}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(map[string]string{
		"session_id": "thr_exact", "hook_event_name": "Stop", "transcript_path": path,
	})
	if err != nil {
		t.Fatal(err)
	}
	event, err := DecodeHook(bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	metrics := event.Metrics
	if event.Kind != provider.MetricsUpdated || metrics == nil || metrics.Model != "gpt-5.4" || metrics.Effort != "high" || metrics.TotalTokens != 1250 || metrics.ContextWindow != 128000 || metrics.ContextUsedPercent != 25 {
		t.Fatalf("metrics event = %#v", event)
	}
	if metrics.FiveHour == nil || metrics.FiveHour.UsedPercent != 25 || metrics.SevenDay == nil || metrics.SevenDay.UsedPercent != 50 {
		t.Fatalf("quota metrics = %#v", metrics)
	}
}

func TestDecodeStopRejectsTranscriptFromAnotherSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "other.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"session_meta","payload":{"id":"thr_other"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(map[string]string{
		"session_id": "thr_current", "hook_event_name": "Stop", "transcript_path": path,
	})
	if _, err := DecodeHook(bytes.NewReader(input)); !errors.Is(err, ErrMetricsUnavailable) {
		t.Fatalf("mismatched transcript error = %v", err)
	}
}
