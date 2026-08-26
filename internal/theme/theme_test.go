package theme

import (
	"image/color"
	"testing"
)

func TestPalettesKeepSharedSlotsAndCanonicalText(t *testing.T) {
	want := map[string]Palette{
		Mocha:     {Name: Mocha, Text: "#cdd6f4", Overlay0: "#6c7086", Green: "#a6e3a1", Yellow: "#f9e2af", Peach: "#fab387", Red: "#f38ba8", Blue: "#89b4fa", Sapphire: "#74c7ec", Mauve: "#cba6f7", Pink: "#f5c2e7", Cell: "#313244"},
		Latte:     {Name: Latte, IsLight: true, Text: "#4c4f69", Overlay0: "#9ca0b0", Green: "#40a02b", Yellow: "#df8e1d", Peach: "#fe640b", Red: "#d20f39", Blue: "#1e66f5", Sapphire: "#209fb5", Mauve: "#8839ef", Pink: "#ea76cb", Cell: "#ccd0da"},
		Frappe:    {Name: Frappe, Text: "#c6d0f5", Overlay0: "#737994", Green: "#a6d189", Yellow: "#e5c890", Peach: "#ef9f76", Red: "#e78284", Blue: "#8caaee", Sapphire: "#85c1dc", Mauve: "#ca9ee6", Pink: "#f4b8e4", Cell: "#414559"},
		Macchiato: {Name: Macchiato, Text: "#cad3f5", Overlay0: "#6e738d", Green: "#a6da95", Yellow: "#eed49f", Peach: "#f5a97f", Red: "#ed8796", Blue: "#8aadf4", Sapphire: "#7dc4e4", Mauve: "#c6a0f6", Pink: "#f5bde6", Cell: "#363a4f"},
		Nord:      {Name: Nord, Text: "#d8dee9", Overlay0: "#4c566a", Green: "#a3be8c", Yellow: "#ebcb8b", Peach: "#d08770", Red: "#bf616a", Blue: "#5e81ac", Sapphire: "#88c0d0", Mauve: "#b48ead", Pink: "#b48ead", Cell: "#3b4252"},
		Dracula:   {Name: Dracula, Text: "#f8f8f2", Overlay0: "#6272a4", Green: "#50fa7b", Yellow: "#f1fa8c", Peach: "#ffb86c", Red: "#ff5555", Blue: "#bd93f9", Sapphire: "#8be9fd", Mauve: "#bd93f9", Pink: "#ff79c6", Cell: "#44475a"},
	}
	for name, expected := range want {
		if got := Resolve(name, false); got != expected {
			t.Fatalf("%s palette = %#v, want %#v", name, got, expected)
		}
	}
}

func TestAutoUsesOnlyMochaOrLatte(t *testing.T) {
	if got := Resolve(Auto, false).Name; got != Mocha {
		t.Fatalf("dark auto = %q", got)
	}
	if got := Resolve(Auto, true).Name; got != Latte {
		t.Fatalf("light auto = %q", got)
	}
}

func TestNamesAndValidation(t *testing.T) {
	want := []string{Auto, Mocha, Latte, Frappe, Macchiato, Nord, Dracula}
	for _, name := range want {
		if !Valid(name) {
			t.Fatalf("theme %q was rejected", name)
		}
	}
	if Valid("unknown") {
		t.Fatal("unknown theme was accepted")
	}
}

func TestSelectableNamesExcludesAuto(t *testing.T) {
	want := []string{Mocha, Latte, Frappe, Macchiato, Nord, Dracula}
	got := SelectableNames()
	if len(got) != len(want) {
		t.Fatalf("selectable names = %#v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("selectable names = %#v", got)
		}
	}
}

func TestThemePickerRoleUsesEachPaletteMauve(t *testing.T) {
	for _, name := range SelectableNames() {
		palette := Resolve(name, false)
		if got := Derive(palette).ThemePick; got != palette.Mauve {
			t.Fatalf("%s theme picker color = %q, want %q", name, got, palette.Mauve)
		}
	}
}

func TestProgressRolesAdaptToThemeBackground(t *testing.T) {
	for _, name := range SelectableNames() {
		palette := Resolve(name, false)
		roles := Derive(palette)
		wantProgress := palette.Sapphire
		if name == Latte {
			wantProgress = palette.Mauve
		}
		if roles.FocusMarker != wantProgress || roles.Accent != wantProgress || roles.ProgressFill != wantProgress {
			t.Fatalf("%s progress roles = marker %q, accent %q, progress %q; want %q", name, roles.FocusMarker, roles.Accent, roles.ProgressFill, wantProgress)
		}
		if roles.ActivityIndicator != wantProgress || roles.Activity != wantProgress {
			t.Fatalf("%s activity roles = indicator %q, compatibility %q; want %q", name, roles.ActivityIndicator, roles.Activity, wantProgress)
		}
		if roles.BodyText != palette.Text {
			t.Fatalf("%s body text = %q, want %q", name, roles.BodyText, palette.Text)
		}
	}
}

func TestInformationAndGitRolesUseStableSemantics(t *testing.T) {
	for _, name := range SelectableNames() {
		palette := Resolve(name, false)
		roles := Derive(palette)
		wantFocus := palette.Sapphire
		if name == Latte {
			wantFocus = palette.Mauve
		}
		for role, colors := range map[string][2]string{
			"body text":         {roles.BodyText, palette.Text},
			"accent":            {roles.Accent, wantFocus},
			"branch":            {roles.Branch, palette.Red},
			"added":             {roles.Added, palette.Green},
			"deleted":           {roles.Deleted, palette.Red},
			"untracked":         {roles.Untracked, palette.Mauve},
			"label":             {roles.Label, palette.Pink},
			"token":             {roles.Token, palette.Peach},
			"model":             {roles.Model, palette.Red},
			"selection text":    {roles.SelectionText, palette.Text},
			"selection surface": {roles.SelectionSurface, palette.Cell},
			"theme pick":        {roles.ThemePick, palette.Mauve},
		} {
			if colors[0] != colors[1] {
				t.Fatalf("%s %s color = %q, want %q", name, role, colors[0], colors[1])
			}
		}
		if roles.Activity != roles.ProgressFill {
			t.Fatalf("%s activity %q and progress %q do not share progress semantics", name, roles.Activity, roles.ProgressFill)
		}
	}
}

func TestExplicitSelectionMeetsTextContrastMinimum(t *testing.T) {
	for _, name := range SelectableNames() {
		palette := Resolve(name, false)
		ratio, ok := ContrastRatio(palette.Text, palette.Cell)
		if !ok || ratio < MinimumTextContrast {
			t.Fatalf("%s text/cell contrast = %.2f:1, want at least 4.5:1", name, ratio)
		}
	}
}

func TestContrastUtilitiesRejectInvalidColorsAndPreserveTerminalRGB(t *testing.T) {
	if got := ColorHex(color.RGBA{R: 0xef, G: 0xf1, B: 0xf5, A: 0xff}); got != "#eff1f5" {
		t.Fatalf("terminal color = %q", got)
	}
	if ratio, ok := ContrastRatio("not-a-color", "#000000"); ok || ratio != 0 {
		t.Fatalf("invalid contrast = %.2f, ok = %v", ratio, ok)
	}
	if MeetsContrast("#777777", "#777777", MinimumTextContrast) {
		t.Fatal("identical colors met the text contrast threshold")
	}
}
