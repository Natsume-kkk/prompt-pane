package codex

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Natsume-kkk/prompt-pane/internal/provider"
)

func decodeHook(r io.Reader) (provider.Event, error) {
	event, _, err := DecodeHookWithObservation(r)
	return event, err
}

func TestDecodePrompt(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "hook-user-prompt-submit.json"))
	if err != nil {
		t.Fatal(err)
	}
	event, err := decodeHook(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if event.Kind != provider.PromptSubmitted || event.TurnID != "turn_1" || event.Prompt == nil || event.Prompt.ID != "turn_1" || event.Prompt.Text != "中文\nsecond line" {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestDecodePromptReturnsExactTurnObservation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "current.jsonl")
	input, err := json.Marshal(map[string]string{
		"session_id":      "thr_exact",
		"turn_id":         "turn_exact",
		"hook_event_name": "UserPromptSubmit",
		"prompt":          "synthetic prompt",
		"transcript_path": path,
	})
	if err != nil {
		t.Fatal(err)
	}
	event, observation, err := DecodeHookWithObservation(bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if event.Kind != provider.PromptSubmitted || observation == nil {
		t.Fatalf("event = %#v, observation = %#v", event, observation)
	}
	if observation.SessionID != "thr_exact" || observation.TurnID != "turn_exact" || observation.TranscriptPath != path || observation.Offset != 0 {
		t.Fatalf("observation = %#v", observation)
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
		event, err := decodeHook(strings.NewReader(string(data)))
		if err != nil || event.Kind != test.kind || event.SessionID != "thr_synthetic" {
			t.Fatalf("%s: %#v, %v", test.name, event, err)
		}
		if test.kind == provider.SessionStarted && event.Source != test.source {
			t.Fatalf("%s: source = %q, want %q", test.name, event.Source, test.source)
		}
	}
}

func TestSessionStartClassifiesPersistentWithoutRetainingTranscriptPath(t *testing.T) {
	input := `{"session_id":"thr_123","hook_event_name":"SessionStart","source":"startup","transcript_path":"C:\\synthetic\\current.jsonl"}`
	event, err := decodeHook(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if event.Source != "startup" || event.Ephemeral {
		t.Fatalf("persistent startup = %#v", event)
	}
}

func TestSessionStartClassifiesMissingTranscriptAsEphemeral(t *testing.T) {
	input := `{"session_id":"thr_side","hook_event_name":"SessionStart","source":"startup","transcript_path":null}`
	event, err := decodeHook(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if !event.Ephemeral {
		t.Fatalf("ephemeral startup = %#v", event)
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
		event, err := decodeHook(strings.NewReader(input))
		if err != nil || event.Source != source {
			t.Fatalf("source %q: event = %#v, err = %v", source, event, err)
		}
	}
}

func TestSessionStartRejectsUnsupportedSource(t *testing.T) {
	input := `{"session_id":"thr_123","hook_event_name":"SessionStart","source":"fork"}`
	if _, err := decodeHook(strings.NewReader(input)); err == nil {
		t.Fatal("expected unsupported source error")
	}
}

func TestDecodeHookRejectsPromptWithoutLeakingIt(t *testing.T) {
	secret := "never-print-this"
	_, err := decodeHook(strings.NewReader(`{"session_id":"thr_123","hook_event_name":"UserPromptSubmit","prompt":"` + secret + `"}`))
	if err == nil {
		t.Fatal("expected validation error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("error leaked prompt")
	}
}

func TestDecodeHookRejectsOversizedInput(t *testing.T) {
	_, err := decodeHook(strings.NewReader(strings.Repeat("x", MaxHookInput+1)))
	if err == nil {
		t.Fatal("expected size error")
	}
}

func TestDecodeHookRejectsTrailingJSON(t *testing.T) {
	input := `{"session_id":"thr_123","hook_event_name":"SessionStart"}{"extra":true}`
	if _, err := decodeHook(strings.NewReader(input)); err == nil {
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
		"session_id": "thr_exact", "turn_id": "turn_exact", "hook_event_name": "Stop", "transcript_path": path,
	})
	if err != nil {
		t.Fatal(err)
	}
	event, err := decodeHook(bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	metrics := event.Metrics
	if event.Kind != provider.TurnCompleted || event.TurnID != "turn_exact" || metrics == nil || metrics.Model != "gpt-5.4" || metrics.Effort != "high" || metrics.TotalTokens != 1250 || metrics.ContextWindow != 128000 || metrics.ContextUsedPercent != 25 {
		t.Fatalf("metrics event = %#v", event)
	}
	if metrics.QuotaStatus != provider.QuotaAvailable || len(metrics.Quotas) != 2 || metrics.Quotas[0].WindowMinutes != 300 || metrics.Quotas[0].UsedPercent != 25 || metrics.Quotas[1].WindowMinutes != 10080 || metrics.Quotas[1].UsedPercent != 50 {
		t.Fatalf("quota metrics = %#v", metrics)
	}
}

func TestDecodeStopCompletesWhenTranscriptBelongsToAnotherSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "other.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"session_meta","payload":{"id":"thr_other"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(map[string]string{
		"session_id": "thr_current", "turn_id": "turn_current", "hook_event_name": "Stop", "transcript_path": path,
	})
	event, err := decodeHook(bytes.NewReader(input))
	if err != nil || event.Kind != provider.TurnCompleted || event.TurnID != "turn_current" || event.Metrics != nil {
		t.Fatalf("mismatched transcript completion = %#v, %v", event, err)
	}
}

func TestDecodeStopWithoutTranscriptStillCompletes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "hook-stop.json"))
	if err != nil {
		t.Fatal(err)
	}
	event, err := decodeHook(bytes.NewReader(data))
	if err != nil || event.Kind != provider.TurnCompleted || event.TurnID != "turn_synthetic" || event.Metrics != nil {
		t.Fatalf("missing transcript completion = %#v, %v", event, err)
	}
}

func TestDecodeStopRequiresTurnID(t *testing.T) {
	input := `{"session_id":"thr_current","hook_event_name":"Stop","transcript_path":null}`
	if _, err := decodeHook(strings.NewReader(input)); err == nil {
		t.Fatal("Stop without turn_id was accepted")
	}
}
