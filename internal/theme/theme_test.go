package theme

import "testing"

func TestPalettesMatchTokenTracker(t *testing.T) {
	want := map[string]Palette{
		Mocha:     {Name: Mocha, Overlay0: "#6c7086", Green: "#a6e3a1", Yellow: "#f9e2af", Peach: "#fab387", Red: "#f38ba8", Blue: "#89b4fa", Sapphire: "#74c7ec", Mauve: "#cba6f7", Pink: "#f5c2e7", Cell: "#313244"},
		Latte:     {Name: Latte, IsLight: true, Overlay0: "#9ca0b0", Green: "#40a02b", Yellow: "#df8e1d", Peach: "#fe640b", Red: "#d20f39", Blue: "#1e66f5", Sapphire: "#209fb5", Mauve: "#8839ef", Pink: "#ea76cb", Cell: "#ccd0da"},
		Frappe:    {Name: Frappe, Overlay0: "#737994", Green: "#a6d189", Yellow: "#e5c890", Peach: "#ef9f76", Red: "#e78284", Blue: "#8caaee", Sapphire: "#85c1dc", Mauve: "#ca9ee6", Pink: "#f4b8e4", Cell: "#414559"},
		Macchiato: {Name: Macchiato, Overlay0: "#6e738d", Green: "#a6da95", Yellow: "#eed49f", Peach: "#f5a97f", Red: "#ed8796", Blue: "#8aadf4", Sapphire: "#7dc4e4", Mauve: "#c6a0f6", Pink: "#f5bde6", Cell: "#363a4f"},
		Nord:      {Name: Nord, Overlay0: "#4c566a", Green: "#a3be8c", Yellow: "#ebcb8b", Peach: "#d08770", Red: "#bf616a", Blue: "#5e81ac", Sapphire: "#88c0d0", Mauve: "#b48ead", Pink: "#b48ead", Cell: "#3b4252"},
		Dracula:   {Name: Dracula, Overlay0: "#6272a4", Green: "#50fa7b", Yellow: "#f1fa8c", Peach: "#ffb86c", Red: "#ff5555", Blue: "#bd93f9", Sapphire: "#8be9fd", Mauve: "#bd93f9", Pink: "#ff79c6", Cell: "#44475a"},
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
	got := Names()
	if len(got) != len(want) {
		t.Fatalf("names = %#v", got)
	}
	for index := range want {
		if got[index] != want[index] || !Valid(want[index]) {
			t.Fatalf("names = %#v", got)
		}
	}
	if Valid("unknown") {
		t.Fatal("unknown theme was accepted")
	}
}
