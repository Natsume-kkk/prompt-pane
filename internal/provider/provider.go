package provider

type Kind string

const (
	SessionStarted  Kind = "session.started"
	PromptSubmitted Kind = "prompt.submitted"
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

type Event struct {
	Kind      Kind        `json:"kind"`
	SessionID string      `json:"session_id"`
	Source    string      `json:"source,omitempty"`
	Prompt    *UserPrompt `json:"prompt,omitempty"`
}
