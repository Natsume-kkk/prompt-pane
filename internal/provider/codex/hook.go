package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Natsume-kkk/prompt-pane/internal/provider"
)

const MaxHookInput = 1 << 20

var ErrMetricsUnavailable = errors.New("current session metrics unavailable")

type hookInput struct {
	SessionID      string `json:"session_id"`
	HookEventName  string `json:"hook_event_name"`
	TurnID         string `json:"turn_id"`
	Prompt         string `json:"prompt"`
	Source         string `json:"source"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	Model          string `json:"model"`
}

func DecodeHook(r io.Reader) (provider.Event, error) {
	limited := io.LimitReader(r, MaxHookInput+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return provider.Event{}, fmt.Errorf("read hook input: %w", err)
	}
	if len(data) > MaxHookInput {
		return provider.Event{}, fmt.Errorf("hook input exceeds %d bytes", MaxHookInput)
	}

	var input hookInput
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&input); err != nil {
		return provider.Event{}, fmt.Errorf("decode hook input: invalid JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return provider.Event{}, fmt.Errorf("decode hook input: expected one JSON object")
	}
	if strings.TrimSpace(input.SessionID) == "" {
		return provider.Event{}, fmt.Errorf("hook input has no session id")
	}

	switch input.HookEventName {
	case "SessionStart":
		switch input.Source {
		case provider.SessionSourceStartup, provider.SessionSourceResume, provider.SessionSourceClear, provider.SessionSourceCompact:
		default:
			return provider.Event{}, fmt.Errorf("session hook has unsupported source")
		}
		return provider.Event{Kind: provider.SessionStarted, SessionID: input.SessionID, Source: input.Source}, nil
	case "UserPromptSubmit":
		if strings.TrimSpace(input.TurnID) == "" || input.Prompt == "" {
			return provider.Event{}, fmt.Errorf("prompt hook is missing required fields")
		}
		return provider.Event{
			Kind:      provider.PromptSubmitted,
			SessionID: input.SessionID,
			Prompt: &provider.UserPrompt{
				ID:   input.TurnID,
				Text: input.Prompt,
			},
		}, nil
	case "SessionEnd":
		return provider.Event{Kind: provider.SessionEnded, SessionID: input.SessionID}, nil
	case "Stop":
		if strings.TrimSpace(input.TranscriptPath) == "" {
			return provider.Event{}, fmt.Errorf("%w: stop hook has no transcript path", ErrMetricsUnavailable)
		}
		metrics, err := readMetrics(input.TranscriptPath, input.SessionID, input.CWD, input.Model)
		if err != nil {
			return provider.Event{}, fmt.Errorf("%w: %v", ErrMetricsUnavailable, err)
		}
		return provider.Event{Kind: provider.MetricsUpdated, SessionID: input.SessionID, Metrics: metrics}, nil
	default:
		return provider.Event{}, fmt.Errorf("unsupported hook event")
	}
}
