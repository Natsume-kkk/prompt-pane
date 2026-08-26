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
}

type Response struct {
	OK bool `json:"ok"`
}

type Snapshot struct {
	State          string                   `json:"state"`
	Prompts        []provider.UserPrompt    `json:"prompts"`
	Notice         string                   `json:"notice"`
	ActiveTurnID   string                   `json:"active_turn_id,omitempty"`
	ActivePromptID string                   `json:"active_prompt_id,omitempty"`
	Metrics        *provider.SessionMetrics `json:"metrics,omitempty"`
}
