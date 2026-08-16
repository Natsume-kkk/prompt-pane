package theme

import "strings"

const (
	Auto        = "auto"
	Mocha       = "mocha"
	Latte       = "latte"
	Frappe      = "frappe"
	Macchiato   = "macchiato"
	Nord        = "nord"
	Dracula     = "dracula"
	Environment = "PROMPT_PANE_THEME"
)

var names = []string{Auto, Mocha, Latte, Frappe, Macchiato, Nord, Dracula}

// Palette uses the same nine base slots and exact Hex values as token-tracker.
// UI roles are derived from these slots so every Prompt Pane surface shares one
// color source.
type Palette struct {
	Name     string
	IsLight  bool
	Overlay0 string
	Green    string
	Yellow   string
	Peach    string
	Red      string
	Blue     string
	Sapphire string
	Mauve    string
	Pink     string
	Cell     string
}

type Roles struct {
	Muted     string
	Accent    string
	Success   string
	Warning   string
	Error     string
	Project   string
	Branch    string
	Added     string
	Deleted   string
	Untracked string
	Label     string
	Token     string
	Model     string
	Selection string
	Cell      string
}

var palettes = map[string]Palette{
	Mocha: {
		Name: Mocha, Overlay0: "#6c7086", Green: "#a6e3a1", Yellow: "#f9e2af",
		Peach: "#fab387", Red: "#f38ba8", Blue: "#89b4fa", Sapphire: "#74c7ec",
		Mauve: "#cba6f7", Pink: "#f5c2e7", Cell: "#313244",
	},
	Latte: {
		Name: Latte, IsLight: true, Overlay0: "#9ca0b0", Green: "#40a02b", Yellow: "#df8e1d",
		Peach: "#fe640b", Red: "#d20f39", Blue: "#1e66f5", Sapphire: "#209fb5",
		Mauve: "#8839ef", Pink: "#ea76cb", Cell: "#ccd0da",
	},
	Frappe: {
		Name: Frappe, Overlay0: "#737994", Green: "#a6d189", Yellow: "#e5c890",
		Peach: "#ef9f76", Red: "#e78284", Blue: "#8caaee", Sapphire: "#85c1dc",
		Mauve: "#ca9ee6", Pink: "#f4b8e4", Cell: "#414559",
	},
	Macchiato: {
		Name: Macchiato, Overlay0: "#6e738d", Green: "#a6da95", Yellow: "#eed49f",
		Peach: "#f5a97f", Red: "#ed8796", Blue: "#8aadf4", Sapphire: "#7dc4e4",
		Mauve: "#c6a0f6", Pink: "#f5bde6", Cell: "#363a4f",
	},
	Nord: {
		Name: Nord, Overlay0: "#4c566a", Green: "#a3be8c", Yellow: "#ebcb8b",
		Peach: "#d08770", Red: "#bf616a", Blue: "#5e81ac", Sapphire: "#88c0d0",
		Mauve: "#b48ead", Pink: "#b48ead", Cell: "#3b4252",
	},
	Dracula: {
		Name: Dracula, Overlay0: "#6272a4", Green: "#50fa7b", Yellow: "#f1fa8c",
		Peach: "#ffb86c", Red: "#ff5555", Blue: "#bd93f9", Sapphire: "#8be9fd",
		Mauve: "#bd93f9", Pink: "#ff79c6", Cell: "#44475a",
	},
}

func Names() []string {
	return append([]string(nil), names...)
}

func Valid(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == Auto {
		return true
	}
	_, ok := palettes[name]
	return ok
}

func Resolve(name string, lightBackground bool) Palette {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == Auto || name == "" {
		if lightBackground {
			return palettes[Latte]
		}
		return palettes[Mocha]
	}
	if palette, ok := palettes[name]; ok {
		return palette
	}
	return palettes[Mocha]
}

func Derive(p Palette) Roles {
	return Roles{
		Muted: p.Overlay0, Accent: p.Sapphire, Success: p.Green, Warning: p.Yellow,
		Error: p.Red, Project: p.Green, Branch: p.Red, Added: p.Green, Deleted: p.Red,
		Untracked: p.Mauve, Label: p.Pink, Token: p.Peach, Model: p.Blue,
		Selection: p.Sapphire, Cell: p.Cell,
	}
}
