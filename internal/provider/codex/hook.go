package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Natsume-kkk/prompt-pane/internal/provider"
)

const MaxHookInput = 1 << 20

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

func DecodeHookWithObservation(r io.Reader) (provider.Event, *TurnObservation, error) {
	limited := io.LimitReader(r, MaxHookInput+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return provider.Event{}, nil, fmt.Errorf("read hook input: %w", err)
	}
	if len(data) > MaxHookInput {
		return provider.Event{}, nil, fmt.Errorf("hook input exceeds %d bytes", MaxHookInput)
	}

	var input hookInput
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&input); err != nil {
		return provider.Event{}, nil, fmt.Errorf("decode hook input: invalid JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return provider.Event{}, nil, fmt.Errorf("decode hook input: expected one JSON object")
	}
	if strings.TrimSpace(input.SessionID) == "" {
		return provider.Event{}, nil, fmt.Errorf("hook input has no session id")
	}

	switch input.HookEventName {
	case "SessionStart":
		switch input.Source {
		case provider.SessionSourceStartup, provider.SessionSourceResume, provider.SessionSourceClear, provider.SessionSourceCompact:
		default:
			return provider.Event{}, nil, fmt.Errorf("session hook has unsupported source")
		}
		return provider.Event{
			Kind:      provider.SessionStarted,
			SessionID: input.SessionID,
			Source:    input.Source,
			Ephemeral: strings.TrimSpace(input.TranscriptPath) == "",
		}, nil, nil
	case "UserPromptSubmit":
		if strings.TrimSpace(input.TurnID) == "" || input.Prompt == "" {
			return provider.Event{}, nil, fmt.Errorf("prompt hook is missing required fields")
		}
		event := provider.Event{
			Kind:      provider.PromptSubmitted,
			SessionID: input.SessionID,
			TurnID:    input.TurnID,
			Prompt: &provider.UserPrompt{
				ID:   input.TurnID,
				Text: input.Prompt,
			},
		}
		if strings.TrimSpace(input.TranscriptPath) == "" {
			return event, nil, nil
		}
		return event, &TurnObservation{
			SessionID:      input.SessionID,
			TurnID:         input.TurnID,
			TranscriptPath: input.TranscriptPath,
		}, nil
	case "SessionEnd":
		return provider.Event{Kind: provider.SessionEnded, SessionID: input.SessionID}, nil, nil
	case "Stop":
		if strings.TrimSpace(input.TurnID) == "" {
			return provider.Event{}, nil, fmt.Errorf("stop hook has no turn id")
		}
		event := provider.Event{Kind: provider.TurnCompleted, SessionID: input.SessionID, TurnID: input.TurnID}
		if strings.TrimSpace(input.TranscriptPath) != "" {
			metrics, err := readMetrics(input.TranscriptPath, input.SessionID, input.CWD, input.Model)
			if err == nil {
				event.Metrics = metrics
			}
		}
		return event, nil, nil
	default:
		return provider.Event{}, nil, fmt.Errorf("unsupported hook event")
	}
}
