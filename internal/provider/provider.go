package provider

type Kind string

const (
	SessionStarted  Kind = "session.started"
	PromptSubmitted Kind = "prompt.submitted"
	MetricsUpdated  Kind = "metrics.updated"
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

type QuotaWindow struct {
	UsedPercent float64 `json:"used_percent"`
	ResetsAt    int64   `json:"resets_at,omitempty"`
}

type SessionMetrics struct {
	Branch             string       `json:"branch,omitempty"`
	Added              int          `json:"added,omitempty"`
	Deleted            int          `json:"deleted,omitempty"`
	Untracked          int          `json:"untracked,omitempty"`
	Model              string       `json:"model,omitempty"`
	Effort             string       `json:"effort,omitempty"`
	TotalTokens        int64        `json:"total_tokens,omitempty"`
	ContextWindow      int64        `json:"context_window,omitempty"`
	ContextUsedPercent float64      `json:"context_used_percent,omitempty"`
	FiveHour           *QuotaWindow `json:"five_hour,omitempty"`
	SevenDay           *QuotaWindow `json:"seven_day,omitempty"`
}

type Event struct {
	Kind      Kind            `json:"kind"`
	SessionID string          `json:"session_id"`
	Source    string          `json:"source,omitempty"`
	Prompt    *UserPrompt     `json:"prompt,omitempty"`
	Metrics   *SessionMetrics `json:"metrics,omitempty"`
}
