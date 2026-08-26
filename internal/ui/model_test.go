package ui

import (
	"fmt"
	"image/color"
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
	appversion "github.com/Natsume-kkk/prompt-pane/internal/version"
	"github.com/Natsume-kkk/prompt-pane/internal/zellij"
)

func leftClick(model Model, x, y int) Model {
	updated, _ := model.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseNone})
	return updated.(Model)
}

func openSettingsPage(model Model) Model {
	updated, _ := model.Update(tea.KeyPressMsg{Code: 's'})
	return updated.(Model)
}

func openThemePage(model Model) Model {
	model = openSettingsPage(model)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return updated.(Model)
}

func openHelpPage(model Model) Model {
	model = openSettingsPage(model)
	for range 2 {
		updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		model = updated.(Model)
	}
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return updated.(Model)
}

func openAboutPage(model Model) Model {
	model = openSettingsPage(model)
	for range 3 {
		updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		model = updated.(Model)
	}
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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

func TestNewUsesConfiguredChineseInterface(t *testing.T) {
	t.Setenv(paths.EnvHome, t.TempDir())
	if err := config.SaveInterfaceLanguage(config.InterfaceLanguageChinese); err != nil {
		t.Fatal(err)
	}
	model := New(nil)
	model.width = 48
	model.height = 20
	model.noColor = true
	if model.interfaceLanguage != config.InterfaceLanguageChinese {
		t.Fatalf("default interface language = %q", model.interfaceLanguage)
	}
	if output := model.render(); !strings.Contains(output, "等待第一条提示词") || strings.Contains(output, "[READY]") || !strings.Contains(output, "[s] 设置") || strings.Contains(output, "[s] settings") {
		t.Fatalf("default interface did not localize user-facing status copy: %q", output)
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
	for _, name := range theme.SelectableNames() {
		for _, size := range sizes {
			model := Model{
				width: size[0], height: size[1], following: true, themeName: name, themeSource: config.ThemeConfig,
				snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "1", Text: "中文 prompt"}}, Metrics: &provider.SessionMetrics{
					TotalTokens: 12000, ContextWindow: 128000, ContextUsedPercent: 42,
					Quotas: []provider.QuotaWindow{{WindowMinutes: 300, UsedPercent: 55}, {WindowMinutes: 10080, UsedPercent: 82}}, QuotaStatus: provider.QuotaAvailable,
				}},
			}
			model.applyTheme(name)
			output := model.render()
			for index, line := range strings.Split(output, "\n") {
				if got := ansi.StringWidth(line); got > size[0] {
					t.Fatalf("theme=%s %dx%d line %d width = %d", name, size[0], size[1], index, got)
				}
			}
			model.overlay = overlayTheme
			model.beginThemePreview()
			for index, line := range model.themeLines() {
				if got := ansi.StringWidth(line); got > size[0] && !strings.Contains(line, "■") {
					t.Fatalf("theme=%s %dx%d preview line %d width = %d: %q", name, size[0], size[1], index, got, line)
				}
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

func TestWrapTextHandlesLongUnbrokenRuns(t *testing.T) {
	const width = 40
	longWord := strings.Repeat("a", 64<<10)
	got := wrapText(longWord, width)
	if strings.Join(got, "") != longWord {
		t.Fatal("long unbroken word changed while wrapping")
	}
	if len(got) != (len(longWord)+width-1)/width {
		t.Fatalf("long word produced %d lines", len(got))
	}
	for index, line := range got {
		if ansi.StringWidth(line) > width {
			t.Fatalf("long word line %d exceeded width", index)
		}
	}
}

func TestMixedWrapTokensGroupsLongWhitespaceRuns(t *testing.T) {
	spaces := strings.Repeat(" ", 64<<10)
	tokens := mixedWrapTokens("a" + spaces + "b")
	if len(tokens) != 3 || tokens[0].text != "a" || !tokens[1].space || tokens[1].text != spaces || tokens[1].width != len(spaces) || tokens[2].text != "b" {
		t.Fatalf("long whitespace run produced %#v", tokens)
	}
}

func TestVisibleLinesClampsStaleOffsetAfterContentShrinks(t *testing.T) {
	got := visibleLines([]string{"only"}, 99, 3)
	if !slices.Equal(got, []string{"", "", ""}) {
		t.Fatalf("visible lines at stale offset = %#v", got)
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
	if model.displaySelectedIndex() != 2 || !strings.Contains(ansi.Strip(strings.Join(model.bodyLines(), "\n")), "3 third") {
		t.Fatal("latest prompt was not selected by default")
	}

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model = updated.(Model)
	if model.selectedID != "two" || model.following || !strings.Contains(ansi.Strip(strings.Join(model.bodyLines(), "\n")), "2 second") {
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

func TestSettingsOwnsHelpAndViewerRequiresCtrlXToQuit(t *testing.T) {
	model := Model{width: 48, height: 20, noColor: true, following: false, selectedID: "one", offset: 1,
		snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: "first"}, {ID: "two", Text: "second"}}},
	}
	model = openHelpPage(model)
	if model.overlay != overlayHelp || !strings.Contains(model.render(), "Help controls") || !strings.Contains(model.render(), "Ctrl+X     Close this pane") || !strings.Contains(model.render(), "Settings controls") || !strings.Contains(model.render(), "Open/switch") || !strings.Contains(model.render(), "Prompt controls") || !strings.Contains(model.render(), "Enter      Expand or fold") || strings.Contains(model.render(), "DblClick") || !strings.Contains(model.render(), "Esc back") {
		t.Fatalf("help did not expose viewer shortcuts: %q", model.render())
	}
	if model.selectedID != "one" || model.offset != 1 || model.following {
		t.Fatalf("opening help changed prompt reading state: %#v", model)
	}

	updated, cmd := model.Update(tea.KeyPressMsg{Code: 'q'})
	model = updated.(Model)
	if cmd != nil || model.overlay != overlayHelp {
		t.Fatal("single-key q closed help or quit the viewer")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.overlay != overlaySettings {
		t.Fatal("Escape did not return from Help to Settings")
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

func TestOverlayFootersExplainPagePositionAndCurrentActions(t *testing.T) {
	for _, test := range []struct {
		width int
		want  string
	}{
		{width: 80, want: "↑/↓ preview · Enter save · Esc cancel"},
		{width: 48, want: "↑/↓ · Enter save · Esc cancel"},
		{width: 32, want: "↑/↓ · Enter · Esc"},
		{width: 24, want: "↑↓ Enter Esc"},
		{width: 20, want: "↑↓ Enter Esc"},
	} {
		model := Model{width: test.width, height: 20, noColor: true, overlay: overlayTheme, themeName: theme.Mocha, snapshot: ipc.Snapshot{State: "ready"}}
		model.beginThemePreview()
		footer := model.renderFooter()
		helpLabel := "Help 1/"
		if test.width < 26 {
			helpLabel = "H 1/"
		}
		if strings.Contains(footer, "Theme") || strings.Contains(footer, "1/1") || !strings.Contains(footer, test.want) || strings.Contains(footer, "[READY]") {
			t.Fatalf("width=%d theme footer did not explain its page and controls: %q", test.width, footer)
		}
		if ansi.StringWidth(footer) > test.width {
			t.Fatalf("width=%d theme footer exceeded width: %q", test.width, footer)
		}

		model.overlay = overlayHelp
		model.overlayOffset = 0
		footer = model.renderFooter()
		if !strings.Contains(footer, helpLabel) || !strings.Contains(footer, "↑") || !strings.Contains(footer, "Esc") || strings.Contains(footer, "[READY]") {
			t.Fatalf("width=%d help footer did not explain its page and controls: %q", test.width, footer)
		}
		if test.width >= 56 && !strings.Contains(footer, "PgUp/PgDn") {
			t.Fatalf("width=%d wide Help footer omitted compatible page keys: %q", test.width, footer)
		}
		if ansi.StringWidth(footer) > test.width {
			t.Fatalf("width=%d help footer exceeded width: %q", test.width, footer)
		}

		model.overlay = overlayAbout
		model.overlayOffset = 0
		footer = model.renderFooter()
		if strings.TrimSpace(ansi.Strip(footer)) != "Esc back" || strings.Contains(footer, "About") || strings.Contains(footer, "[READY]") || strings.Contains(footer, "PgUp") {
			t.Fatalf("width=%d single-page About footer did not explain its page and back action: %q", test.width, footer)
		}
		if ansi.StringWidth(footer) > test.width {
			t.Fatalf("width=%d About footer exceeded width: %q", test.width, footer)
		}

		model.overlay = overlaySettings
		model.settingsIndex = 1
		footer = model.renderFooter()
		if got := strings.TrimSpace(ansi.Strip(footer)); got != "Esc close" {
			t.Fatalf("width=%d Settings footer = %q, want only the close action", test.width, footer)
		}
		if ansi.StringWidth(footer) > test.width || strings.Contains(footer, "[READY]") {
			t.Fatalf("width=%d Settings footer overflowed or exposed state: %q", test.width, footer)
		}
	}
}

func TestSinglePageOverlaysHidePageCountsAndInactiveScrollAction(t *testing.T) {
	model := Model{width: 80, height: 80, noColor: true, overlay: overlayHelp, snapshot: ipc.Snapshot{State: "live"}}
	footer := model.renderFooter()
	if !strings.Contains(footer, "Help") || !strings.Contains(footer, "Esc back") || strings.Contains(footer, "1/1") || strings.Contains(footer, "PgUp/PgDn") || strings.Contains(footer, "↑") {
		t.Fatalf("single-page Help footer retained empty navigation: %q", footer)
	}
	model.overlay = overlayAbout
	footer = model.renderFooter()
	if strings.TrimSpace(ansi.Strip(footer)) != "Esc back" || strings.Contains(footer, "About") || strings.Contains(footer, "1/1") || strings.Contains(footer, "PgUp/PgDn") || strings.Contains(footer, "↑") {
		t.Fatalf("single-page About footer retained empty navigation: %q", footer)
	}
	model.overlay = overlayTheme
	model.themeName = theme.Mocha
	model.beginThemePreview()
	footer = model.renderFooter()
	if strings.Contains(footer, "Theme") || strings.Contains(footer, "1/1") {
		t.Fatalf("single-page Theme footer retained an empty page count: %q", footer)
	}
}

func TestHelpAndAboutArrowKeysPageWithPgKeyCompatibility(t *testing.T) {
	for _, page := range []string{"Help", "About"} {
		model := Model{width: 20, height: 6, noColor: true, snapshot: ipc.Snapshot{State: "live"}}
		if page == "Help" {
			model.overlay = overlayHelp
		} else {
			model.overlay = overlayAbout
		}
		if model.overlayMaxOffset() == 0 {
			t.Fatalf("%s fixture did not span multiple pages", page)
		}

		updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		model = updated.(Model)
		arrowOffset := model.overlayOffset
		if arrowOffset != min(model.bodyHeight(), model.overlayMaxOffset()) {
			t.Fatalf("%s Down moved to offset %d, want one page", page, arrowOffset)
		}

		updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		model = updated.(Model)
		if model.overlayOffset != 0 {
			t.Fatalf("%s Up did not return one page: offset=%d", page, model.overlayOffset)
		}

		updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		model = updated.(Model)
		if model.overlayOffset != arrowOffset {
			t.Fatalf("%s PgDn offset=%d, want arrow-compatible %d", page, model.overlayOffset, arrowOffset)
		}
		updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
		model = updated.(Model)
		if model.overlayOffset != 0 {
			t.Fatalf("%s PgUp did not return one page: offset=%d", page, model.overlayOffset)
		}
	}
}

func TestHelpExplainsGitStatusAtResponsiveWidths(t *testing.T) {
	for _, width := range []int{20, 24, 32, 48, 80} {
		model := Model{width: width, noColor: true, snapshot: ipc.Snapshot{State: "live"}}
		lines := model.helpLines()
		output := strings.Join(lines, "\n")
		for _, expected := range []string{"Git status", "main*", "branch", "Tracked", "+N/-N", "Untracked"} {
			if !strings.Contains(output, expected) {
				t.Fatalf("width=%d help omitted Git status meaning %q: %q", width, expected, output)
			}
		}
		if strings.Contains(output, "HEAD") {
			t.Fatalf("width=%d help exposed Git implementation terminology: %q", width, output)
		}
		if width >= 52 && !strings.Contains(output, "Lines since last commit") {
			t.Fatalf("width=%d help did not explain line counts in user terms: %q", width, output)
		}
		inGitStatus := false
		for _, line := range lines {
			heading := strings.TrimSpace(ansi.Strip(line))
			if heading == "Git status" {
				inGitStatus = true
			} else if heading == "Theme" {
				inGitStatus = false
			}
			if inGitStatus && ansi.StringWidth(line) > width {
				t.Fatalf("width=%d Git help exceeded width: %q", width, line)
			}
		}
	}
}

func TestHelpExplainsLeftCodexDisplayRecovery(t *testing.T) {
	for _, test := range []struct {
		language string
		width    int
		want     []string
	}{
		{
			language: config.InterfaceLanguageChinese,
			width:    20,
			want:     []string{"显示问题", "左侧错位时", "仅显示异常", "数据不受影响", "调整大小后", "滚到底部", "Ctrl+p→f 两次"},
		},
		{
			language: config.InterfaceLanguageChinese,
			width:    80,
			want:     []string{"显示排障", "左侧 Codex 偶尔错位或断行", "会话和提示词数据不受影响", "调整窗口／窗格大小并滚到底部", "Ctrl+p→f", "自定义 Zellij 按键"},
		},
		{
			language: config.InterfaceLanguageEnglish,
			width:    20,
			want:     []string{"Display issue", "Left pane glitch", "Display only", "Data is safe", "Resize pane", "Scroll to bottom", "Ctrl+p→f twice"},
		},
		{
			language: config.InterfaceLanguageEnglish,
			width:    80,
			want:     []string{"Display troubleshooting", "Left Codex may occasionally misalign", "Session and prompt data are unaffected", "Resize the window or pane and scroll to bottom", "Ctrl+p→f", "Custom Zellij bindings may differ"},
		},
	} {
		model := Model{width: test.width, noColor: true, interfaceLanguage: test.language, snapshot: ipc.Snapshot{State: "live"}}
		output := strings.Join(model.helpLines(), "\n")
		for _, expected := range test.want {
			if !strings.Contains(output, expected) {
				t.Fatalf("language=%s width=%d Help omitted display recovery %q: %q", test.language, test.width, expected, output)
			}
		}
	}
}

func TestCompactHelpScrollsWithoutChangingPromptReadingState(t *testing.T) {
	for _, size := range [][2]int{{20, 6}, {24, 10}} {
		model := Model{width: size[0], height: size[1], noColor: true, following: false, selectedID: "one", offset: 1,
			snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: "first\ncontinued"}, {ID: "two", Text: "second"}}},
		}
		model = openHelpPage(model)
		var updated tea.Model
		if strings.Contains(model.render(), "dracula") {
			t.Fatalf("%dx%d help unexpectedly fit without scrolling", size[0], size[1])
		}

		sawPaneDetails := false
		for range 20 {
			output := model.render()
			sawPaneDetails = sawPaneDetails || strings.Contains(output, "Custom keys vary")
			if strings.Contains(output, "Token Tracker") || strings.Contains(output, "About") {
				t.Fatalf("%dx%d Help retained About content: %q", size[0], size[1], output)
			}
			if model.overlayOffset == model.overlayMaxOffset() {
				break
			}
			updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
			model = updated.(Model)
		}
		if model.overlayOffset == 0 || !sawPaneDetails {
			t.Fatalf("%dx%d help did not reach pane details: %q", size[0], size[1], model.render())
		}
		label, _ := model.overlayPageInfo("Help")
		page := strings.Split(label, " ")[1]
		parts := strings.Split(page, "/")
		if len(parts) != 2 || parts[0] != parts[1] {
			t.Fatalf("%dx%d help bottom did not report the final page: %q", size[0], size[1], page)
		}
		previousOffset := model.overlayOffset
		updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
		model = updated.(Model)
		if model.overlayOffset >= previousOffset {
			t.Fatalf("%dx%d mouse wheel did not scroll help upward", size[0], size[1])
		}
		if model.selectedID != "one" || model.offset != 1 || model.following {
			t.Fatalf("%dx%d help scrolling changed prompt state: %#v", size[0], size[1], model)
		}
		updated, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		model = updated.(Model)
		if model.overlayOffset != model.overlayMaxOffset() {
			t.Fatalf("%dx%d help offset was not clamped after resize: %#v", size[0], size[1], model)
		}

		updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		model = updated.(Model)
		model.settingsIndex = 0
		updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		model = updated.(Model)
		if model.overlay != overlayTheme || model.overlayOffset != 0 {
			t.Fatalf("%dx%d Settings did not open Theme cleanly: %#v", size[0], size[1], model)
		}
		sawTheme, sawPreview := false, false
		for range 20 {
			output := model.render()
			sawTheme = sawTheme || strings.Contains(output, "dracula")
			sawPreview = sawPreview || strings.Contains(output, "界面预览") || strings.Contains(output, "Interface preview")
			if model.overlayOffset == model.overlayMaxOffset() {
				break
			}
			updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
			model = updated.(Model)
		}
		if !sawTheme || !sawPreview {
			t.Fatalf("%dx%d Theme did not reach its picker and preview: %q", size[0], size[1], model.render())
		}
		updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		model = updated.(Model)
		if model.overlay != overlaySettings || model.overlayOffset != 0 {
			t.Fatalf("%dx%d Theme did not return to Settings cleanly: %#v", size[0], size[1], model)
		}
	}
}

func TestMouseClickSelectsVisiblePromptWithoutJumpingViewport(t *testing.T) {
	model := Model{
		width: 40, height: 7, noColor: true, following: false, selectedID: "three", offset: 1,
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

	model = leftClick(model, 4, 1)
	if model.selectedID != "one" || model.offset != 1 {
		t.Fatalf("clicking prompt spacing changed selection: selected=%q offset=%d", model.selectedID, model.offset)
	}

	model = leftClick(model, 4, 2)
	if model.selectedID != "two" || model.offset != 1 {
		t.Fatalf("click did not map the visible prompt: selected=%q offset=%d", model.selectedID, model.offset)
	}

	model = leftClick(model, 4, 3)
	if model.selectedID != "two" || model.offset != 1 {
		t.Fatalf("clicking prompt spacing changed selection: selected=%q offset=%d", model.selectedID, model.offset)
	}

	adjacentModel := model
	adjacentModel.selectedID = "two"
	adjacentModel = leftClick(adjacentModel, 4, 4)
	if adjacentModel.selectedID != "three" || !adjacentModel.following {
		t.Fatalf("adjacent prompt first line mapped to the preceding prompt: %#v", adjacentModel)
	}
}

func TestMouseClickLatestRestoresFollowingAndIgnoresFooter(t *testing.T) {
	model := Model{
		width: 40, height: 10, noColor: true, selectedID: "one",
		snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{
			{ID: "one", Text: "first"}, {ID: "two", Text: "second"},
		}},
	}
	latest := model.layoutBody().prompts[1].start - model.offset
	model = leftClick(model, 4, latest)
	if model.selectedID != "two" || !model.following || model.offset != model.maxOffset() {
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
	expected := lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Text)).Background(lipgloss.Color(palette.Cell)).Render("只回复")
	if !strings.Contains(output, expected) || strings.Contains(output, "\x1b[7m") {
		t.Fatalf("wide selection did not use explicit colors: %q", output)
	}
}

func TestTerminalBackgroundAddsSemanticContrastFallbacks(t *testing.T) {
	model := Model{themeName: theme.Latte}
	model.applyTheme(theme.Latte)
	updated, _ := model.Update(tea.BackgroundColorMsg{Color: color.RGBA{R: 0xef, G: 0xf1, B: 0xf5, A: 0xff}})
	model = updated.(Model)
	if model.backgroundColor != "#eff1f5" || !model.lightBackground {
		t.Fatalf("terminal background = %q, light = %v", model.backgroundColor, model.lightBackground)
	}
	roles := model.visualRoles()
	if got := model.styleColor("66%", roles.Warning); got != "66%" {
		t.Fatalf("low-contrast warning text retained its color: %q", got)
	}
	if got, want := model.styleGraphicColor("██", roles.Warning), lipgloss.NewStyle().Foreground(lipgloss.Color(roles.Warning)).Render("██"); got != want {
		t.Fatalf("progress graphic = %q, want %q", got, want)
	}
	if got, want := model.styleColor("Failure", roles.Error), lipgloss.NewStyle().Foreground(lipgloss.Color(roles.Error)).Render("Failure"); got != want {
		t.Fatalf("readable error text = %q, want %q", got, want)
	}
	if got, want := model.styleAction("Settings"), lipgloss.NewStyle().Foreground(lipgloss.Color(roles.Accent)).Render("Settings"); got != want {
		t.Fatalf("fallback action = %q, want %q", got, want)
	}
	if got := model.styleMuted("Muted text"); got != "Muted text" {
		t.Fatalf("low-contrast muted text retained its color: %q", got)
	}
	selection := lipgloss.NewStyle().Foreground(lipgloss.Color(roles.SelectionText)).Background(lipgloss.Color(roles.SelectionSurface)).Underline(true).Render("Text selection")
	if got := model.styleTextSelection("Text selection"); got != selection {
		t.Fatalf("low-contrast selection boundary = %q, want %q", got, selection)
	}

	updated, _ = model.Update(tea.BackgroundColorMsg{Color: color.RGBA{A: 0xff}})
	model = updated.(Model)
	if got, want := model.styleColor("66%", roles.Warning), lipgloss.NewStyle().Foreground(lipgloss.Color(roles.Warning)).Render("66%"); got != want {
		t.Fatalf("readable warning text = %q, want %q", got, want)
	}
	selection = lipgloss.NewStyle().Foreground(lipgloss.Color(roles.SelectionText)).Background(lipgloss.Color(roles.SelectionSurface)).Render("Text selection")
	if got := model.styleTextSelection("Text selection"); got != selection {
		t.Fatalf("high-contrast selection gained an unnecessary boundary: %q", got)
	}
}

func TestThemePagePreviewsAndCancelsSelection(t *testing.T) {
	model := Model{width: 48, height: 20, noColor: true, themeName: theme.Mocha, themeSource: config.ThemeConfig}
	model.applyTheme(theme.Mocha)
	model = openThemePage(model)
	page := strings.Join(model.themeLines(), "\n")
	if model.overlay != overlayTheme || !strings.Contains(page, "mocha") || !strings.Contains(page, "■ ■ ■ ■ ■ ■ ■ ■") || strings.Contains(page, "current") || strings.Contains(page, "recommended") {
		t.Fatalf("Theme did not expose its palettes: %q", page)
	}
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	if model.themeName != theme.Latte || model.colors.Success != "#40a02b" {
		t.Fatalf("theme preview = %q %#v", model.themeName, model.colors)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.overlay != overlaySettings || model.themeName != theme.Mocha {
		t.Fatalf("theme cancel did not restore original: %#v", model)
	}
}

func TestThemePageExplainsControlsWithoutCrowdingExtremeShortPane(t *testing.T) {
	tests := []struct {
		width           int
		height          int
		wantInstruction bool
	}{
		{width: 80, height: 20, wantInstruction: true},
		{width: 32, height: 12, wantInstruction: true},
		{width: 24, height: 10, wantInstruction: true},
		{width: 20, height: 6, wantInstruction: false},
	}
	for _, test := range tests {
		model := Model{width: test.width, height: test.height, noColor: true, themeName: theme.Mocha}
		instruction := model.themeInstruction()
		if test.wantInstruction && (!strings.Contains(instruction, "click") || !strings.Contains(instruction, "Enter")) {
			t.Fatalf("%dx%d Theme instruction = %q", test.width, test.height, instruction)
		}
		if !test.wantInstruction && instruction != "" {
			t.Fatalf("%dx%d Theme instruction crowded the short pane: %q", test.width, test.height, instruction)
		}
		if ansi.StringWidth(instruction) > test.width {
			t.Fatalf("%dx%d Theme instruction exceeded width: %q", test.width, test.height, instruction)
		}
	}
}

func TestSettingsIsTheOnlyGlobalConfigurationEntry(t *testing.T) {
	t.Setenv(paths.EnvHome, t.TempDir())
	model := Model{width: 48, height: 20, noColor: true, themeName: theme.Mocha, interfaceLanguage: config.InterfaceLanguageChinese,
		snapshot: ipc.Snapshot{State: "live"}}
	model.applyTheme(theme.Mocha)

	for _, key := range []rune{'h', 't', 'l'} {
		updated, command := model.Update(tea.KeyPressMsg{Code: key})
		model = updated.(Model)
		if command != nil || model.overlayActive() || model.interfaceLanguage != config.InterfaceLanguageChinese {
			t.Fatalf("legacy shortcut %q still changed settings: %#v", key, model)
		}
	}

	model = openSettingsPage(model)
	page := strings.Join(model.settingsLines(), "\n")
	if model.overlay != overlaySettings || !strings.Contains(page, "主题") || !strings.Contains(page, "界面语言") || !strings.Contains(page, "中文") || !strings.Contains(page, "帮助") || !strings.Contains(page, "关于") {
		t.Fatalf("Settings did not collect the configuration entries: %q", page)
	}
	helpRow := -1
	for index, line := range model.settingsLines() {
		if strings.TrimSpace(ansi.Strip(line)) == "帮助" {
			helpRow = index
			break
		}
	}
	model = leftClick(model, 4, helpRow)
	if helpRow < 0 || model.settingsIndex != 2 || model.overlay != overlaySettings {
		t.Fatalf("click did not select the Settings Help row: row=%d model=%#v", helpRow, model)
	}
	model.settingsIndex = 0

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil || model.overlay != overlaySettings {
		t.Fatalf("Settings language selection did not remain visible: %#v", model)
	}
	updated, _ = model.Update(command())
	model = updated.(Model)
	if model.interfaceLanguage != config.InterfaceLanguageEnglish || !strings.Contains(strings.Join(model.settingsLines(), "\n"), "English") {
		t.Fatalf("Settings did not save and display English: %#v", model)
	}
	language, err := config.LoadInterfaceLanguage()
	if err != nil || language != config.InterfaceLanguageEnglish {
		t.Fatalf("saved interface language = %q, err = %v", language, err)
	}
}

func TestSettingsSelectionUsesBoldChoiceEmphasis(t *testing.T) {
	for _, noColor := range []bool{false, true} {
		model := Model{width: 48, height: 20, noColor: noColor, themeName: theme.Mocha, interfaceLanguage: config.InterfaceLanguageEnglish}
		model.applyTheme(theme.Mocha)
		selected := model.settingsLines()[2]
		style := lipgloss.NewStyle().Bold(true)
		if want := style.Render(ansi.Strip(selected)); selected != want {
			t.Fatalf("noColor=%v selected setting = %q, want bold choice emphasis %q", noColor, selected, want)
		}
		if unselected := model.settingsLines()[3]; strings.Contains(unselected, "\x1b[") {
			t.Fatalf("noColor=%v unselected setting gained emphasis: %q", noColor, unselected)
		}
	}
}

func TestCompactSettingsRevealsAboutAndEveryFooterKeepsRequiredActions(t *testing.T) {
	model := Model{
		width: 20, height: 6, noColor: true, themeName: theme.Mocha,
		interfaceLanguage: config.InterfaceLanguageChinese,
		following:         false,
		selectedID:        "one",
		offset:            1,
		snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{
			{ID: "one", Text: "first"},
			{ID: "two", Text: "second"},
		}},
	}
	model.applyTheme(theme.Mocha)
	model = openSettingsPage(model)

	for range 3 {
		updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		model = updated.(Model)
	}
	if model.settingsIndex != 3 || model.overlayOffset == 0 || !strings.Contains(model.render(), "关于") {
		t.Fatalf("20x6 Settings did not reveal About: %#v\n%s", model, model.render())
	}
	if footer := model.renderFooter(); strings.TrimSpace(ansi.Strip(footer)) != "Esc 关闭" {
		t.Fatalf("compact Settings footer did not contain only its close action: %q", footer)
	}

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.overlay != overlayAbout {
		t.Fatalf("Settings did not open About independently: %#v", model)
	}
	aboutFooter := model.renderFooter()
	for _, expected := range []string{"1/", "↑", "Esc"} {
		if !strings.Contains(aboutFooter, expected) {
			t.Fatalf("compact About footer omitted %q: %q", expected, aboutFooter)
		}
	}
	if strings.Contains(aboutFooter, "关于") {
		t.Fatalf("compact About footer repeated its top title: %q", aboutFooter)
	}

	sawEnvironment, sawCredit := false, false
	for range 10 {
		output := model.render()
		sawEnvironment = sawEnvironment || strings.Contains(output, "PowerShell 5.1/7") && strings.Contains(output, "Codex CLI")
		sawCredit = sawCredit || strings.Contains(output, "Token Tracker")
		if model.overlayOffset == model.overlayMaxOffset() {
			break
		}
		updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		model = updated.(Model)
	}
	if !sawEnvironment || !sawCredit {
		t.Fatalf("compact About did not expose all content: environment=%v credit=%v\n%s", sawEnvironment, sawCredit, model.render())
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.overlay != overlaySettings || model.settingsIndex != 3 || model.overlayOffset == 0 || !strings.Contains(model.render(), "关于") {
		t.Fatalf("About did not return to the visible selected Settings row: %#v\n%s", model, model.render())
	}
	if model.selectedID != "one" || model.offset != 1 || model.following {
		t.Fatalf("About navigation changed prompt reading state: %#v", model)
	}
}

func TestSettingsKeepsCloseActionOutOfBody(t *testing.T) {
	for _, language := range []string{config.InterfaceLanguageChinese, config.InterfaceLanguageEnglish} {
		wantAction := "Esc 关闭"
		if language == config.InterfaceLanguageEnglish {
			wantAction = "Esc close"
		}
		for _, size := range [][2]int{{20, 6}, {24, 10}, {32, 12}, {48, 20}, {80, 24}} {
			model := Model{width: size[0], height: size[1], noColor: true, overlay: overlaySettings, themeName: theme.Mocha, interfaceLanguage: language}
			if footer := model.renderFooter(); strings.TrimSpace(ansi.Strip(footer)) != wantAction {
				t.Fatalf("language=%s %dx%d Settings footer = %q, want only %q", language, size[0], size[1], footer, wantAction)
			}
			page := strings.Join(model.settingsLines(), "\n")
			for _, instruction := range []string{"↑/↓", "Enter", "Esc", "click", "单击"} {
				if strings.Contains(page, instruction) {
					t.Fatalf("language=%s %dx%d Settings retained %q instruction: %q", language, size[0], size[1], instruction, page)
				}
			}
		}
	}
}

func TestHelpOwnsSettingsControlInstructions(t *testing.T) {
	for _, test := range []struct {
		language string
		width    int
		want     []string
	}{
		{language: config.InterfaceLanguageChinese, width: 20, want: []string{"设置页操作", "选择设置", "打开/切换", "关闭"}},
		{language: config.InterfaceLanguageChinese, width: 80, want: []string{"设置页操作", "选择设置", "打开页面或切换语言", "关闭设置"}},
		{language: config.InterfaceLanguageEnglish, width: 20, want: []string{"Settings controls", "Select", "Open/switch", "Close"}},
		{language: config.InterfaceLanguageEnglish, width: 80, want: []string{"Settings controls", "Select setting", "Open page or switch language", "Close settings"}},
	} {
		model := Model{width: test.width, height: 24, noColor: true, overlay: overlayHelp, interfaceLanguage: test.language, snapshot: ipc.Snapshot{State: "live"}}
		page := strings.Join(model.helpLines(), "\n")
		for _, expected := range append([]string{"↑/↓", "Enter", "Esc"}, test.want...) {
			if !strings.Contains(page, expected) {
				t.Fatalf("language=%s width=%d Help omitted Settings instruction %q: %q", test.language, test.width, expected, page)
			}
		}
	}
}

func TestChineseOverlayFootersLocalizeLabelsAndActions(t *testing.T) {
	model := Model{width: 80, height: 24, noColor: true, overlay: overlaySettings, themeName: theme.Mocha, interfaceLanguage: config.InterfaceLanguageChinese}
	for index := range 4 {
		model.settingsIndex = index
		footer := model.renderFooter()
		if got := strings.TrimSpace(ansi.Strip(footer)); got != "Esc 关闭" || strings.Contains(footer, "设置") {
			t.Fatalf("setting=%d Chinese Settings footer repeated its title or omitted close: %q", index, footer)
		}
	}

	model.overlay = overlayTheme
	model.beginThemePreview()
	if footer := model.renderFooter(); strings.Contains(footer, "主题") || !strings.Contains(footer, "预览") || !strings.Contains(footer, "保存") || !strings.Contains(footer, "取消") {
		t.Fatalf("Chinese Theme footer = %q", footer)
	}
	model.overlay = overlayHelp
	if footer := model.renderFooter(); !strings.Contains(footer, "帮助") || !strings.Contains(footer, "返回") || strings.Contains(footer, "Help") || strings.Contains(footer, "back") {
		t.Fatalf("Chinese Help footer = %q", footer)
	}
	model.overlay = overlayAbout
	if footer := model.renderFooter(); strings.Contains(footer, "关于") || !strings.Contains(footer, "返回") || strings.Contains(footer, "About") || strings.Contains(footer, "back") {
		t.Fatalf("Chinese About footer = %q", footer)
	}
}

func TestLocalizedFootersFitRequiredWidths(t *testing.T) {
	for _, language := range []string{config.InterfaceLanguageChinese, config.InterfaceLanguageEnglish} {
		for _, width := range []int{20, 24, 32, 48, 80} {
			for page := range 5 {
				model := Model{width: width, height: 10, noColor: true, themeName: theme.Mocha, interfaceLanguage: language, snapshot: ipc.Snapshot{State: "ready"}}
				switch page {
				case 1:
					model.overlay = overlaySettings
				case 2:
					model.overlay = overlayHelp
				case 3:
					model.overlay = overlayAbout
				case 4:
					model.overlay = overlayTheme
					model.beginThemePreview()
				}
				if footer := model.renderFooter(); ansi.StringWidth(footer) > width {
					t.Fatalf("language=%s width=%d page=%d footer overflowed: %q", language, width, page, footer)
				}
			}
		}
	}
}

func TestStatusLocalizationKeepsTechnicalFieldsStable(t *testing.T) {
	metrics := &provider.SessionMetrics{
		Branch: "main", Model: "gpt-5.6", Effort: "high", TotalTokens: 2400000,
		ContextWindow: 258000, ContextUsedPercent: 42,
		Quotas: []provider.QuotaWindow{{WindowMinutes: 300, UsedPercent: 55}, {WindowMinutes: 10080, UsedPercent: 82}}, QuotaStatus: provider.QuotaAvailable,
	}
	for _, test := range []struct {
		language string
		action   string
	}{
		{language: config.InterfaceLanguageChinese, action: "[s] 设置"},
		{language: config.InterfaceLanguageEnglish, action: "[s] settings"},
	} {
		model := Model{width: 120, height: 24, noColor: true, interfaceLanguage: test.language, snapshot: ipc.Snapshot{State: "live", Metrics: metrics}}
		status := strings.Join(model.renderStatusBlock(2), "\n")
		for _, expected := range []string{"main", "Total: 2.4M", "gpt-5.6 high", "5h:", "7d:", "Ctx: 258k", test.action} {
			if !strings.Contains(status, expected) {
				t.Fatalf("language=%s status omitted stable field %q: %q", test.language, expected, status)
			}
		}
		if strings.Contains(status, "[LIVE]") {
			t.Fatalf("language=%s status exposed the normal live state: %q", test.language, status)
		}
	}
}

func TestInterfaceLanguageLocalizesUIAndUserFacingStatusCopy(t *testing.T) {
	model := Model{
		width: 80, height: 24, noColor: true, themeName: theme.Mocha,
		interfaceLanguage: config.InterfaceLanguageChinese,
		snapshot:          ipc.Snapshot{State: "ready"},
		expanded:          make(map[string]bool),
	}
	model.applyTheme(theme.Mocha)
	if body := strings.Join(model.bodyLines(), "\n"); !strings.Contains(body, "等待第一条提示词") || strings.Contains(body, "Waiting for") {
		t.Fatalf("Chinese empty state = %q", body)
	}
	if settings := strings.Join(model.settingsLines(), "\n"); !strings.Contains(settings, "设置") || !strings.Contains(settings, "界面语言") || !strings.Contains(settings, "帮助") || !strings.Contains(settings, "关于") {
		t.Fatalf("Chinese settings = %q", settings)
	}
	if help := strings.Join(model.helpLines(), "\n"); !strings.Contains(help, "连接") || !strings.Contains(help, "帮助页操作") || !strings.Contains(help, "设置页操作") || !strings.Contains(help, "窗格操作") || !strings.Contains(help, "显示排障") || strings.Contains(help, "Help controls") {
		t.Fatalf("Chinese help = %q", help)
	}
	if about := strings.Join(model.aboutLines(), "\n"); !strings.Contains(about, "关于") || !strings.Contains(about, "支持环境") || !strings.Contains(about, "视觉参考") || !strings.Contains(about, "PowerShell 5.1/7") || !strings.Contains(about, "Codex CLI") {
		t.Fatalf("Chinese About = %q", about)
	}
	if themePage := strings.Join(model.themeLines(), "\n"); !strings.Contains(themePage, "主题") || !strings.Contains(themePage, "界面预览") || !strings.Contains(ansi.Strip(themePage), "再看边界条件") || strings.Contains(themePage, "[LIVE]") || !strings.Contains(themePage, "Total:") || !strings.Contains(themePage, "[s] 设置") || strings.Contains(themePage, "[s] settings") {
		t.Fatalf("Chinese theme page = %q", themePage)
	}
	if notice := model.localizedNotice("Session resumed. Showing new prompts only."); notice != "会话已恢复，只显示之后的新提示词。" {
		t.Fatalf("Chinese session notice = %q", notice)
	}
	if below := model.belowText(3); below != "↓ 下方还有 3 条提示词" {
		t.Fatalf("Chinese below notice = %q", below)
	}
	if fold := model.foldSummary(4, 40, true); !strings.Contains(fold, "另有 4 行") || !strings.Contains(fold, "展开") {
		t.Fatalf("Chinese fold summary = %q", fold)
	}

	chineseStatus := strings.Join(model.renderStatusBlock(2), "\n")
	model.interfaceLanguage = config.InterfaceLanguageEnglish
	englishStatus := strings.Join(model.renderStatusBlock(2), "\n")
	if chineseStatus == englishStatus || strings.Contains(chineseStatus, "[READY]") || !strings.Contains(chineseStatus, "[s] 设置") || !strings.Contains(chineseStatus, "首次回复后显示指标") || strings.Contains(chineseStatus, "settings") || strings.Contains(chineseStatus, "Metrics available") {
		t.Fatalf("Chinese status copy = %q", chineseStatus)
	}
	if strings.Contains(englishStatus, "[READY]") || !strings.Contains(englishStatus, "[s] settings") || !strings.Contains(englishStatus, "Metrics available after first response") || strings.Contains(englishStatus, "设置") || strings.Contains(englishStatus, "首次回复") {
		t.Fatalf("English status copy = %q", englishStatus)
	}
	if body := strings.Join(model.bodyLines(), "\n"); !strings.Contains(body, "Waiting for your first prompt") || strings.Contains(body, "等待第一条提示词") {
		t.Fatalf("English empty state = %q", body)
	}
	if settings := strings.Join(model.settingsLines(), "\n"); !strings.Contains(settings, "Settings") || !strings.Contains(settings, "Language") || !strings.Contains(settings, "Help") || !strings.Contains(settings, "About") || strings.Contains(settings, "界面语言") {
		t.Fatalf("English settings = %q", settings)
	}
	if about := strings.Join(model.aboutLines(), "\n"); !strings.Contains(about, "About") || !strings.Contains(about, "Supported environment") || !strings.Contains(about, "Visual reference") || strings.Contains(about, "支持环境") {
		t.Fatalf("English About = %q", about)
	}

	model.snapshot = ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "turn", Text: "Keep this 原文"}}}
	if body := strings.Join(model.bodyLines(), "\n"); !strings.Contains(body, "Keep this 原文") {
		t.Fatalf("prompt text was translated: %q", body)
	}
}

func TestSettingsOpensHelpAndThemeWithoutChangingPromptReadingState(t *testing.T) {
	model := Model{width: 48, height: 20, noColor: true, selectedID: "one", offset: 2, themeName: theme.Mocha,
		snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: "first"}}}}
	model.applyTheme(theme.Mocha)
	model = openHelpPage(model)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	model.settingsIndex = 0
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.overlay != overlayTheme {
		t.Fatalf("Settings did not open Theme: %#v", model)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	model.settingsIndex = 2
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.overlay != overlayHelp || model.selectedID != "one" || model.offset != 2 {
		t.Fatalf("Settings navigation changed prompt state: %#v", model)
	}
}

func TestThemePickerSupportsClickWithoutTreatingDragAsClick(t *testing.T) {
	model := Model{width: 48, height: 20, noColor: true, themeName: theme.Mocha, themeSource: config.ThemeConfig}
	model.applyTheme(theme.Mocha)
	model = openThemePage(model)
	latteRow := -1
	for index, line := range model.themeLines() {
		if strings.Contains(ansi.Strip(line), "latte") {
			latteRow = index - model.overlayOffset
			break
		}
	}
	if latteRow < 0 || latteRow >= model.bodyHeight() {
		t.Fatalf("latte theme row is not visible: row=%d page=%q", latteRow, model.render())
	}
	model = leftClick(model, 4, latteRow)
	if model.themeName != theme.Latte || model.themeIndex != 1 || model.overlay != overlayTheme {
		t.Fatalf("click did not preview latte: %#v", model)
	}

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	model.settingsIndex = 0
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: 4, Y: latteRow, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseMotionMsg{X: 5, Y: latteRow, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseReleaseMsg{X: 5, Y: latteRow, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.themeName != theme.Mocha {
		t.Fatalf("dragging theme text also changed the preview: %#v", model)
	}
}

func TestThemePageResolvesAutoWithoutListingIt(t *testing.T) {
	model := Model{width: 48, height: 20, noColor: true, themeName: theme.Auto, themeSource: config.ThemeDefault}
	model.applyTheme(theme.Auto)
	model = openThemePage(model)
	output := strings.Join(model.themeLines(), "\n")
	if strings.Contains(output, " auto") || model.themeName != theme.Mocha || !strings.Contains(output, "› mocha") || strings.Contains(output, "recommended") {
		t.Fatalf("auto was not resolved to an explicit dark theme: name=%q output=%q", model.themeName, output)
	}
}

func TestHelpAboutAndThemeUseUnifiedColumnsAndSectionColors(t *testing.T) {
	model := Model{width: 80, height: 24, themeName: theme.Mocha, themeSource: config.ThemeConfig, snapshot: ipc.Snapshot{State: "live"}}
	model.applyTheme(theme.Dracula)
	model = openHelpPage(model)
	lines := model.helpLines()

	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Resolve(theme.Dracula, false).Sapphire))
	if len(lines) == 0 || ansi.Strip(lines[0]) != " Help controls" {
		t.Fatalf("help did not start directly with its first section: %q", lines)
	}
	for _, heading := range []string{" Help controls", " Settings controls", " Prompt controls · outside Help", " Metrics", " Pane controls", " Display troubleshooting"} {
		if !slices.Contains(lines, accent.Render(heading)) {
			t.Fatalf("help missed grouped heading %q: %q", heading, lines)
		}
	}
	if !slices.Contains(lines, model.helpEntry("Ctrl+X", "Close this pane")) {
		t.Fatalf("help body did not use the normal foreground: %q", lines)
	}
	if output := strings.Join(lines, "\n"); strings.Contains(output, "Interface preview") || strings.Contains(output, "› mocha") {
		t.Fatalf("Help retained Theme content: %q", output)
	}
	plainOutput := ansi.Strip(strings.Join(lines, "\n"))
	ordered := []string{" Help controls\n", "\n Settings controls\n", "\n Prompt controls · outside Help\n", "\n Metrics\n", "\n Git status\n", "\n Pane controls\n", "\n Display troubleshooting\n"}
	previous := -1
	for _, section := range ordered {
		index := strings.Index(plainOutput, section)
		if index <= previous {
			t.Fatalf("help sections are missing or out of order at %q: %q", section, plainOutput)
		}
		previous = index
	}
	for _, expected := range []string{
		"Alt+←/→    Focus pane",
		"Drag edge  Resize panes",
		"Ctrl+p→f   Fullscreen focused pane",
		"Left Codex may occasionally misalign",
		"Session and prompt data are unaffected.",
	} {
		if !strings.Contains(plainOutput, expected) {
			t.Fatalf("help missed compact workspace/about text %q: %q", expected, plainOutput)
		}
	}
	for _, unexpected := range []string{"Prompt Pane v" + appversion.Current, "Token Tracker", " About"} {
		if strings.Contains(plainOutput, unexpected) {
			t.Fatalf("Help retained About content %q: %q", unexpected, plainOutput)
		}
	}

	model.overlay = overlayAbout
	aboutLines := model.aboutLines()
	aboutOutput := ansi.Strip(strings.Join(aboutLines, "\n"))
	for _, expected := range []string{
		" About",
		"Prompt Pane v" + appversion.Current,
		"Supported environment",
		"Windows x64",
		"PowerShell 5.1/7",
		"Codex CLI",
		"Zellij " + zellij.Version,
		"Visual reference",
		"Token Tracker",
	} {
		if !strings.Contains(aboutOutput, expected) {
			t.Fatalf("About missed %q: %q", expected, aboutOutput)
		}
	}

	model.overlay = overlayHelp
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	model.settingsIndex = 0
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	themeLines := model.themeLines()
	if model.overlay != overlayTheme || !slices.Contains(themeLines, accent.Render(" Theme")) {
		t.Fatalf("Theme did not open independently with an accented title: %q", themeLines)
	}

	nameColumn, swatchColumn := -1, -1
	for _, name := range theme.SelectableNames() {
		for _, line := range themeLines {
			plain := ansi.Strip(line)
			nameIndex := strings.Index(plain, name)
			swatchIndex := strings.Index(plain, "■")
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
	if nameColumn != 3 {
		t.Fatalf("theme names start at column %d, want shared content column 3", nameColumn)
	}
	shortcutLine := ansi.Strip(model.helpEntry("Ctrl+X", "Close this pane"))
	if shortcutColumn := ansi.StringWidth(shortcutLine[:strings.Index(shortcutLine, "Ctrl+X")]); shortcutColumn != nameColumn {
		t.Fatalf("shortcut column = %d, theme name column = %d", shortcutColumn, nameColumn)
	}
	descriptionColumn := ansi.StringWidth(shortcutLine[:strings.Index(shortcutLine, "Close this pane")])
	if descriptionColumn != swatchColumn {
		t.Fatalf("description column = %d, swatch column = %d", descriptionColumn, swatchColumn)
	}
	for content, wantColumn := range map[string]int{"Check this code": 4, "Test edge cases": 4} {
		found := false
		for _, line := range themeLines {
			plain := ansi.Strip(line)
			index := strings.Index(plain, content)
			if index < 0 {
				continue
			}
			found = true
			if column := ansi.StringWidth(plain[:index]); column != wantColumn {
				t.Fatalf("content %q starts at column %d, want %d: %q", content, column, wantColumn, plain)
			}
			break
		}
		if !found {
			t.Fatalf("help missed content %q", content)
		}
	}
}

func TestHelpUnifiedGridFitsFixedSizes(t *testing.T) {
	for _, language := range []string{config.InterfaceLanguageChinese, config.InterfaceLanguageEnglish} {
		for _, size := range [][2]int{{20, 6}, {24, 10}, {32, 12}, {48, 20}, {80, 24}} {
			model := Model{width: size[0], height: size[1], noColor: true, overlay: overlayHelp, themeName: theme.Mocha, interfaceLanguage: language, snapshot: ipc.Snapshot{State: "ready"}}
			model.applyTheme(theme.Mocha)
			model.overlay = overlaySettings
			for _, line := range model.settingsLines() {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("language=%s %dx%d Settings line exceeded width: %q", language, size[0], size[1], line)
				}
			}
			model.overlay = overlayHelp
			for _, line := range model.helpLines() {
				if ansi.StringWidth(line) > size[0] && !strings.Contains(line, "■") {
					t.Fatalf("language=%s %dx%d help line exceeded width: %q", language, size[0], size[1], line)
				}
			}
			model.overlay = overlayAbout
			for _, line := range model.aboutLines() {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("language=%s %dx%d About line exceeded width: %q", language, size[0], size[1], line)
				}
			}
			model.overlay = overlayTheme
			model.beginThemePreview()
			for _, line := range model.themeLines() {
				if ansi.StringWidth(line) > size[0] && !strings.Contains(line, "■") {
					t.Fatalf("language=%s %dx%d Theme line exceeded width: %q", language, size[0], size[1], line)
				}
			}
		}
	}
}

func TestThemePickerUsesTokenTrackerSquarePalette(t *testing.T) {
	model := Model{width: 48, height: 20, themeName: theme.Mocha, themeSource: config.ThemeConfig}
	model.applyTheme(theme.Mocha)
	model = openThemePage(model)
	output := strings.Join(model.themeLines(), "\n")
	palette := theme.Resolve(theme.Mocha, false)

	selected := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(palette.Mauve)).Render(" › mocha    ")
	if !strings.Contains(output, selected) {
		t.Fatalf("selected theme did not use bold mauve choice emphasis: %q", output)
	}
	for _, color := range []string{
		palette.Green, palette.Yellow, palette.Peach, palette.Red,
		palette.Blue, palette.Sapphire, palette.Mauve, palette.Pink,
	} {
		square := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render("■")
		if !strings.Contains(output, square) {
			t.Fatalf("theme palette missed square color %s: %q", color, output)
		}
	}
	plain := ansi.Strip(output)
	if strings.Contains(plain, "●") || !strings.Contains(plain, "■ ■ ■ ■ ■ ■ ■ ■") || strings.Contains(plain, "recommended") || strings.Contains(plain, "current") {
		t.Fatalf("theme picker did not use the requested square-only layout: %q", plain)
	}
}

func TestThemeSemanticPreviewUsesVisibleThemeRoles(t *testing.T) {
	for _, name := range theme.SelectableNames() {
		model := Model{width: 80, height: 24, themeName: name, themeSource: config.ThemeConfig, snapshot: ipc.Snapshot{State: "live"}}
		model.applyTheme(name)
		output := strings.Join(model.themeLines(), "\n")
		roles := theme.Derive(theme.Resolve(name, false))
		styled := func(color, text string) string {
			return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(text)
		}
		styledBold := func(color, text string) string {
			return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color)).Render(text)
		}
		want := []string{
			styled(roles.Accent, " Interface preview"),
			styledBold(roles.BodyText, "  1 "),
			styledBold(roles.BodyText, "Check this code"),
			styledBold(roles.BodyText, "  2 "),
			styledBold(roles.BodyText, "Test "),
			styled(roles.ActivityIndicator, "Pondering "),
			styled(roles.ActivityIndicator, "..."),
			styled(roles.Accent, "↓ 3 prompts below"),
			styled(roles.Accent, "[s] settings"),
			styled(roles.Token, "Total: 2.4M"),
			styled(roles.Model, "gpt-5.6"),
			styled(roles.Label, "5h: "),
			styled(roles.Warning, "66%"),
			styled(roles.Branch, "main"),
			styled(roles.Branch, "*"),
			styled(roles.Added, "+12"),
			styled(roles.Deleted, "-3"),
			styled(roles.Untracked, "?1"),
			lipgloss.NewStyle().Foreground(lipgloss.Color(roles.SelectionText)).Background(lipgloss.Color(roles.SelectionSurface)).Render("edge cases"),
		}
		for _, expected := range want {
			if !strings.Contains(output, expected) {
				t.Fatalf("theme=%s preview missed semantic sample %q: %q", name, ansi.Strip(expected), output)
			}
		}
		lines := model.themePreviewLines()
		plain := make([]string, len(lines))
		for index := range lines {
			plain[index] = ansi.Strip(lines[index])
		}
		if len(lines) != 8 || plain[0] != "  1 Check this code" || plain[1] != "" || plain[2] != "  2 Test edge cases" || plain[3] != "    Pondering ..." || plain[5] != "" {
			t.Fatalf("theme=%s preview did not reuse prompt spacing: %q", name, plain)
		}
		if strings.Contains(output, "[READY]") || strings.Contains(output, "[LIVE]") || strings.Contains(output, "[ENDED]") || strings.Contains(output, "[ERROR]") {
			t.Fatalf("theme=%s preview exposed lifecycle state codes: %q", name, plain)
		}
	}
}

func TestStatusUsesTokenTrackerSemanticColors(t *testing.T) {
	for _, name := range theme.SelectableNames() {
		model := Model{width: 120, height: 24, themeName: name, snapshot: ipc.Snapshot{State: "live", Metrics: &provider.SessionMetrics{
			Branch: "main*", Added: 12, Deleted: 3, Untracked: 1,
			TotalTokens: 2400000, Model: "gpt-5.6", Effort: "high", ContextWindow: 258000, ContextUsedPercent: 42,
			Quotas: []provider.QuotaWindow{{WindowMinutes: 300, UsedPercent: 66}, {WindowMinutes: 10080, UsedPercent: 82}}, QuotaStatus: provider.QuotaAvailable,
		}}}
		model.applyTheme(name)
		roles := model.visualRoles()
		styled := func(color, text string) string {
			return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(text)
		}
		header := model.renderStatusHeader()
		metrics := model.compactMetricRow(119)
		for _, expected := range []string{
			styled(roles.Branch, "main"),
			styled(roles.Branch, "*"),
			styled(roles.Added, "+12"),
			styled(roles.Deleted, "-3"),
			styled(roles.Untracked, "?1"),
			styled(roles.Token, "Total: 2.4M"),
			styled(roles.Model, "gpt-5.6 high"),
		} {
			if !strings.Contains(header, expected) {
				t.Fatalf("theme=%s status header missed %q: %q", name, ansi.Strip(expected), header)
			}
		}
		for _, expected := range []string{
			styled(roles.Label, "5h: "), styled(roles.Warning, "66%"),
			styled(roles.Label, "7d: "), styled(roles.Error, "82%"),
			styled(roles.Label, "Ctx: 258k  "), styled(roles.Success, "42%"),
		} {
			if !strings.Contains(metrics, expected) {
				t.Fatalf("theme=%s metric row missed %q: %q", name, ansi.Strip(expected), metrics)
			}
		}
	}
}

func TestRecoverableWarningsAndSaveFailuresUseDifferentRoles(t *testing.T) {
	model := Model{width: 80, height: 24, themeName: theme.Mocha, themeSource: config.ThemeConfig, interfaceLanguage: config.InterfaceLanguageEnglish, settingsMessage: "settings failed"}
	model.applyTheme(theme.Mocha)
	roles := model.visualRoles()
	styled := func(color, text string) string {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(text)
	}
	if page := strings.Join(model.settingsLines(), "\n"); !strings.Contains(page, styled(roles.Error, "   settings failed")) || strings.Contains(page, styled(roles.Warning, "   settings failed")) {
		t.Fatalf("Settings save failure did not use the error role: %q", page)
	}

	model.themeMessage = "theme failed"
	if page := strings.Join(model.themeLines(), "\n"); !strings.Contains(page, styled(roles.Error, "   theme failed")) || strings.Contains(page, styled(roles.Warning, "   theme failed")) {
		t.Fatalf("Theme save failure did not use the error role: %q", page)
	}

	model.themeSource = config.ThemeEnvironment
	if page := strings.Join(model.themeLines(), "\n"); !strings.Contains(page, styled(roles.Warning, "   "+theme.Environment+" overrides saved settings")) || strings.Contains(page, styled(roles.Error, "   "+theme.Environment+" overrides saved settings")) {
		t.Fatalf("environment override did not retain the warning role: %q", page)
	}
}

func TestThemePageSavesSelectionAndCloses(t *testing.T) {
	t.Setenv(paths.EnvHome, t.TempDir())
	model := Model{width: 48, height: 20, noColor: true, themeName: theme.Mocha, themeSource: config.ThemeConfig}
	model.applyTheme(theme.Mocha)
	model = openThemePage(model)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("theme save command is nil")
	}
	updated, _ = model.Update(command())
	model = updated.(Model)
	name, source, err := config.LoadTheme()
	if err != nil || name != theme.Latte || source != config.ThemeConfig || model.overlay != overlaySettings || model.themeMessage != "" {
		t.Fatalf("saved theme = %q, source = %q, overlay = %v, message = %q, err = %v", name, source, model.overlay, model.themeMessage, err)
	}
}

func TestStatusLineUsesThemeRolesAndFitsWidth(t *testing.T) {
	model := Model{width: 48, height: 12, snapshot: ipc.Snapshot{State: "live", Metrics: &provider.SessionMetrics{
		Branch: "main", Model: "gpt-5", TotalTokens: 12500, ContextUsedPercent: 42,
		Quotas: []provider.QuotaWindow{{WindowMinutes: 300, UsedPercent: 75}, {WindowMinutes: 10080, UsedPercent: 92}}, QuotaStatus: provider.QuotaAvailable,
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
	for _, state := range []string{"ended", "error"} {
		model := Model{width: 48, height: 12, noColor: true, snapshot: ipc.Snapshot{State: state}}
		if output := model.render(); strings.Contains(output, "Metrics available after first response") {
			t.Fatalf("state=%s exposed a future metrics promise: %q", state, output)
		}
	}
	model := Model{width: 80, height: 12, noColor: true, snapshot: ipc.Snapshot{State: "live", Metrics: &provider.SessionMetrics{}}}
	output := strings.Join(model.renderStatusBlock(4), "\n")
	for _, expected := range []string{"Total: --", "Ctx: --"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("known metrics update hid unknown field %q: %q", expected, output)
		}
	}
	if strings.Contains(output, "Limit:") || strings.Contains(output, "5h:") || strings.Contains(output, "7d:") {
		t.Fatalf("known metrics update displayed unavailable quota windows: %q", output)
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

func TestDefaultStatusKeepsShortTokenTrackerBars(t *testing.T) {
	metrics := &provider.SessionMetrics{
		Branch: "main", Model: "gpt-5", TotalTokens: 23000,
		ContextWindow: 258000, ContextUsedPercent: 9,
		Quotas: []provider.QuotaWindow{{WindowMinutes: 300, UsedPercent: 21}, {WindowMinutes: 10080, UsedPercent: 44}}, QuotaStatus: provider.QuotaAvailable,
	}
	narrow := Model{width: 48, height: 20, snapshot: ipc.Snapshot{State: "live", Metrics: metrics}}
	narrow.applyTheme(theme.Mocha)
	narrowLines := narrow.renderStatusBlock(4)
	narrowOutput := strings.Join(narrowLines, "\n")
	plain := ansi.Strip(narrowOutput)
	if !strings.Contains(plain, "main | Total: 23k") || strings.Contains(plain, "(main)") || strings.Contains(plain, "[LIVE]") || strings.Contains(plain, "prompt-pane") || !strings.Contains(plain, "█") || !strings.Contains(plain, "░") || strings.Contains(narrowOutput, "\x1b[48;2;") {
		t.Fatalf("default status lost branch placement or Token Tracker bars: %q", narrowLines)
	}
	for _, width := range []int{48} {
		model := narrow
		model.width = width
		output := strings.Join(model.renderStatusBlock(4), "\n")
		plain := ansi.Strip(output)
		if !strings.Contains(plain, "█") || !strings.Contains(plain, "░") || strings.Contains(output, "\x1b[48;2;") {
			t.Fatalf("width=%d dropped progress bars too early: %q", width, output)
		}
	}
	for _, width := range []int{24, 32} {
		model := narrow
		model.width = width
		output := ansi.Strip(strings.Join(model.renderStatusBlock(4), "\n"))
		if !strings.Contains(output, "5h:") || !strings.Contains(output, "7d:") || strings.Contains(output, "█") || strings.Contains(output, "░") {
			t.Fatalf("width=%d did not preserve quota percentages after exhausting compact bars: %q", width, output)
		}
	}

	wide := narrow
	wide.width = 80
	wideLines := wide.renderStatusBlock(4)
	wideOutput := ansi.Strip(strings.Join(wideLines, "\n"))
	if len(narrowLines) != 2 || len(wideLines) != 2 || !strings.Contains(ansi.Strip(narrowOutput), "Ctx:") || !strings.Contains(wideOutput, "Ctx: 258k") || strings.Contains(wideOutput, "Model:") {
		t.Fatalf("status did not stay at two rows or reveal context only when it fits: narrow=%q wide=%q", narrowLines, wideLines)
	}
	for _, line := range wideLines {
		if strings.Contains(ansi.Strip(line), "█████████") || strings.Contains(ansi.Strip(line), "░░░░░░░░░") {
			t.Fatalf("default status bar exceeded eight cells: %q", wideLines)
		}
	}
	ultrawide := narrow
	ultrawide.width = 110
	ultrawideOutput := ansi.Strip(strings.Join(ultrawide.renderStatusBlock(4), "\n"))
	if !strings.Contains(ultrawideOutput, "███░░░░░░░░░ 21%") {
		t.Fatalf("ultrawide status did not use a twelve-cell bar: %q", ultrawideOutput)
	}
}

func TestStatusUsesOneCompactMetricRow(t *testing.T) {
	metrics := &provider.SessionMetrics{
		Branch: "main", TotalTokens: 129000, ContextWindow: 258000, ContextUsedPercent: 20,
		Quotas: []provider.QuotaWindow{{WindowMinutes: 10080, UsedPercent: 68, ResetsAt: time.Now().Add(3*time.Hour + time.Minute).Unix()}}, QuotaStatus: provider.QuotaAvailable,
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
		if strings.Contains(output, "Limit:") || strings.Contains(output, "5h:") || !strings.Contains(output, "7d:") || strings.Contains(output, "258k Ctx:") {
			t.Fatalf("width=%d status lost quota semantics: %q", width, output)
		}
		if width >= 32 && (!strings.Contains(output, "█") || !strings.Contains(output, "░")) {
			t.Fatalf("width=%d status lost progress bars: %q", width, output)
		}
		if width == 24 && (strings.Contains(output, "█") || strings.Contains(output, "░") || !strings.Contains(output, "Ctx: 20%")) {
			t.Fatalf("width=%d status did not fall back to percentages before hiding Ctx: %q", width, output)
		}
		if len(lines) != 2 {
			t.Fatalf("width=%d status expanded beyond header and metrics rows: %q", width, lines)
		}
	}

	medium := Model{width: 56, height: 20, noColor: true, snapshot: ipc.Snapshot{State: "live", Metrics: metrics}}
	mediumLines := medium.renderStatusBlock(4)
	if len(mediumLines) != 2 || strings.Contains(mediumLines[1], "Limit:") || !strings.Contains(mediumLines[1], "7d:") || !strings.Contains(mediumLines[1], "Ctx: 258k") {
		t.Fatalf("medium status did not use space freed by the unavailable quota: %q", mediumLines)
	}
	compact := medium
	compact.width = 32
	compactOutput := strings.Join(compact.renderStatusBlock(4), "\n")
	if cells := strings.Count(compactOutput, "█") + strings.Count(compactOutput, "░"); cells != 10 {
		t.Fatalf("compact status used %d progress cells, want two five-cell bars: %q", cells, compactOutput)
	}

	wide := medium
	wide.width = 80
	if lines := wide.renderStatusBlock(4); len(lines) != 2 || !strings.Contains(lines[1], " | Ctx: 258k") || !strings.Contains(lines[1], "(3h)") || strings.Contains(lines[1], "3h1m") {
		t.Fatalf("wide status did not combine semantic groups: %q", lines)
	}

	metrics.Quotas = append([]provider.QuotaWindow{{WindowMinutes: 300, UsedPercent: 0}}, metrics.Quotas...)
	zeroOutput := strings.Join(wide.renderStatusBlock(4), "\n")
	if !strings.Contains(zeroOutput, "5h:") || !strings.Contains(zeroOutput, "0%") {
		t.Fatalf("status hid an available zero-percent quota: %q", zeroOutput)
	}
	metrics.ContextUsedPercent = 0
	zeroOutput = strings.Join(wide.renderStatusBlock(4), "\n")
	if !strings.Contains(zeroOutput, "Ctx: 258k") || !strings.Contains(zeroOutput, "0%") || strings.Contains(zeroOutput, "Ctx: --") {
		t.Fatalf("status treated an available zero-percent context as unknown: %q", zeroOutput)
	}
}

func TestQuotaWindowLabelUsesActualDuration(t *testing.T) {
	for _, test := range []struct {
		minutes int64
		want    string
	}{
		{minutes: 30, want: "30m"},
		{minutes: 90, want: "90m"},
		{minutes: 300, want: "5h"},
		{minutes: 2880, want: "2d"},
		{minutes: 10080, want: "7d"},
	} {
		if got := quotaWindowLabel(test.minutes); got != test.want {
			t.Fatalf("quotaWindowLabel(%d) = %q, want %q", test.minutes, got, test.want)
		}
	}
}

func TestUnavailableQuotaStaysOutOfStatusAndHelpExplainsIt(t *testing.T) {
	for _, test := range []struct {
		language string
		helpText []string
	}{
		{language: config.InterfaceLanguageChinese, helpText: []string{"指标说明", "额度未显示", "不影响 Codex 使用"}},
		{language: config.InterfaceLanguageEnglish, helpText: []string{"Metrics", "Quota hidden", "Codex still works"}},
	} {
		model := Model{
			width:             80,
			noColor:           true,
			interfaceLanguage: test.language,
			snapshot: ipc.Snapshot{Metrics: &provider.SessionMetrics{
				QuotaStatus: provider.QuotaUnavailable,
			}},
		}
		if got := model.limitMetricText(8, false); got != "" {
			t.Fatalf("language=%s full status exposed unavailable quota: %q", test.language, got)
		}
		if got := model.compactMetrics(); got != "" {
			t.Fatalf("language=%s compact status exposed unavailable quota: %q", test.language, got)
		}
		help := strings.Join(model.helpLines(), "\n")
		for _, expected := range test.helpText {
			if !strings.Contains(help, expected) {
				t.Fatalf("language=%s Help omitted quota explanation %q: %q", test.language, expected, help)
			}
		}
	}

	model := Model{noColor: true, snapshot: ipc.Snapshot{Metrics: &provider.SessionMetrics{QuotaStatus: provider.QuotaAvailable}}}
	if got := model.limitMetricText(8, false); got != "" {
		t.Fatalf("known empty quota rendered feedback: %q", got)
	}

	for _, size := range [][2]int{{20, 6}, {24, 10}, {32, 12}, {48, 20}, {80, 24}} {
		model := Model{
			width: size[0], height: size[1], noColor: true, interfaceLanguage: config.InterfaceLanguageChinese,
			snapshot: ipc.Snapshot{
				State:   "live",
				Prompts: []provider.UserPrompt{{ID: "one", Text: "检查状态反馈"}},
				Metrics: &provider.SessionMetrics{ContextWindow: 128000, ContextUsedPercent: 25, QuotaStatus: provider.QuotaUnavailable},
			},
		}
		for index, line := range strings.Split(model.render(), "\n") {
			if got := ansi.StringWidth(line); got > size[0] {
				t.Fatalf("%dx%d line %d width = %d: %q", size[0], size[1], index, got, line)
			}
		}
		if output := model.render(); strings.Contains(output, "额度") || strings.Contains(output, "Quota") {
			t.Fatalf("%dx%d status exposed unavailable quota: %q", size[0], size[1], output)
		}
	}
}

func TestEnvironmentThemeCanPreviewButNotSave(t *testing.T) {
	model := Model{width: 48, height: 20, noColor: true, themeName: theme.Mocha, themeSource: config.ThemeEnvironment}
	model.applyTheme(theme.Mocha)
	model = openThemePage(model)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command != nil || model.themeMessage != theme.Environment+" is active" {
		t.Fatalf("environment theme was saveable or missed its source: %#v", model)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.overlay != overlaySettings || model.themeName != theme.Mocha {
		t.Fatalf("environment preview was not canceled: %#v", model)
	}
}

func TestEveryThemeEmphasizesSelectedPromptWithoutColoringItsIndex(t *testing.T) {
	for _, name := range theme.SelectableNames() {
		model := Model{width: 48, height: 12, selectedID: "two", snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: "first"}, {ID: "two", Text: "second"}}}}
		model.applyTheme(name)
		output := model.render()
		roles := theme.Derive(theme.Resolve(name, false))
		focus := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(roles.BodyText)).Render("  2 ")
		body := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(roles.BodyText)).Render("second")
		action := lipgloss.NewStyle().Foreground(lipgloss.Color(roles.Accent)).Render("[s] settings")
		if strings.Contains(output, "[LIVE]") || !strings.Contains(ansi.Strip(output), "  1 first") || !strings.Contains(output, focus+body) || !strings.Contains(output, action) {
			t.Fatalf("theme=%s roles were not shared by index, selection and help action: %q", name, output)
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
		width: 40, height: 12, noColor: true, overlay: overlayHelp,
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

func TestPromptFocusAndActiveStateUseSharedWholePromptEmphasis(t *testing.T) {
	for _, noColor := range []bool{false, true} {
		model := Model{width: 40, height: 12, noColor: noColor, selectedID: "one", snapshot: ipc.Snapshot{
			State: "live", ActiveTurnID: "turn", ActivePromptID: "two",
			Prompts: []provider.UserPrompt{{ID: "one", Text: "first"}, {ID: "two", Text: "second"}},
		}}
		lines := model.bodyLines()
		if got, want := lines[0], model.styleEmphasizedPrompt("  1 ")+model.styleEmphasizedPrompt("first"); got != want {
			t.Fatalf("noColor=%v focused prompt = %q, want whole-prompt emphasis %q", noColor, got, want)
		}
		multiline := model
		multiline.snapshot.Prompts = append([]provider.UserPrompt(nil), model.snapshot.Prompts...)
		multiline.snapshot.Prompts[0].Text = "first\ncontinued"
		if got, want := multiline.bodyLines()[1], multiline.styleEmphasizedPrompt("    ")+multiline.styleEmphasizedPrompt("continued"); got != want {
			t.Fatalf("noColor=%v focused continuation = %q, want whole-prompt emphasis %q", noColor, got, want)
		}
		if got, want := lines[2], model.styleEmphasizedPrompt("  2 ")+model.styleEmphasizedPrompt("second"); got != want {
			t.Fatalf("noColor=%v active prompt = %q, want whole-prompt emphasis %q", noColor, got, want)
		}

		model.selectedID = "two"
		lines = model.bodyLines()
		if got, want := lines[2], model.styleEmphasizedPrompt("  2 ")+model.styleEmphasizedPrompt("second"); got != want {
			t.Fatalf("noColor=%v combined prompt = %q, want composed focus and activity %q", noColor, got, want)
		}

		model.snapshot.ActiveTurnID = ""
		model.snapshot.ActivePromptID = ""
		lines = model.bodyLines()
		if got, want := lines[2], model.styleEmphasizedPrompt("  2 ")+model.styleEmphasizedPrompt("second"); got != want {
			t.Fatalf("noColor=%v completed selected prompt = %q, want focus without stale activity %q", noColor, got, want)
		}
	}
}

func TestPromptEntriesUseSymmetricNumberColumnAndBlankSpacing(t *testing.T) {
	model := Model{width: 40, height: 12, noColor: true, snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{
		{ID: "one", Text: "first"}, {ID: "two", Text: "second\ncontinued"},
	}}}
	want := []string{"  1 first", "", "  2 second", "    continued", ""}
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
	if model.selectedID != "one" || model.following {
		t.Fatalf("new prompt moved paused selection: selected=%q following=%v", model.selectedID, model.following)
	}
}

func TestNewPromptKeepsPausedViewportAtBottom(t *testing.T) {
	prompts := make([]provider.UserPrompt, 8)
	for index := range prompts {
		prompts[index] = provider.UserPrompt{ID: fmt.Sprintf("prompt-%d", index), Text: fmt.Sprintf("prompt-%d", index)}
	}
	model := Model{
		width: 40, height: 8, noColor: true, selectedID: "prompt-3",
		snapshot: ipc.Snapshot{State: "live", Prompts: prompts},
	}
	model.offset = model.maxOffset()
	previousOffset := model.offset

	updated, _ := model.Update(snapshotMsg{snapshot: ipc.Snapshot{
		State: "live", Prompts: append(prompts, provider.UserPrompt{ID: "prompt-8", Text: "prompt-8"}),
	}})
	model = updated.(Model)
	if model.maxOffset() <= previousOffset {
		t.Fatalf("new prompt did not extend the scroll range: old=%d new=%d", previousOffset, model.maxOffset())
	}
	if model.offset != model.maxOffset() || model.selectedID != "prompt-3" || model.following {
		t.Fatalf("new prompt did not keep the paused viewport at the bottom: %#v", model)
	}
}

func TestScrollingMovesOnlyTheViewport(t *testing.T) {
	prompts := make([]provider.UserPrompt, 16)
	for index := range prompts {
		prompts[index] = provider.UserPrompt{ID: fmt.Sprintf("prompt-%d", index), Text: fmt.Sprintf("prompt-%d", index)}
	}
	model := Model{
		width: 40, height: 8, noColor: true, following: true, selectedID: "prompt-15",
		snapshot: ipc.Snapshot{State: "live", Prompts: prompts},
	}
	model.offset = model.maxOffset()

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	model = updated.(Model)
	if model.offset == model.maxOffset() || model.selectedID != "prompt-15" || model.following {
		t.Fatalf("PgUp changed selection or failed to pause following: %#v", model)
	}
	pausedOffset := model.offset

	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	model = updated.(Model)
	if model.offset >= pausedOffset || model.selectedID != "prompt-15" || model.following {
		t.Fatalf("mouse wheel changed selection or failed to move the viewport: %#v", model)
	}

	model.scroll(1000)
	if model.offset != model.maxOffset() || model.selectedID != "prompt-15" || model.following {
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
	if model.offset != 0 || model.selectedID != "prompt-7" || model.following {
		t.Fatalf("new prompt disturbed an offscreen selection: %#v", model)
	}
}

func TestViewerPagesStartAtTop(t *testing.T) {
	model := Model{width: 40, height: 10, noColor: true, snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: "first"}}}}
	lines := strings.Split(model.render(), "\n")
	if !strings.Contains(ansi.Strip(lines[0]), "1 first") {
		t.Fatalf("prompt page did not start at the top: %q", model.render())
	}

	model = openHelpPage(model)
	lines = strings.Split(model.render(), "\n")
	if strings.TrimSpace(ansi.Strip(lines[0])) != "Help controls" {
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
	if body := ansi.Strip(strings.Join(model.bodyLines(), "\n")); !strings.Contains(body, "first-10") || !strings.Contains(body, "1 first-01") {
		t.Fatalf("restored selection was not rendered expanded: %q", body)
	}
}

func TestEmptySnapshotResetsSessionViewState(t *testing.T) {
	model := Model{
		width: 40, height: 20, noColor: true, selectedID: "one", offset: 3,
		expanded: map[string]bool{"one": true},
		snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: numberedLines("first", 10)}}},
	}
	updated, _ := model.Update(snapshotMsg{snapshot: ipc.Snapshot{
		State:  "live",
		Notice: "Session resumed. Showing new prompts only.",
	}})
	model = updated.(Model)
	if model.selectedID != "" || len(model.expanded) != 0 || model.offset != 0 || !model.following {
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

func TestStatusBarOmitsLifecycleStateCodes(t *testing.T) {
	for _, state := range []string{"ready", "live", "ended", "error"} {
		model := Model{width: 20, height: 6, noColor: true, snapshot: ipc.Snapshot{State: state}}
		output := model.render()
		for _, code := range []string{"[READY]", "[LIVE]", "[ENDED]", "[ERROR]"} {
			if strings.Contains(output, code) {
				t.Fatalf("state=%s exposed lifecycle code %q: %q", state, code, output)
			}
		}
	}
}

func TestExtremeNarrowFooterHidesActions(t *testing.T) {
	for _, noColor := range []bool{false, true} {
		for width := 1; width < 20; width++ {
			model := Model{width: width, height: 6, noColor: noColor, snapshot: ipc.Snapshot{State: "live"}}
			output := model.render()
			lines := strings.Split(output, "\n")
			if strings.Contains(output, "[s] settings") || strings.Contains(output, "latest") {
				t.Fatalf("color=%v width=%d narrow footer = %q", !noColor, width, output)
			}
			if strings.Contains(output, "[LIVE]") {
				t.Fatalf("color=%v width=%d exposed the normal live state: %q", !noColor, width, output)
			}
			for _, line := range lines {
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
	short := Model{width: 8, height: 3, noColor: true, snapshot: ipc.Snapshot{State: "live"}}
	shortLines := strings.Split(short.render(), "\n")
	if shortLines[len(shortLines)-1] != "" {
		t.Fatalf("short narrow footer = %q, want an empty live footer", shortLines[len(shortLines)-1])
	}
}

func TestExtremeNarrowNoticeWrapsWithoutTruncatingWords(t *testing.T) {
	tests := []struct {
		width int
		lines []string
	}{
		{width: 16, lines: []string{" Pane too narrow"}},
		{width: 15, lines: []string{" Pane too", " narrow"}},
		{width: 9, lines: []string{" Pane too", " narrow"}},
		{width: 8, lines: []string{" Pane", " too", " narrow"}},
	}
	for _, test := range tests {
		model := Model{width: test.width, height: 6, noColor: true, snapshot: ipc.Snapshot{State: "live"}}
		got := strings.Split(model.render(), "\n")
		if len(got) != model.height {
			t.Fatalf("width=%d rendered %d lines, want %d: %q", test.width, len(got), model.height, got)
		}
		for index, want := range test.lines {
			if got[index] != want {
				t.Fatalf("width=%d line %d = %q, want %q", test.width, index, got[index], want)
			}
		}
		if got[len(got)-1] != "" {
			t.Fatalf("width=%d footer = %q, want an empty live footer", test.width, got[len(got)-1])
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

func TestErrorNoticeRemainsVisibleWithExistingPrompts(t *testing.T) {
	for _, height := range []int{4, 6, 8, 12} {
		model := Model{width: 40, height: height, noColor: true, snapshot: ipc.Snapshot{
			State: "error", Notice: "Prompt stream disconnected",
			Prompts: []provider.UserPrompt{{ID: "one", Text: numberedLines("existing prompt", 12)}},
			Metrics: &provider.SessionMetrics{TotalTokens: 2400},
		}}
		output := model.render()
		if !strings.Contains(output, "Prompt stream disconnected") || strings.Contains(output, "Total:") || strings.Contains(output, "Metrics available after first response") {
			t.Fatalf("height=%d did not prioritize the actionable error notice: %q", height, output)
		}
		if height >= 6 && !strings.Contains(output, "[s] settings") {
			t.Fatalf("height=%d let the error notice displace settings: %q", height, output)
		}
		for _, line := range strings.Split(output, "\n") {
			if ansi.StringWidth(line) > model.width {
				t.Fatalf("height=%d error notice exceeded width: %q", height, line)
			}
		}
	}
}

func TestViewerHasNoBrandHeaderAndFooterOwnsStatus(t *testing.T) {
	model := Model{width: 40, height: 12, noColor: true, snapshot: ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "one", Text: "first"}}}}
	output := model.render()
	if strings.Contains(output, "Prompt Pane") || strings.Contains(output, "[LIVE]") || !strings.Contains(output, "[s] settings") {
		t.Fatalf("viewer chrome was not lightweight: %q", output)
	}
	if !strings.Contains(output, "Metrics available after first response") {
		t.Fatalf("status area did not explain pending metrics: %q", output)
	}
}

func TestBelowNoticeUsesLastVisiblePromptAndAlignsWithStatus(t *testing.T) {
	prompts := make([]provider.UserPrompt, 8)
	for index := range prompts {
		prompts[index] = provider.UserPrompt{ID: fmt.Sprintf("prompt-%d", index), Text: fmt.Sprintf("prompt-%d", index)}
	}
	model := Model{width: 48, height: 10, noColor: true, selectedID: "prompt-7", offset: 0,
		snapshot: ipc.Snapshot{State: "live", Prompts: prompts}}
	lines := strings.Split(model.render(), "\n")
	if got := ansi.Strip(lines[len(lines)-3]); got != " ↓ 4 prompts below" {
		t.Fatalf("below notice did not use the last visible prompt: %q", lines)
	}
	if !strings.HasPrefix(lines[len(lines)-3], " ") || strings.Contains(lines[len(lines)-2], "[LIVE]") {
		t.Fatalf("below notice and status were not left-aligned: %q", lines)
	}
	if strings.Contains(strings.Join(lines, "\n"), "End") {
		t.Fatalf("below notice exposed an unwanted End hint: %q", lines)
	}

	model.scroll(1000)
	if strings.Contains(model.render(), "below") {
		t.Fatalf("below notice remained visible at the bottom: %q", model.render())
	}
}

func TestBelowNoticeHandlesSingularContinuationAndCompactHeight(t *testing.T) {
	model := Model{width: 40, height: 8, noColor: true, selectedID: "two", offset: 0,
		snapshot: ipc.Snapshot{State: "live", Metrics: &provider.SessionMetrics{TotalTokens: 100}, Prompts: []provider.UserPrompt{
			{ID: "one", Text: numberedLines("first", 6)},
			{ID: "two", Text: "second"},
		}}}
	output := model.render()
	if !strings.Contains(output, "↓ 1 prompt below") || strings.Contains(output, "Total:") || strings.Contains(output, "[s] settings") || strings.Contains(output, "[LIVE]") {
		t.Fatalf("compact view did not prioritize the singular below notice and status: %q", output)
	}
	model.height = 4
	if output := model.render(); !strings.Contains(output, "↓ 1 prompt below") || strings.Contains(output, "T 100") || strings.Contains(output, "[s] settings") || strings.Contains(output, "[LIVE]") {
		t.Fatalf("short view did not prioritize the below notice and status: %q", output)
	}

	model.height = 8
	model.snapshot.Prompts = []provider.UserPrompt{{ID: "one", Text: numberedLines("first", 12)}}
	model.selectedID = "one"
	if output := model.render(); !strings.Contains(output, "↓ More below") {
		t.Fatalf("long visible prompt did not report remaining content: %q", output)
	}
}

func TestReadyStateRoutesTroubleshootingThroughHelp(t *testing.T) {
	model := Model{width: 48, height: 20, noColor: true, snapshot: ipc.Snapshot{State: "ready"}}
	if output := model.render(); strings.Contains(output, "/hooks") || !strings.Contains(output, "[s] settings") || strings.Contains(output, "troubleshoot") {
		t.Fatalf("ready view did not keep troubleshooting in help: %q", output)
	}

	model = openHelpPage(model)
	if output := model.render(); !strings.Contains(output, "Help") || !strings.Contains(output, "Connection") || !strings.Contains(output, "Submit your first prompt") || !strings.Contains(output, "/hooks") || !strings.Contains(output, "Trust Prompt Pane") || !strings.Contains(output, "Restart codex.pp") {
		t.Fatalf("ready help did not explain how to confirm the connection: %q", output)
	}

	updated, _ := model.Update(snapshotMsg{snapshot: ipc.Snapshot{State: "live"}})
	model = updated.(Model)
	model.overlay = overlayNone
	if output := model.render(); strings.Contains(output, "[LIVE]") || !strings.Contains(output, "[s] settings") || strings.Contains(output, "/hooks") || !strings.Contains(output, "Waiting for your first prompt") {
		t.Fatalf("live empty state retained ready diagnostics: %q", output)
	}
}

func TestReadyHelpSeparatesExplanationFromTroubleshootingSteps(t *testing.T) {
	for _, test := range []struct {
		width       int
		explanation string
		firstStep   string
	}{
		{width: 20, explanation: "   If it is missing:", firstStep: "   1. Open /hooks."},
		{width: 48, explanation: "   If it does not appear:", firstStep: "   1. Open /hooks."},
		{width: 80, explanation: "   If a prompt does not appear:", firstStep: "   1. Open /hooks in Codex."},
	} {
		model := Model{width: test.width, noColor: true, snapshot: ipc.Snapshot{State: "ready"}}
		lines := model.helpLines()
		if len(lines) < 2 || ansi.Strip(lines[0]) != " Connection" || lines[1] == "" {
			t.Fatalf("width=%d connection heading retained a blank line: %q", test.width, lines)
		}
		foundExplanation := false
		for index, line := range lines {
			if ansi.Strip(line) != test.explanation {
				continue
			}
			foundExplanation = true
			if index+1 >= len(lines) || ansi.Strip(lines[index+1]) != test.firstStep {
				t.Fatalf("width=%d troubleshooting explanation did not lead directly to the steps: %q", test.width, lines)
			}
			break
		}
		if !foundExplanation {
			t.Fatalf("width=%d troubleshooting explanation was missing: %q", test.width, lines)
		}
	}
}

func TestReadyTroubleshootingIsResponsive(t *testing.T) {
	for _, size := range [][2]int{{20, 6}, {24, 10}, {32, 12}, {48, 20}, {80, 24}} {
		model := Model{width: size[0], height: size[1], noColor: true, snapshot: ipc.Snapshot{State: "ready"}}
		footer := model.renderFooter()
		settingsAction := "[s] settings"
		if size[0] == 20 {
			settingsAction = "[s]"
		}
		if !strings.Contains(footer, settingsAction) || strings.Contains(footer, "troubleshoot") {
			t.Fatalf("%dx%d ready footer did not keep the stable settings entry: %q", size[0], size[1], footer)
		}

		model = openHelpPage(model)
		foundHooks := false
		for {
			output := model.render()
			foundHooks = foundHooks || strings.Contains(output, "/hooks")
			for _, line := range strings.Split(output, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("%dx%d troubleshooting line exceeded width: %q", size[0], size[1], line)
				}
			}
			previous := model.overlayOffset
			model.scrollOverlay(model.bodyHeight())
			if model.overlayOffset == previous {
				break
			}
		}
		if !foundHooks {
			t.Fatalf("%dx%d troubleshooting never exposed /hooks", size[0], size[1])
		}
	}
}

func TestReadyStateNeverShowsAutomaticDiagnostics(t *testing.T) {
	for _, size := range [][2]int{{20, 6}, {24, 10}, {32, 12}, {48, 20}, {80, 24}} {
		model := Model{width: size[0], height: size[1], noColor: true, snapshot: ipc.Snapshot{State: "ready"}}
		output := model.render()
		normalized := strings.Join(strings.Fields(ansi.Strip(output)), " ")
		if !strings.Contains(normalized, "Waiting for your first prompt") || strings.Contains(output, "/hooks") || strings.Contains(output, "Restart codex.pp") {
			t.Fatalf("%dx%d ready state implied a connection failure: %q", size[0], size[1], output)
		}
		for _, line := range strings.Split(output, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("%dx%d ready state exceeded width: %q", size[0], size[1], line)
			}
		}
	}
}
