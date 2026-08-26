package provider

type Kind string

const (
	SessionStarted  Kind = "session.started"
	PromptSubmitted Kind = "prompt.submitted"
	TurnCompleted   Kind = "turn.completed"
	SessionEnded    Kind = "session.ended"
)

const (
	SessionSourceStartup = "startup"
	SessionSourceResume  = "resume"
	SessionSourceClear   = "clear"
	SessionSourceCompact = "compact"
)

type UserPrompt struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type QuotaStatus string

const (
	QuotaUnknown     QuotaStatus = ""
	QuotaAvailable   QuotaStatus = "available"
	QuotaUnavailable QuotaStatus = "unavailable"
)

type QuotaWindow struct {
	WindowMinutes int64   `json:"window_minutes"`
	UsedPercent   float64 `json:"used_percent"`
	ResetsAt      int64   `json:"resets_at,omitempty"`
}

type SessionMetrics struct {
	Branch             string        `json:"branch,omitempty"`
	Added              int           `json:"added,omitempty"`
	Deleted            int           `json:"deleted,omitempty"`
	Untracked          int           `json:"untracked,omitempty"`
	Model              string        `json:"model,omitempty"`
	Effort             string        `json:"effort,omitempty"`
	TotalTokens        int64         `json:"total_tokens,omitempty"`
	ContextWindow      int64         `json:"context_window,omitempty"`
	ContextUsedPercent float64       `json:"context_used_percent,omitempty"`
	Quotas             []QuotaWindow `json:"quotas,omitempty"`
	QuotaStatus        QuotaStatus   `json:"quota_status,omitempty"`
}

type Event struct {
	Kind      Kind            `json:"kind"`
	SessionID string          `json:"session_id"`
	TurnID    string          `json:"turn_id,omitempty"`
	Source    string          `json:"source,omitempty"`
	Ephemeral bool            `json:"ephemeral,omitempty"`
	Prompt    *UserPrompt     `json:"prompt,omitempty"`
	Metrics   *SessionMetrics `json:"metrics,omitempty"`
}
