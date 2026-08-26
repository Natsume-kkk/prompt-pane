package theme

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"
)

const (
	Auto        = "auto"
	Mocha       = "mocha"
	Latte       = "latte"
	Frappe      = "frappe"
	Macchiato   = "macchiato"
	Nord        = "nord"
	Dracula     = "dracula"
	Environment = "PROMPT_PANE_THEME"

	MinimumTextContrast              = 4.5
	MinimumSelectionBoundaryContrast = 3.0
)

var names = []string{Auto, Mocha, Latte, Frappe, Macchiato, Nord, Dracula}

// Palette preserves the exact token-tracker slots and adds each theme's
// canonical text color for explicit foreground/background selections.
type Palette struct {
	Name     string
	IsLight  bool
	Text     string
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
	BodyText          string
	FocusMarker       string
	ActivityIndicator string
	ProgressFill      string
	Muted             string
	Accent            string
	Activity          string
	Success           string
	Warning           string
	Error             string
	Branch            string
	Added             string
	Deleted           string
	Untracked         string
	Label             string
	Token             string
	Model             string
	SelectionText     string
	SelectionSurface  string
	ThemePick         string
}

var palettes = map[string]Palette{
	Mocha: {
		Name: Mocha, Text: "#cdd6f4", Overlay0: "#6c7086", Green: "#a6e3a1", Yellow: "#f9e2af",
		Peach: "#fab387", Red: "#f38ba8", Blue: "#89b4fa", Sapphire: "#74c7ec",
		Mauve: "#cba6f7", Pink: "#f5c2e7", Cell: "#313244",
	},
	Latte: {
		Name: Latte, IsLight: true, Text: "#4c4f69", Overlay0: "#9ca0b0", Green: "#40a02b", Yellow: "#df8e1d",
		Peach: "#fe640b", Red: "#d20f39", Blue: "#1e66f5", Sapphire: "#209fb5",
		Mauve: "#8839ef", Pink: "#ea76cb", Cell: "#ccd0da",
	},
	Frappe: {
		Name: Frappe, Text: "#c6d0f5", Overlay0: "#737994", Green: "#a6d189", Yellow: "#e5c890",
		Peach: "#ef9f76", Red: "#e78284", Blue: "#8caaee", Sapphire: "#85c1dc",
		Mauve: "#ca9ee6", Pink: "#f4b8e4", Cell: "#414559",
	},
	Macchiato: {
		Name: Macchiato, Text: "#cad3f5", Overlay0: "#6e738d", Green: "#a6da95", Yellow: "#eed49f",
		Peach: "#f5a97f", Red: "#ed8796", Blue: "#8aadf4", Sapphire: "#7dc4e4",
		Mauve: "#c6a0f6", Pink: "#f5bde6", Cell: "#363a4f",
	},
	Nord: {
		Name: Nord, Text: "#d8dee9", Overlay0: "#4c566a", Green: "#a3be8c", Yellow: "#ebcb8b",
		Peach: "#d08770", Red: "#bf616a", Blue: "#5e81ac", Sapphire: "#88c0d0",
		Mauve: "#b48ead", Pink: "#b48ead", Cell: "#3b4252",
	},
	Dracula: {
		Name: Dracula, Text: "#f8f8f2", Overlay0: "#6272a4", Green: "#50fa7b", Yellow: "#f1fa8c",
		Peach: "#ffb86c", Red: "#ff5555", Blue: "#bd93f9", Sapphire: "#8be9fd",
		Mauve: "#bd93f9", Pink: "#ff79c6", Cell: "#44475a",
	},
}

func SelectableNames() []string {
	return append([]string(nil), names[1:]...)
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
	focusMarker := p.Sapphire
	if p.Name == Latte {
		focusMarker = p.Mauve
	}
	return Roles{
		BodyText: p.Text, FocusMarker: focusMarker, ActivityIndicator: focusMarker, ProgressFill: focusMarker,
		Muted: p.Overlay0, Accent: focusMarker, Activity: focusMarker, Success: p.Green, Warning: p.Yellow,
		Error: p.Red, Branch: p.Red, Added: p.Green, Deleted: p.Red,
		Untracked: p.Mauve, Label: p.Pink, Token: p.Peach, Model: p.Red,
		SelectionText: p.Text, SelectionSurface: p.Cell, ThemePick: p.Mauve,
	}
}

func ColorHex(value color.Color) string {
	if value == nil {
		return ""
	}
	red, green, blue, _ := value.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", red>>8, green>>8, blue>>8)
}

func MeetsContrast(foreground, background string, minimum float64) bool {
	ratio, ok := ContrastRatio(foreground, background)
	return ok && ratio >= minimum
}

func ContrastRatio(foreground, background string) (float64, bool) {
	foregroundLuminance, foregroundOK := relativeLuminance(foreground)
	backgroundLuminance, backgroundOK := relativeLuminance(background)
	if !foregroundOK || !backgroundOK {
		return 0, false
	}
	if foregroundLuminance < backgroundLuminance {
		foregroundLuminance, backgroundLuminance = backgroundLuminance, foregroundLuminance
	}
	return (foregroundLuminance + 0.05) / (backgroundLuminance + 0.05), true
}

func relativeLuminance(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	if len(value) != 7 || value[0] != '#' {
		return 0, false
	}
	channels := [3]float64{}
	for index := range channels {
		channel, err := strconv.ParseUint(value[1+index*2:3+index*2], 16, 8)
		if err != nil {
			return 0, false
		}
		normalized := float64(channel) / 255
		if normalized <= 0.04045 {
			channels[index] = normalized / 12.92
		} else {
			channels[index] = math.Pow((normalized+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*channels[0] + 0.7152*channels[1] + 0.0722*channels[2], true
}
