package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Natsume-kkk/prompt-pane/internal/config"
	"github.com/Natsume-kkk/prompt-pane/internal/ipc"
	"github.com/Natsume-kkk/prompt-pane/internal/paths"
	"github.com/Natsume-kkk/prompt-pane/internal/provider"
	"github.com/Natsume-kkk/prompt-pane/internal/theme"
)

func leftClick(model Model, x, y int) Model {
	updated, _ := model.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseNone})
	return updated.(Model)
}

func assertSelectionCommand(t *testing.T, command tea.Cmd, expected string) {
	t.Helper()
	if command == nil {
		t.Fatal("selection command is nil")
	}
	if message := command(); fmt.Sprint(message) != expected {
		t.Fatalf("selection command = %T %v, want clipboard text %q", message, message, expected)
	}
}

func TestRenderFitsFixedSizes(t *testing.T) {
	sizes := [][2]int{{20, 6}, {24, 10}, {32, 12}, {48, 20}, {80, 24}}
	for _, noColor := range []bool{false, true} {
		for _, size := range sizes {
			model := Model{
				width:     size[0],
				height:    size[1],
				following: true,
				noColor:   noColor,
				snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{
					{ID: "1", Text: "中文 e\u0301 👨‍👩‍👧‍👦 mixed text with a very long line that must wrap"},
					{ID: "2", Text: "same\nsame"},
				}},
			}
			output := model.render()
			lines := strings.Split(output, "\n")
			if len(lines) > size[1] {
				t.Fatalf("color=%v %dx%d rendered %d lines", !noColor, size[0], size[1], len(lines))
			}
			for index, line := range lines {
				if got := ansi.StringWidth(line); got > size[0] {
					t.Fatalf("color=%v %dx%d line %d width = %d: %q", !noColor, size[0], size[1], index, got, line)
				}
			}
		}
	}
}

func TestAllTokenTrackerThemesRenderAtFixedSizes(t *testing.T) {
	sizes := [][2]int{{20, 6}, {24, 10}, {32, 12}, {48, 20}, {80, 24}}
	for _, name := range theme.Names()[1:] {
		for _, size := range sizes {
			model := Model{
				width: size[0], height: size[1], following: true,
				snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "1", Text: "中文 prompt"}}, Metrics: &provider.SessionMetrics{
					TotalTokens: 12000, ContextWindow: 128000, ContextUsedPercent: 42,
					FiveHour: &provider.QuotaWindow{UsedPercent: 55}, SevenDay: &provider.QuotaWindow{UsedPercent: 82},
				}},
			}
			model.applyTheme(name)
			output := model.render()
			for index, line := range strings.Split(output, "\n") {
				if got := ansi.StringWidth(line); got > size[0] {
					t.Fatalf("theme=%s %dx%d line %d width = %d", name, size[0], size[1], index, got)
				}
			}
			palette := theme.Resolve(name, false)
			expectedState := lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Green)).Render("[LIVE]")
			if !strings.Contains(output, expectedState) {
				t.Fatalf("theme=%s did not use its exact success color: %q", name, output)
			}
		}
	}
}

func TestWrapTextPreservesGraphemeClustersAndWhitespace(t *testing.T) {
	text := "  👨‍👩‍👧‍👦e\u0301🏳️‍🌈  "
	want := []string{"  ", "👨‍👩‍👧‍👦", "e\u0301", "🏳️‍🌈", "  "}
	got := wrapText(text, 2)
	if len(got) != len(want) {
		t.Fatalf("wrapped lines = %#v", got)
	}
	for index := range want {
		if got[index] != want[index] || ansi.StringWidth(got[index]) > 2 {
			t.Fatalf("line %d = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestWrapTextPrefersWordBoundariesAndSplitsOnlyOversizedWords(t *testing.T) {
	if got := wrapText("12345678 codex events", 10); !slices.Equal(got, []string{"12345678", "codex", "events"}) {
		t.Fatalf("ordinary words were split: %#v", got)
	}
	longWord := "supercalifragilistic"
	got := wrapText(longWord, 5)
	if strings.Join(got, "") != longWord {
		t.Fatalf("oversized word changed while wrapping: %#v", got)
	}
	for _, line := range got {
		if ansi.StringWidth(line) > 5 {
			t.Fatalf("oversized word exceeded the available width: %q", line)
		}
	}
}

func TestWrapTextFillsLineBeforeMixedCJKText(t *testing.T) {
	got := wrapText("现在你这个[Image #1]序号也使用弱化色了", 24)
	if len(got) < 2 || !strings.Contains(got[0], "#1]") {
		t.Fatalf("mixed text left avoidable space: %#v", got)
	}
	for _, line := range got {
		if ansi.StringWidth(line) > 24 {
			t.Fatalf("mixed line exceeded width: %q", line)
		}
	}
}

func TestFitLinesTruncatesStyledTextWithoutBreakingANSI(t *testing.T) {
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render("selected text")
	got := fitLines([]string{styled}, 5, 1)
	if ansi.StringWidth(got) != 5 || ansi.Strip(got) != "selec" {
		t.Fatalf("truncated line = %q", got)
	}
	withoutCSI := csiPattern.ReplaceAllString(got, "")
	if strings.ContainsRune(withoutCSI, '\x1b') {
		t.Fatalf("truncated line contains a partial escape sequence: %q", got)
	}
}

func TestSanitizeRemovesTerminalControls(t *testing.T) {
	input := "safe\x1b[31mred\x1b[0m\x1b]0;fake-title\x07\nnext\tcolumn"
	got := sanitize(input)
	if strings.ContainsRune(got, '\x1b') || strings.Contains(got, "fake-title") {
		t.Fatalf("control sequence survived: %q", got)
	}
	if !strings.Contains(got, "safered\nnext    column") {
		t.Fatalf("visible text changed unexpectedly: %q", got)
	}
}

func TestDuplicatePromptsRemainSeparate(t *testing.T) {
	model := Model{width: 40, height: 12, noColor: true, snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{
		{ID: "one", Text: "duplicate"},
		{ID: "two", Text: "duplicate"},
	}}}
	output := model.render()
	if strings.Count(output, "duplicate") != 2 {
		t.Fatalf("duplicates were not preserved: %q", output)
	}
}

func TestLongPromptsExpandIndependently(t *testing.T) {
	model := Model{width: 80, height: 20, noColor: true, following: true, snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{
		{ID: "first", Text: numberedLines("first", 10)},
		{ID: "second", Text: numberedLines("second", 10)},
	}}}
	collapsed := strings.Join(model.bodyLines(), "\n")
	if strings.Count(collapsed, "+4 lines") != 2 || strings.Contains(collapsed, "second-10") {
		t.Fatalf("long prompts were not collapsed: %q", collapsed)
	}
	if view := model.render(); !strings.Contains(view, "Enter expand") {
		t.Fatalf("collapsed selection did not advertise expand: %q", view)
	}

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	expanded := strings.Join(model.bodyLines(), "\n")
	if !strings.Contains(expanded, "second-10") || strings.Contains(expanded, "first-10") || strings.Count(expanded, "+4 lines") != 1 {
		t.Fatalf("only the selected long prompt should expand: %q", expanded)
	}
	if view := model.render(); strings.Contains(view, "Enter fold") || strings.Contains(view, "c fold all") {
		t.Fatalf("footer retained expanded prompt actions: %q", view)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if body := strings.Join(model.bodyLines(), "\n"); !strings.Contains(body, "first-10") || !strings.Contains(body, "second-10") {
		t.Fatalf("each prompt did not retain its own expansion state: %q", body)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'c'})
	model = updated.(Model)
	if body := strings.Join(model.bodyLines(), "\n"); strings.Count(body, "+4 lines") != 2 {
		t.Fatalf("collapse all did not fold every long prompt: %q", body)
	}
}

func TestShortPromptIsNotFoldable(t *testing.T) {
	model := Model{width: 40, height: 12, noColor: true, snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{
		{ID: "short", Text: "first\nsecond"},
	}}}
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if updatedModel := updated.(Model); len(updatedModel.expanded) != 0 {
		t.Fatal("short prompt changed the fold state")
	} else if view := updatedModel.render(); strings.Contains(view, "Enter expand") || strings.Contains(view, "Enter fold") {
		t.Fatalf("short prompt advertised a fold action: %q", view)
	}
}

func TestArrowKeysSelectPromptsAndLatestRestoresFollowing(t *testing.T) {
	model := Model{width: 40, height: 12, noColor: true, following: true, snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{
		{ID: "one", Text: "first"},
		{ID: "two", Text: "second"},
		{ID: "three", Text: "third"},
	}}}
	if model.displaySelectedIndex() != 2 || !strings.Contains(strings.Join(model.bodyLines(), "\n"), "3 third") {
		t.Fatal("latest prompt was not selected by default")
	}

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model = updated.(Model)
	if model.selectedID != "two" || model.following || !strings.Contains(strings.Join(model.bodyLines(), "\n"), "2 second") {
		t.Fatalf("up did not select the previous prompt: selected=%q following=%v", model.selectedID, model.following)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	if model.selectedID != "three" || !model.following {
		t.Fatalf("down to latest did not restore following: selected=%q following=%v", model.selectedID, model.following)
	}
}

func TestHomeAndEndReplaceCaseSensitiveNavigation(t *testing.T) {
	model := Model{width: 40, height: 12, noColor: true, selectedID: "two", snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{
		{ID: "one", Text: "first"}, {ID: "two", Text: "second"}, {ID: "three", Text: "third"},
	}}}
	for _, key := range []rune{'g', 'G'} {
		updated, _ := model.Update(tea.KeyPressMsg{Code: key})
		model = updated.(Model)
		if model.selectedID != "two" {
			t.Fatalf("case-sensitive key %q still changed selection", key)
		}
	}

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	model = updated.(Model)
	if model.selectedID != "one" || model.following {
		t.Fatalf("Home did not select the first prompt: %#v", model)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	model = updated.(Model)
	if model.selectedID != "three" || !model.following {
		t.Fatalf("End did not restore latest following: %#v", model)
	}
}

func TestHelpOwnsShortcutsAndViewerRequiresCtrlXToQuit(t *testing.T) {
	model := Model{width: 48, height: 20, noColor: true, following: false, selectedID: "one", offset: 1,
		snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: "first"}, {ID: "two", Text: "second"}}},
	}
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'h'})
	model = updated.(Model)
	if !model.showHelp || !strings.Contains(model.render(), "Ctrl+X     Close viewer pane") || !strings.Contains(model.render(), "Enter      Expand or fold") || strings.Contains(model.render(), "DblClick") || !strings.Contains(model.render(), "Esc close") {
		t.Fatalf("help did not expose viewer shortcuts: %q", model.render())
	}
	if model.selectedID != "one" || model.offset != 1 || model.following {
		t.Fatalf("opening help changed prompt reading state: %#v", model)
	}

	updated, cmd := model.Update(tea.KeyPressMsg{Code: 'q'})
	model = updated.(Model)
	if cmd != nil || !model.showHelp {
		t.Fatal("single-key q closed help or quit the viewer")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.showHelp {
		t.Fatal("Escape did not close help")
	}
	updated, cmd = model.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("Ctrl+X did not request viewer exit")
	}
	if !model.CloseRequested() {
		t.Fatal("Ctrl+X quit without recording an explicit pane close request")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("Ctrl+X command returned %T, want tea.QuitMsg", cmd())
	}
}

func TestViewerErrorsNeverRequestPaneClosure(t *testing.T) {
	model := Model{width: 40, height: 10, snapshot: ipc.Snapshot{State: "ready"}}
	updated, cmd := model.Update(streamEndedMsg{})
	model = updated.(Model)
	if cmd != nil || model.snapshot.State != "error" || model.CloseRequested() {
		t.Fatalf("stream failure closed the pane or missed the error state: %#v", model)
	}
}

func TestCompactHelpScrollsWithoutChangingPromptReadingState(t *testing.T) {
	for _, size := range [][2]int{{20, 6}, {24, 10}} {
		model := Model{width: size[0], height: size[1], noColor: true, following: false, selectedID: "one", offset: 1,
			snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: "first\ncontinued"}, {ID: "two", Text: "second"}}},
		}
		updated, _ := model.Update(tea.KeyPressMsg{Code: 'h'})
		model = updated.(Model)
		if strings.Contains(model.render(), "dracula") {
			t.Fatalf("%dx%d help unexpectedly fit without scrolling", size[0], size[1])
		}

		sawTheme, sawPreview := false, false
		for range 20 {
			output := model.render()
			sawTheme = sawTheme || strings.Contains(output, "dracula")
			sawPreview = sawPreview || strings.Contains(output, "[ERROR]")
			if model.helpOffset == model.helpMaxOffset() {
				break
			}
			updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
			model = updated.(Model)
		}
		if model.helpOffset == 0 || !sawTheme || !sawPreview {
			t.Fatalf("%dx%d help did not reach themes and semantic preview: %q", size[0], size[1], model.render())
		}
		previousOffset := model.helpOffset
		updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
		model = updated.(Model)
		if model.helpOffset >= previousOffset {
			t.Fatalf("%dx%d mouse wheel did not scroll help upward", size[0], size[1])
		}
		if model.selectedID != "one" || model.offset != 1 || model.following {
			t.Fatalf("%dx%d help scrolling changed prompt state: %#v", size[0], size[1], model)
		}
		updated, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		model = updated.(Model)
		if model.helpOffset != model.helpMaxOffset() {
			t.Fatalf("%dx%d help offset was not clamped after resize: %#v", size[0], size[1], model)
		}

		updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		model = updated.(Model)
		if model.showHelp || model.helpOffset != 0 {
			t.Fatalf("%dx%d help did not close cleanly: %#v", size[0], size[1], model)
		}
	}
}

func TestMouseClickSelectsVisiblePromptWithoutJumpingViewport(t *testing.T) {
	model := Model{
		width: 40, height: 6, noColor: true, following: false, selectedID: "three", offset: 1,
		snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{
			{ID: "one", Text: "first\ncontinued"},
			{ID: "two", Text: "second"},
			{ID: "three", Text: "third"},
		}},
	}
	updated, _ := model.Update(tea.MouseClickMsg{X: 4, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.selectedID != "three" || !model.pendingClick {
		t.Fatalf("mouse press selected before release: %#v", model)
	}
	updated, _ = model.Update(tea.MouseReleaseMsg{X: 4, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.selectedID != "one" || model.following || model.offset != 1 {
		t.Fatalf("click selection moved the viewport: selected=%q following=%v offset=%d", model.selectedID, model.following, model.offset)
	}

	model = leftClick(model, 4, 2)
	if model.selectedID != "two" || model.offset != 1 {
		t.Fatalf("click did not map the visible prompt: selected=%q offset=%d", model.selectedID, model.offset)
	}

	separatorModel := model
	separatorModel.selectedID = "three"
	separatorModel = leftClick(separatorModel, 4, 3)
	if separatorModel.selectedID != "two" || separatorModel.following || separatorModel.offset != 1 {
		t.Fatalf("blank separator did not select its preceding prompt: %#v", separatorModel)
	}
}

func TestMouseClickLatestRestoresFollowingAndIgnoresFooter(t *testing.T) {
	model := Model{
		width: 40, height: 10, noColor: true, selectedID: "one", newCount: 2,
		snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{
			{ID: "one", Text: "first"}, {ID: "two", Text: "second"},
		}},
	}
	latest := model.layoutBody().prompts[1].start - model.offset
	model = leftClick(model, 4, latest)
	if model.selectedID != "two" || !model.following || model.newCount != 0 || model.offset != model.maxOffset() {
		t.Fatalf("latest click did not restore following: %#v", model)
	}

	footerModel := leftClick(model, 4, model.height-1)
	if footerModel.selectedID != "two" || !footerModel.following {
		t.Fatalf("footer click changed selection: %#v", footerModel)
	}
}

func TestMouseDragCopiesVisibleTextAndHighlightsSelection(t *testing.T) {
	model := Model{
		width: 40, height: 10, noColor: true,
		snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: "alpha beta"}}},
	}
	updated, _ := model.Update(tea.MouseClickMsg{X: 4, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseMotionMsg{X: 8, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, command := model.Update(tea.MouseReleaseMsg{X: 8, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	assertSelectionCommand(t, command, "alpha")
	if model.selectedID != "" {
		t.Fatalf("text drag changed prompt selection: %#v", model)
	}
	output := model.render()
	if !strings.Contains(output, "\x1b[7m") || !strings.Contains(ansi.Strip(output), "1 alpha beta") {
		t.Fatalf("drag selection was not visibly highlighted: %q", output)
	}
}

func TestMouseDragUsesExplicitColorsForWideSelection(t *testing.T) {
	model := Model{
		width: 40, height: 10,
		snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: "只回复 ok"}}},
	}
	updated, _ := model.Update(tea.MouseClickMsg{X: 4, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseMotionMsg{X: 9, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)

	output := model.render()
	palette := theme.Resolve(theme.Mocha, false)
	expected := lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Sapphire)).Background(lipgloss.Color(palette.Cell)).Render("只回复")
	if !strings.Contains(output, expected) || strings.Contains(output, "\x1b[7m") {
		t.Fatalf("wide selection did not use explicit colors: %q", output)
	}
}

func TestHelpPreviewsAndCancelsThemeSelection(t *testing.T) {
	model := Model{width: 48, height: 20, noColor: true, themeName: theme.Mocha, themeSource: config.ThemeConfig}
	model.applyTheme(theme.Mocha)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'h'})
	model = updated.(Model)
	if !model.showHelp || !strings.Contains(model.render(), "mocha") || !strings.Contains(model.render(), "●●●●●●") {
		t.Fatalf("help did not embed theme palettes: %q", model.render())
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	if model.themeName != theme.Latte || model.colors.Success != "#40a02b" {
		t.Fatalf("theme preview = %q %#v", model.themeName, model.colors)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.showHelp || model.themeName != theme.Mocha {
		t.Fatalf("theme cancel did not restore original: %#v", model)
	}
}

func TestHelpResolvesAutoWithoutListingIt(t *testing.T) {
	model := Model{width: 48, height: 20, noColor: true, themeName: theme.Auto, themeSource: config.ThemeDefault}
	model.applyTheme(theme.Auto)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'h'})
	model = updated.(Model)
	output := strings.Join(model.helpLines(), "\n")
	if strings.Contains(output, " auto") || model.themeName != theme.Mocha || !strings.Contains(output, "› mocha") {
		t.Fatalf("auto was not resolved to an explicit dark theme: name=%q output=%q", model.themeName, output)
	}
}

func TestHelpThemeColumnsAndSectionColors(t *testing.T) {
	model := Model{width: 80, height: 24, themeName: theme.Mocha, themeSource: config.ThemeConfig, snapshot: ipc.Snapshot{State: "live"}}
	model.applyTheme(theme.Dracula)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'h'})
	model = updated.(Model)
	lines := model.helpLines()

	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Resolve(theme.Dracula, false).Sapphire))
	if !slices.Contains(lines, accent.Render(" Help")) || !slices.Contains(lines, accent.Render(" Theme")) {
		t.Fatalf("help section titles did not use the theme accent: %q", lines)
	}
	for _, heading := range []string{" Viewer", " Navigate", " Prompt"} {
		if !slices.Contains(lines, accent.Render(heading)) {
			t.Fatalf("help missed grouped heading %q: %q", heading, lines)
		}
	}
	if !slices.Contains(lines, helpEntry("Ctrl+X", "Close viewer pane")) {
		t.Fatalf("help body did not use the normal foreground: %q", lines)
	}
	if output := strings.Join(lines, "\n"); strings.Contains(output, "preview · Enter save") {
		t.Fatalf("help body repeated fixed footer actions: %q", output)
	}

	nameColumn, swatchColumn := -1, -1
	for _, name := range theme.SelectableNames() {
		for _, line := range lines {
			plain := ansi.Strip(line)
			nameIndex := strings.Index(plain, name)
			swatchIndex := strings.Index(plain, "●")
			if nameIndex < 0 || swatchIndex < 0 {
				continue
			}
			gotNameColumn := ansi.StringWidth(plain[:nameIndex])
			gotSwatchColumn := ansi.StringWidth(plain[:swatchIndex])
			if nameColumn < 0 {
				nameColumn, swatchColumn = gotNameColumn, gotSwatchColumn
			}
			if gotNameColumn != nameColumn || gotSwatchColumn != swatchColumn {
				t.Fatalf("theme columns are not aligned: name=%s line=%q nameColumn=%d swatchColumn=%d", name, plain, gotNameColumn, gotSwatchColumn)
			}
			break
		}
	}
}

func TestHelpSemanticPreviewUsesEveryThemeRole(t *testing.T) {
	for _, name := range theme.Names()[1:] {
		model := Model{width: 80, height: 24, themeName: name, themeSource: config.ThemeConfig, snapshot: ipc.Snapshot{State: "live"}}
		model.applyTheme(name)
		output := strings.Join(model.helpLines(), "\n")
		roles := theme.Derive(theme.Resolve(name, false))
		styled := func(color, text string) string {
			return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(text)
		}
		want := []string{
			styled(roles.Accent, " Theme preview"),
			styled(roles.Success, "[LIVE]"),
			"1 Other prompt",
			styled(roles.Accent, "2 Selected prompt"),
			styled(roles.Accent, "h help"),
			styled(roles.Token, "Total: 2.4M"),
			styled(roles.Model, "Model: gpt-5.6"),
			styled(roles.Label, "Limit:"),
			styled(roles.Warning, "66%"),
			styled(roles.Branch, "main"),
			styled(roles.Added, "+12"),
			styled(roles.Deleted, "-3"),
			styled(roles.Untracked, "?1"),
			styled(roles.Muted, "Muted text"),
			styled(roles.Error, "[ERROR]"),
		}
		for _, expected := range want {
			if !strings.Contains(output, expected) {
				t.Fatalf("theme=%s preview missed semantic sample %q: %q", name, ansi.Strip(expected), output)
			}
		}
		if lines := model.themePreviewLines(); len(lines) != 3 {
			t.Fatalf("theme=%s preview did not keep three semantic rows: %q", name, lines)
		}
	}
}

func TestHelpSavesThemeSelection(t *testing.T) {
	t.Setenv(paths.EnvHome, t.TempDir())
	model := Model{width: 48, height: 20, noColor: true, themeName: theme.Mocha, themeSource: config.ThemeConfig}
	model.applyTheme(theme.Mocha)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'h'})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("theme save command is nil")
	}
	updated, _ = model.Update(command())
	model = updated.(Model)
	name, source, err := config.LoadTheme()
	if err != nil || name != theme.Latte || source != config.ThemeConfig || !model.showHelp || model.themeMessage != "Theme saved" {
		t.Fatalf("saved theme = %q, source = %q, help = %v, message = %q, err = %v", name, source, model.showHelp, model.themeMessage, err)
	}
}

func TestStatusLineUsesThemeRolesAndFitsWidth(t *testing.T) {
	model := Model{width: 48, height: 12, snapshot: ipc.Snapshot{State: "live", Metrics: &provider.SessionMetrics{
		Project: "prompt-pane", Branch: "main", Model: "gpt-5", TotalTokens: 12500, ContextUsedPercent: 42,
		FiveHour: &provider.QuotaWindow{UsedPercent: 75}, SevenDay: &provider.QuotaWindow{UsedPercent: 92},
	}}}
	model.applyTheme(theme.Dracula)
	lines := model.renderStatusBlock(4)
	output := strings.Join(lines, "\n")
	for _, line := range lines {
		if ansi.StringWidth(line) > model.width {
			t.Fatalf("status line exceeded width: %q", lines)
		}
	}
	if !strings.Contains(output, "\x1b[38;2;255;85;85m") || !strings.Contains(output, "█") || !strings.Contains(output, "░") || strings.Contains(output, "\x1b[48;2;") || strings.Contains(output, "prompt-pane") {
		t.Fatalf("status lines do not fit or use Dracula danger color: %q", lines)
	}
}

func TestStatusWaitsForFirstMetricsUpdate(t *testing.T) {
	for _, state := range []string{"ready", "live"} {
		model := Model{width: 48, height: 12, noColor: true, snapshot: ipc.Snapshot{State: state}}
		output := model.render()
		if !strings.Contains(output, "Metrics available after first response") || strings.Contains(output, "waiting for Codex response") {
			t.Fatalf("state=%s hid pending metrics: %q", state, output)
		}
		for _, line := range strings.Split(output, "\n") {
			if ansi.StringWidth(line) > model.width {
				t.Fatalf("state=%s pending metrics exceeded width: %q", state, line)
			}
		}
	}
	model := Model{width: 80, height: 12, noColor: true, snapshot: ipc.Snapshot{State: "live", Metrics: &provider.SessionMetrics{}}}
	output := strings.Join(model.renderStatusBlock(4), "\n")
	for _, expected := range []string{"Total: --", "5h: --", "7d: --", "Ctx: --"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("known metrics update hid unknown field %q: %q", expected, output)
		}
	}
}

func TestProgressBarUsesForegroundFillAndDottedRemainder(t *testing.T) {
	plain := Model{noColor: true}
	if got, want := plain.renderPercent(66, 10), "███████░░░ 66%"; got != want {
		t.Fatalf("plain progress = %q, want %q", got, want)
	}

	colored := Model{}
	colored.applyTheme(theme.Mocha)
	output := colored.renderPercent(66, 10)
	if !strings.Contains(ansi.Strip(output), "███████░░░") || !strings.Contains(output, "\x1b[38;2;") || strings.Contains(output, "\x1b[48;2;") {
		t.Fatalf("colored progress did not use foreground glyphs: %q", output)
	}
}

func TestDefaultStatusKeepsTokenTrackerBarsAndWideStatusCompresses(t *testing.T) {
	metrics := &provider.SessionMetrics{
		Project: "prompt-pane", Branch: "main", Model: "gpt-5", TotalTokens: 23000,
		ContextWindow: 258000, ContextUsedPercent: 9,
		FiveHour: &provider.QuotaWindow{UsedPercent: 21}, SevenDay: &provider.QuotaWindow{UsedPercent: 44},
	}
	narrow := Model{width: 48, height: 20, snapshot: ipc.Snapshot{State: "live", Metrics: metrics}}
	narrow.applyTheme(theme.Mocha)
	narrowLines := narrow.renderStatusBlock(4)
	narrowOutput := strings.Join(narrowLines, "\n")
	plain := ansi.Strip(narrowOutput)
	if !strings.Contains(plain, "[LIVE] (main)") || strings.Contains(plain, "prompt-pane") || !strings.Contains(plain, "█") || !strings.Contains(plain, "░") || strings.Contains(narrowOutput, "\x1b[48;2;") {
		t.Fatalf("default status lost branch placement or Token Tracker bars: %q", narrowLines)
	}
	for _, width := range []int{24, 32, 48} {
		model := narrow
		model.width = width
		output := strings.Join(model.renderStatusBlock(4), "\n")
		plain := ansi.Strip(output)
		if !strings.Contains(plain, "█") || !strings.Contains(plain, "░") || strings.Contains(output, "\x1b[48;2;") {
			t.Fatalf("width=%d dropped progress bars too early: %q", width, output)
		}
	}

	wide := narrow
	wide.width = 80
	wideLines := wide.renderStatusBlock(4)
	if len(wideLines) >= len(narrowLines) {
		t.Fatalf("wide status did not compress rows: narrow=%q wide=%q", narrowLines, wideLines)
	}
}

func TestStatusUsesSemanticRowsAndElasticBars(t *testing.T) {
	metrics := &provider.SessionMetrics{
		Branch: "main", TotalTokens: 129000, ContextWindow: 258000, ContextUsedPercent: 20,
		SevenDay: &provider.QuotaWindow{UsedPercent: 68, ResetsAt: time.Now().Add(3*time.Hour + time.Minute).Unix()},
	}
	for _, width := range []int{24, 32, 48, 56, 80} {
		model := Model{width: width, height: 20, noColor: true, snapshot: ipc.Snapshot{State: "live", Metrics: metrics}}
		lines := model.renderStatusBlock(4)
		output := strings.Join(lines, "\n")
		for _, line := range lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width=%d status line overflowed: %q", width, line)
			}
		}
		if !strings.Contains(output, "Limit: 5h: --") || !strings.Contains(output, "7d:") || !strings.Contains(output, "Ctx: 258k") || strings.Contains(output, "258k Ctx:") {
			t.Fatalf("width=%d status lost label-first semantic groups: %q", width, output)
		}
		if strings.Contains(output, "5h: -- | 7d:") {
			t.Fatalf("width=%d split the limit group with a divider: %q", width, output)
		}
		if !strings.Contains(output, "█") || !strings.Contains(output, "░") {
			t.Fatalf("width=%d status lost progress bars: %q", width, output)
		}
	}

	medium := Model{width: 56, height: 20, noColor: true, snapshot: ipc.Snapshot{State: "live", Metrics: metrics}}
	mediumLines := medium.renderStatusBlock(4)
	if len(mediumLines) != 3 || !strings.Contains(mediumLines[1], "Limit:") || !strings.Contains(mediumLines[2], "Ctx:") {
		t.Fatalf("medium status did not use limit/context rows: %q", mediumLines)
	}
	for _, line := range mediumLines[1:] {
		if width := ansi.StringWidth(line); width < medium.width-1 {
			t.Fatalf("medium elastic row left avoidable space: width=%d row=%q", width, line)
		}
	}

	wide := medium
	wide.width = 80
	if lines := wide.renderStatusBlock(4); len(lines) != 2 || !strings.Contains(lines[1], " | Ctx:") {
		t.Fatalf("wide status did not combine semantic groups: %q", lines)
	}
}

func TestEnvironmentThemeCanPreviewButNotSave(t *testing.T) {
	model := Model{width: 48, height: 20, noColor: true, themeName: theme.Mocha, themeSource: config.ThemeEnvironment}
	model.applyTheme(theme.Mocha)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'h'})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command != nil || model.themeMessage != theme.Environment+" is active" {
		t.Fatalf("environment theme was saveable or missed its source: %#v", model)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.showHelp || model.themeName != theme.Mocha {
		t.Fatalf("environment preview was not canceled: %#v", model)
	}
}

func TestEveryThemeColorsStatusIndexSelectionAndHelpAction(t *testing.T) {
	for _, name := range theme.Names()[1:] {
		model := Model{width: 48, height: 12, selectedID: "two", snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: "first"}, {ID: "two", Text: "second"}}}}
		model.applyTheme(name)
		output := model.render()
		palette := theme.Resolve(name, false)
		state := lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Green)).Render("[LIVE]")
		accent := lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Sapphire))
		if !strings.Contains(output, state) || !strings.Contains(output, "  1 first") || !strings.Contains(output, accent.Render("  2 second")) || !strings.Contains(output, accent.Render("h help")) {
			t.Fatalf("theme=%s roles were not shared by state, index, selection and help action: %q", name, output)
		}
	}
}

func TestMouseDragCopiesUnicodeAcrossVisibleLines(t *testing.T) {
	model := Model{
		width: 40, height: 10, noColor: true,
		snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: "中文\nsecond"}}},
	}
	updated, _ := model.Update(tea.MouseClickMsg{X: 4, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseMotionMsg{X: 6, Y: 1, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, command := model.Update(tea.MouseReleaseMsg{X: 6, Y: 1, Button: tea.MouseLeft})
	model = updated.(Model)
	assertSelectionCommand(t, command, "中文\n    sec")
	for _, line := range strings.Split(model.render(), "\n") {
		if ansi.StringWidth(line) > model.width {
			t.Fatalf("selection highlight exceeded viewer width: %q", line)
		}
	}
}

func TestMouseDragSnapsWideCharacterSelectionToGraphemeBounds(t *testing.T) {
	for _, test := range []struct {
		name       string
		start, end int
	}{
		{name: "forward", start: 5, end: 8},
		{name: "backward", start: 8, end: 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := Model{
				width: 40, height: 10, noColor: true,
				snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: "你修复"}}},
			}
			updated, _ := model.Update(tea.MouseClickMsg{X: test.start, Y: 0, Button: tea.MouseLeft})
			model = updated.(Model)
			updated, _ = model.Update(tea.MouseMotionMsg{X: test.end, Y: 0, Button: tea.MouseLeft})
			model = updated.(Model)
			updated, command := model.Update(tea.MouseReleaseMsg{X: test.end, Y: 0, Button: tea.MouseLeft})
			model = updated.(Model)
			assertSelectionCommand(t, command, "你修复")
			output := model.render()
			if !strings.Contains(output, "\x1b[7m你修复") || !strings.Contains(ansi.Strip(output), "1 你修复") {
				t.Fatalf("wide-character selection was split: %q", output)
			}
		})
	}
}

func TestMouseDragTracksWideCharactersWithoutForcingScreenClear(t *testing.T) {
	model := Model{
		width: 40, height: 10, noColor: true,
		snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: "我觉得刷新就不需要"}}},
	}
	updated, _ := model.Update(tea.MouseClickMsg{X: 10, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, command := model.Update(tea.MouseMotionMsg{X: 21, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	if command != nil {
		t.Fatalf("wide-character drag command = %v, want renderer-managed update", command)
	}
	if selected := model.selectedText(); selected != "刷新就不需要" {
		t.Fatalf("wide-character drag selection = %q", selected)
	}
}

func TestSnapTextRangePreservesEmojiAndCombiningGraphemes(t *testing.T) {
	line := "A👨‍👩‍👧‍👦e\u0301B"
	left, right := snapTextRange(line, 2, 3)
	if selected := ansi.Cut(line, left, right); selected != "👨‍👩‍👧‍👦" {
		t.Fatalf("emoji selection = %q at [%d,%d)", selected, left, right)
	}
	left, right = snapTextRange(line, 3, 4)
	if selected := ansi.Cut(line, left, right); selected != "e\u0301" {
		t.Fatalf("combining grapheme selection = %q at [%d,%d)", selected, left, right)
	}
}

func TestMouseReleaseWithoutDragDoesNotCopy(t *testing.T) {
	model := Model{
		width: 40, height: 10, noColor: true,
		snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: "alpha"}}},
	}
	updated, _ := model.Update(tea.MouseClickMsg{X: 4, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, command := model.Update(tea.MouseReleaseMsg{X: 4, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	if command != nil || model.textSelected || model.selecting {
		t.Fatalf("single click created a clipboard selection: %#v", model)
	}
	if model.selectedID != "one" {
		t.Fatalf("single click no longer selected its prompt: %#v", model)
	}
}

func TestMouseDragCopiesHelpText(t *testing.T) {
	model := Model{
		width: 40, height: 12, noColor: true, showHelp: true,
		snapshot: ipc.Snapshot{State: "live"},
	}
	updated, _ := model.Update(tea.MouseClickMsg{X: 1, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseMotionMsg{X: 4, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	_, command := model.Update(tea.MouseReleaseMsg{X: 4, Y: 0, Button: tea.MouseLeft})
	assertSelectionCommand(t, command, "Help")
}

func TestMouseDragRejectsFooterAndCancelsPendingClick(t *testing.T) {
	model := Model{
		width: 40, height: 10, noColor: true,
		snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "long", Text: numberedLines("line", 10)}}},
	}
	updated, _ := model.Update(tea.MouseClickMsg{X: 4, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseMotionMsg{X: 5, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, command := model.Update(tea.MouseReleaseMsg{X: 5, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	if command == nil || model.pendingClick {
		t.Fatalf("drag did not cancel the pending click: %#v", model)
	}

	updated, _ = model.Update(tea.MouseClickMsg{X: 4, Y: model.height - 1, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, command = model.Update(tea.MouseReleaseMsg{X: 8, Y: model.height - 1, Button: tea.MouseLeft})
	model = updated.(Model)
	if command != nil || model.textSelected || model.selecting {
		t.Fatalf("footer drag reached the clipboard: %#v", model)
	}
}

func TestRepeatedMouseClickOnlySelectsAndEnterTogglesLongPrompt(t *testing.T) {
	model := Model{
		width: 80, height: 20, noColor: true, following: false, selectedID: "short",
		snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{
			{ID: "long", Text: numberedLines("line", 10)},
			{ID: "short", Text: "short"},
		}},
	}
	row := model.layoutBody().prompts[0].start
	model = leftClick(model, 4, row)
	model = leftClick(model, 4, row)
	if model.selectedID != "long" || model.expanded["long"] || strings.Contains(strings.Join(model.bodyLines(), "\n"), "line-10") {
		t.Fatalf("repeated click changed fold state: %#v", model)
	}

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if !model.expanded["long"] || !strings.Contains(strings.Join(model.bodyLines(), "\n"), "line-10") {
		t.Fatalf("Enter did not expand the selected long prompt: %#v", model)
	}
}

func TestSelectedPromptUsesEmphasisWithoutMarker(t *testing.T) {
	model := Model{width: 40, height: 12, selectedID: "two", snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{
		{ID: "one", Text: "first"}, {ID: "two", Text: "second\ncontinued"},
	}}}
	body := strings.Join(model.bodyLines(), "\n")
	if strings.Contains(body, ">") {
		t.Fatalf("selection marker remained in body: %q", body)
	}
	if !strings.Contains(body, "\x1b[") || !strings.Contains(body, "2 second") || !strings.Contains(body, "   continued") || strings.Contains(body, "│") {
		t.Fatalf("selected prompt block was not emphasized: %q", body)
	}
}

func TestPromptNumberColumnHasSymmetricSpacing(t *testing.T) {
	model := Model{width: 40, height: 12, noColor: true, snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{
		{ID: "one", Text: "first"}, {ID: "two", Text: "second\ncontinued"},
	}}}
	want := []string{"  1 first", "", "  2 second", "    continued"}
	got := model.bodyLines()
	for index := range got {
		got[index] = ansi.Strip(got[index])
	}
	if !slices.Equal(got, want) {
		t.Fatalf("prompt spacing = %#v, want %#v", got, want)
	}
}

func TestPromptNumberColumnDoesNotShiftAtTenPrompts(t *testing.T) {
	for _, count := range []int{9, 10} {
		prompts := make([]provider.UserPrompt, count)
		for index := range prompts {
			prompts[index] = provider.UserPrompt{ID: fmt.Sprintf("prompt-%d", index), Text: "text"}
		}
		model := Model{width: 40, height: 20, noColor: true, snapshot: ipc.Snapshot{State: "live", Prompts: prompts}}
		first := ansi.Strip(model.bodyLines()[0])
		if first != "  1 text" {
			t.Fatalf("count=%d first prompt shifted: %q", count, first)
		}
	}
}

func TestFoldSummaryIsContextualAndResponsive(t *testing.T) {
	for _, width := range []int{20, 24} {
		model := Model{width: width, height: 10, noColor: true, selectedID: "two", snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{
			{ID: "one", Text: numberedLines("first", 10)},
			{ID: "two", Text: numberedLines("second", 10)},
		}}}
		body := strings.Join(model.bodyLines(), "\n")
		if strings.Count(body, "Enter") != 1 || !strings.Contains(body, "… +4 · Enter") || strings.Count(body, "+4") != 2 {
			t.Fatalf("width=%d fold summaries were not contextual: %q", width, body)
		}
		for _, line := range strings.Split(body, "\n") {
			if ansi.StringWidth(line) > model.width {
				t.Fatalf("width=%d fold summary exceeded width: %q", width, line)
			}
		}
	}
}

func TestNewPromptDoesNotMovePausedSelection(t *testing.T) {
	model := Model{
		width: 40, height: 12, noColor: true, selectedID: "one",
		snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: "first"}, {ID: "two", Text: "second"}}},
	}
	updated, _ := model.Update(snapshotMsg{snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{
		{ID: "one", Text: "first"}, {ID: "two", Text: "second"}, {ID: "three", Text: "third"},
	}}})
	model = updated.(Model)
	if model.selectedID != "one" || model.newCount != 1 || model.following {
		t.Fatalf("new prompt moved paused selection: selected=%q new=%d following=%v", model.selectedID, model.newCount, model.following)
	}
}

func TestScrollingMovesOnlyTheViewport(t *testing.T) {
	prompts := make([]provider.UserPrompt, 8)
	for index := range prompts {
		prompts[index] = provider.UserPrompt{ID: fmt.Sprintf("prompt-%d", index), Text: fmt.Sprintf("prompt-%d", index)}
	}
	model := Model{
		width: 40, height: 8, noColor: true, following: true, selectedID: "prompt-7",
		snapshot: ipc.Snapshot{State: "live", Prompts: prompts},
	}
	model.offset = model.maxOffset()

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	model = updated.(Model)
	if model.offset == model.maxOffset() || model.selectedID != "prompt-7" || model.following {
		t.Fatalf("PgUp changed selection or failed to pause following: %#v", model)
	}
	pausedOffset := model.offset

	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	model = updated.(Model)
	if model.offset >= pausedOffset || model.selectedID != "prompt-7" || model.following {
		t.Fatalf("mouse wheel changed selection or failed to move the viewport: %#v", model)
	}

	model.scroll(1000)
	if model.offset != model.maxOffset() || model.selectedID != "prompt-7" || model.following {
		t.Fatalf("scrolling to the bottom changed selection or resumed following: %#v", model)
	}
}

func TestNewPromptDoesNotRevealOffscreenSelection(t *testing.T) {
	prompts := make([]provider.UserPrompt, 8)
	for index := range prompts {
		prompts[index] = provider.UserPrompt{ID: fmt.Sprintf("prompt-%d", index), Text: fmt.Sprintf("prompt-%d", index)}
	}
	model := Model{
		width: 40, height: 8, noColor: true, selectedID: "prompt-7", offset: 0,
		snapshot: ipc.Snapshot{State: "live", Prompts: prompts},
	}
	updated, _ := model.Update(snapshotMsg{snapshot: ipc.Snapshot{
		State: "live", Prompts: append(prompts, provider.UserPrompt{ID: "prompt-8", Text: "prompt-8"}),
	}})
	model = updated.(Model)
	if model.offset != 0 || model.selectedID != "prompt-7" || model.newCount != 1 || model.following {
		t.Fatalf("new prompt disturbed an offscreen selection: %#v", model)
	}
}

func TestViewerPagesStartAtTop(t *testing.T) {
	model := Model{width: 40, height: 10, noColor: true, snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: "first"}}}}
	lines := strings.Split(model.render(), "\n")
	if !strings.Contains(lines[0], "1 first") {
		t.Fatalf("prompt page did not start at the top: %q", model.render())
	}

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'h'})
	model = updated.(Model)
	lines = strings.Split(model.render(), "\n")
	if strings.TrimSpace(lines[0]) != "Help" || !strings.Contains(model.render(), "PgUp/PgDn  Scroll page") {
		t.Fatalf("help page did not follow the shared grid: %q", model.render())
	}

	empty := Model{width: 40, height: 20, noColor: true, snapshot: ipc.Snapshot{State: "ready"}}
	lines = strings.Split(empty.render(), "\n")
	if strings.TrimSpace(lines[0]) != "Waiting for your first prompt" {
		t.Fatalf("empty state did not start at the top: %q", empty.render())
	}
}

func TestSelectionAndExpansionSurviveResizeAndIncrementalSnapshot(t *testing.T) {
	model := Model{
		width: 40, height: 20, noColor: true, selectedID: "one",
		expanded: map[string]bool{"one": true},
		snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{
			{ID: "one", Text: numberedLines("first", 10)},
			{ID: "two", Text: "second"},
		}},
	}
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 32, Height: 12})
	model = updated.(Model)
	updated, _ = model.Update(snapshotMsg{snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{
		{ID: "one", Text: numberedLines("first", 10)},
		{ID: "two", Text: "second"},
		{ID: "three", Text: "third"},
	}}})
	model = updated.(Model)
	if model.selectedID != "one" || !model.expanded["one"] {
		t.Fatalf("selection or expansion was lost: selected=%q expanded=%v", model.selectedID, model.expanded["one"])
	}
	if body := strings.Join(model.bodyLines(), "\n"); !strings.Contains(body, "first-10") || !strings.Contains(body, "1 first-01") {
		t.Fatalf("restored selection was not rendered expanded: %q", body)
	}
}

func TestEmptySnapshotResetsSessionViewState(t *testing.T) {
	model := Model{
		width: 40, height: 20, noColor: true, selectedID: "one", newCount: 2, offset: 3,
		expanded: map[string]bool{"one": true},
		snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: numberedLines("first", 10)}}},
	}
	updated, _ := model.Update(snapshotMsg{snapshot: ipc.Snapshot{
		State:  "live",
		Notice: "Session resumed. Showing new prompts only.",
	}})
	model = updated.(Model)
	if model.selectedID != "" || len(model.expanded) != 0 || model.newCount != 0 || model.offset != 0 || !model.following {
		t.Fatalf("session view state was not reset: %#v", model)
	}
	if output := model.render(); !strings.Contains(output, "Session resumed") || !strings.Contains(output, "new prompts") {
		t.Fatalf("session reset notice was not rendered: %q", output)
	}
}

func numberedLines(prefix string, count int) string {
	lines := make([]string, count)
	for index := range lines {
		lines[index] = fmt.Sprintf("%s-%02d", prefix, index+1)
	}
	return strings.Join(lines, "\n")
}

func TestStateLabelsMatchTheUIContract(t *testing.T) {
	want := map[string]string{
		"ready": "READY", "live": "LIVE", "ended": "ENDED", "error": "ERROR",
	}
	for state, label := range want {
		if got := stateLabel(state); got != label {
			t.Fatalf("stateLabel(%q) = %q, want %q", state, got, label)
		}
	}
	model := Model{width: 20, height: 6, noColor: true, snapshot: ipc.Snapshot{State: "error"}}
	if output := model.render(); !strings.Contains(output, "[ERROR]") {
		t.Fatalf("compact footer hid state: %q", output)
	}
}

func TestExtremeNarrowFooterHidesActions(t *testing.T) {
	for _, noColor := range []bool{false, true} {
		for width := 1; width < 20; width++ {
			model := Model{width: width, height: 6, noColor: noColor, snapshot: ipc.Snapshot{State: "live"}}
			output := model.render()
			if strings.Contains(output, "h help") || strings.Contains(output, "latest") {
				t.Fatalf("color=%v width=%d narrow footer = %q", !noColor, width, output)
			}
			if width >= 9 && !strings.Contains(output, "[LIVE]") {
				t.Fatalf("color=%v width=%d hid live state: %q", !noColor, width, output)
			}
			for _, line := range strings.Split(output, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatalf("width=%d narrow line exceeds width: %q", width, line)
				}
				withoutCSI := csiPattern.ReplaceAllString(line, "")
				if strings.ContainsRune(withoutCSI, '\x1b') {
					t.Fatalf("width=%d narrow line contains a partial escape: %q", width, line)
				}
			}
		}
	}
}

func TestNoticeStylesFollowStateSeverity(t *testing.T) {
	errorModel := Model{width: 40, height: 10, snapshot: ipc.Snapshot{State: "error"}}
	errorBody := strings.Join(errorModel.bodyLines(), "\n")
	if !strings.Contains(errorBody, "\x1b[") || strings.Contains(errorBody, "\x1b[2m") || !strings.Contains(ansi.Strip(errorBody), "Prompt stream unavailable") {
		t.Fatalf("error notice was not emphasized: %q", errorBody)
	}

	liveModel := Model{width: 40, height: 10, snapshot: ipc.Snapshot{State: "live", Notice: "Session resumed. Showing new prompts only."}}
	if liveBody := strings.Join(liveModel.bodyLines(), "\n"); strings.Contains(liveBody, "\x1b[") {
		t.Fatalf("live boundary notice was unexpectedly muted: %q", liveBody)
	}

	readyModel := Model{width: 40, height: 10, snapshot: ipc.Snapshot{State: "ready"}}
	if readyBody := strings.Join(readyModel.bodyLines(), "\n"); !strings.Contains(ansi.Strip(readyBody), "Waiting for your first prompt") {
		t.Fatalf("ready body did not explain the empty session: %q", readyBody)
	}
}

func TestViewerHasNoBrandHeaderAndFooterOwnsStatus(t *testing.T) {
	model := Model{width: 40, height: 12, noColor: true, snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: "first"}}}}
	output := model.render()
	if strings.Contains(output, "Prompt Pane") || !strings.Contains(output, "[LIVE]") || strings.Contains(output, "1 [LIVE]") || !strings.Contains(output, "h help") {
		t.Fatalf("viewer chrome was not lightweight: %q", output)
	}
	if !strings.Contains(output, "Metrics available after first response") {
		t.Fatalf("status area did not explain pending metrics: %q", output)
	}
}

func TestFooterShowsOnlyStatusUntilNewPromptsNeedAction(t *testing.T) {
	model := Model{width: 48, height: 12, noColor: true, snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: "first"}, {ID: "two", Text: "second"}}}}
	footer := model.renderFooter(false)
	if !strings.Contains(footer, "[LIVE]") || !strings.Contains(footer, "h help") || strings.Contains(footer, "2 [LIVE]") {
		t.Fatalf("ordinary footer contains redundant prompt count or misses help: %q", footer)
	}
	model.newCount = 3
	footer = model.renderFooter(false)
	if !strings.Contains(footer, "3 new · End latest") || strings.Contains(footer, "h help") {
		t.Fatalf("paused footer did not prioritize the latest action: %q", footer)
	}
}

func TestReadyStateRoutesTroubleshootingThroughHelp(t *testing.T) {
	model := Model{width: 48, height: 20, noColor: true, snapshot: ipc.Snapshot{State: "ready"}}
	if output := model.render(); strings.Contains(output, "/hooks") || !strings.Contains(output, "h troubleshoot") {
		t.Fatalf("ready view did not keep troubleshooting in help: %q", output)
	}

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'h'})
	model = updated.(Model)
	if output := model.render(); !strings.Contains(output, "Help") || !strings.Contains(output, "Connection") || !strings.Contains(output, "Hook confirmation starts with the first prompt") || !strings.Contains(output, "/hooks") || !strings.Contains(output, "Restart codex.pp") {
		t.Fatalf("ready help did not explain how to confirm the connection: %q", output)
	}

	updated, _ = model.Update(snapshotMsg{snapshot: ipc.Snapshot{State: "live"}})
	model = updated.(Model)
	model.showHelp = false
	if output := model.render(); !strings.Contains(output, "[LIVE]") || !strings.Contains(output, "h help") || strings.Contains(output, "/hooks") || !strings.Contains(output, "Waiting for your first prompt") {
		t.Fatalf("live empty state retained ready diagnostics: %q", output)
	}
}

func TestReadyTroubleshootingIsResponsive(t *testing.T) {
	for _, size := range [][2]int{{20, 6}, {24, 10}, {32, 12}, {48, 20}, {80, 24}} {
		model := Model{width: size[0], height: size[1], noColor: true, snapshot: ipc.Snapshot{State: "ready"}}
		footer := model.renderFooter(false)
		if size[0] < 32 && !strings.Contains(footer, "h help") {
			t.Fatalf("%dx%d ready footer hid compact help: %q", size[0], size[1], footer)
		}
		if size[0] >= 32 && !strings.Contains(footer, "h troubleshoot") {
			t.Fatalf("%dx%d ready footer hid troubleshooting: %q", size[0], size[1], footer)
		}

		updated, _ := model.Update(tea.KeyPressMsg{Code: 'h'})
		model = updated.(Model)
		foundHooks := false
		for {
			output := model.render()
			foundHooks = foundHooks || strings.Contains(output, "/hooks")
			for _, line := range strings.Split(output, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("%dx%d troubleshooting line exceeded width: %q", size[0], size[1], line)
				}
			}
			previous := model.helpOffset
			model.scrollHelp(model.bodyHeight())
			if model.helpOffset == previous {
				break
			}
		}
		if !foundHooks {
			t.Fatalf("%dx%d troubleshooting never exposed /hooks", size[0], size[1])
		}
	}
}
