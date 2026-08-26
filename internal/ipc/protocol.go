package ipc

import "github.com/Natsume-kkk/prompt-pane/internal/provider"

const (
	ProtocolVersion = 1
	MaxMessageBytes = 8 << 20
)

type Request struct {
	Version int            `json:"version"`
	RunID   string         `json:"run_id"`
	Token   string         `json:"token"`
	Type    string         `json:"type"`
	Event   provider.Event `json:"event,omitempty"`
	Watch   *TurnWatch     `json:"watch,omitempty"`
}

type Response struct {
	OK       bool `json:"ok"`
	Watching bool `json:"watching,omitempty"`
	Release  bool `json:"release,omitempty"`
}

type TurnWatch struct {
	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id"`
}

type Snapshot struct {
	State          string                   `json:"state"`
	Prompts        []provider.UserPrompt    `json:"prompts"`
	Notice         string                   `json:"notice"`
	ActiveTurnID   string                   `json:"active_turn_id,omitempty"`
	ActivePromptID string                   `json:"active_prompt_id,omitempty"`
	Metrics        *provider.SessionMetrics `json:"metrics,omitempty"`
}
