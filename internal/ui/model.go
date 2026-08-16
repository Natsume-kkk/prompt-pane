package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Natsume-kkk/prompt-pane/internal/config"
	"github.com/Natsume-kkk/prompt-pane/internal/ipc"
	"github.com/Natsume-kkk/prompt-pane/internal/provider"
	"github.com/Natsume-kkk/prompt-pane/internal/theme"
)

var (
	csiPattern = regexp.MustCompile("\\x1b\\[[0-?]*[ -/]*[@-~]")
	oscPattern = regexp.MustCompile("\\x1b\\][^\\x07]*(?:\\x07|\\x1b\\\\)")
)

const (
	collapsedLineLimit    = 8
	collapsedVisibleLines = 6
)

type snapshotMsg struct {
	snapshot ipc.Snapshot
}

type streamEndedMsg struct{}

type themeSavedMsg struct{ name string }
type themeSaveFailedMsg struct{ err error }

type promptRange struct {
	start int
	end   int
	long  bool
}

type bodyLayout struct {
	lines   []string
	prompts []promptRange
}

type textPoint struct {
	x int
	y int
}

type Model struct {
	decoder         *json.Decoder
	snapshot        ipc.Snapshot
	width           int
	height          int
	offset          int
	following       bool
	newCount        int
	noColor         bool
	selectedID      string
	expanded        map[string]bool
	showHelp        bool
	showTheme       bool
	helpOffset      int
	themeName       string
	themeSource     config.ThemeSource
	themeIndex      int
	themeOriginal   string
	themeMessage    string
	lightBackground bool
	colors          theme.Roles
	closeViewer     bool
	pendingClick    bool
	pendingClickAt  textPoint
	selecting       bool
	dragging        bool
	textSelected    bool
	selectionStart  textPoint
	selectionEnd    textPoint
}

func New(decoder *json.Decoder) Model {
	_, noColor := os.LookupEnv("NO_COLOR")
	themeName, source, err := config.LoadTheme()
	if err != nil {
		themeName, source = theme.Auto, config.ThemeDefault
	}
	model := Model{
		decoder: decoder, following: true, noColor: noColor,
		expanded: make(map[string]bool), snapshot: ipc.Snapshot{State: "ready"},
		themeName: themeName, themeSource: source,
	}
	model.applyTheme(themeName)
	return model
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(readSnapshot(m.decoder), func() tea.Msg { return tea.RequestBackgroundColor() })
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.BackgroundColorMsg:
		m.lightBackground = !msg.IsDark()
		m.applyTheme(m.themeName)
	case tea.WindowSizeMsg:
		m.resetPendingClick()
		m.resetTextSelection()
		m.width = msg.Width
		m.height = msg.Height
		m.clampOffset()
		m.clampHelpOffset()
	case tea.KeyPressMsg:
		m.resetPendingClick()
		m.resetTextSelection()
		key := msg.String()
		if key == "ctrl+x" {
			m.closeViewer = true
			return m, tea.Quit
		}
		if m.showHelp {
			if m.showTheme {
				switch key {
				case "up", "k":
					m.moveTheme(-1)
				case "down", "j":
					m.moveTheme(1)
				case "enter":
					if m.themeSource == config.ThemeEnvironment {
						m.themeMessage = theme.Environment + " is active"
						return m, nil
					}
					name := theme.Names()[m.themeIndex]
					return m, saveTheme(name)
				case "esc", "h", "t":
					m.applyTheme(m.themeOriginal)
					m.showTheme = false
					m.themeMessage = ""
				}
				return m, nil
			}
			switch key {
			case "h", "esc":
				m.showHelp = false
				m.helpOffset = 0
			case "t":
				m.openThemePicker()
			case "up":
				m.scrollHelp(-1)
			case "down":
				m.scrollHelp(1)
			case "pgup":
				m.scrollHelp(-m.bodyHeight())
			case "pgdown":
				m.scrollHelp(m.bodyHeight())
			}
			return m, nil
		}
		switch key {
		case "h":
			m.showHelp = true
			m.helpOffset = 0
		case "up", "k":
			m.moveSelection(-1)
		case "down", "j":
			m.moveSelection(1)
		case "pgup":
			m.scroll(-m.bodyHeight())
		case "pgdown":
			m.scroll(m.bodyHeight())
		case "home":
			m.selectPrompt(0)
		case "end":
			m.selectPrompt(len(m.snapshot.Prompts) - 1)
		case "enter":
			m.togglePrompt(m.displaySelectedIndex())
		case "c":
			if m.anyExpanded() {
				m.expanded = make(map[string]bool)
				m.clampOffset()
			}
		}
	case tea.MouseClickMsg:
		m.resetTextSelection()
		m.resetPendingClick()
		if msg.Button != tea.MouseLeft {
			break
		}
		m.pendingClick = true
		m.pendingClickAt = textPoint{x: msg.X, y: msg.Y}
		m.beginTextSelection(msg.X, msg.Y)
	case tea.MouseMotionMsg:
		if m.pendingClick && (msg.X != m.pendingClickAt.x || msg.Y != m.pendingClickAt.y) {
			m.resetPendingClick()
		}
		if m.selecting && msg.Button == tea.MouseLeft {
			m.extendTextSelection(msg.X, msg.Y)
		}
	case tea.MouseReleaseMsg:
		click := m.pendingClick && msg.X == m.pendingClickAt.x && msg.Y == m.pendingClickAt.y
		m.resetPendingClick()
		if m.selecting {
			m.extendTextSelection(msg.X, msg.Y)
			m.selecting = false
			if m.dragging {
				selected := m.selectedText()
				if selected == "" {
					m.resetTextSelection()
					break
				}
				m.textSelected = true
				return m, tea.SetClipboard(selected)
			}
		}
		m.resetTextSelection()
		if click && !m.showHelp {
			m.selectPromptAtRow(msg.Y)
		}
	case tea.MouseWheelMsg:
		m.resetPendingClick()
		m.resetTextSelection()
		if m.showHelp {
			if msg.Button == tea.MouseWheelUp {
				m.scrollHelp(-3)
			} else if msg.Button == tea.MouseWheelDown {
				m.scrollHelp(3)
			}
		} else if msg.Button == tea.MouseWheelUp {
			m.scroll(-3)
		} else if msg.Button == tea.MouseWheelDown {
			m.scroll(3)
		}
	case snapshotMsg:
		m.resetPendingClick()
		m.resetTextSelection()
		added := len(msg.snapshot.Prompts) - len(m.snapshot.Prompts)
		wasFollowing := m.following
		m.snapshot = msg.snapshot
		if len(m.snapshot.Prompts) == 0 {
			m.selectedID = ""
			m.expanded = make(map[string]bool)
			m.offset = 0
			m.following = true
			m.newCount = 0
		} else if wasFollowing {
			m.selectedID = m.snapshot.Prompts[len(m.snapshot.Prompts)-1].ID
			m.offset = m.maxOffset()
			m.newCount = 0
		} else {
			if m.selectedIndex() < 0 {
				m.selectedID = m.snapshot.Prompts[len(m.snapshot.Prompts)-1].ID
			}
			m.clampOffset()
			if added > 0 {
				m.newCount += added
			}
		}
		readCmd := readSnapshot(m.decoder)
		return m, readCmd
	case themeSavedMsg:
		m.themeName = msg.name
		m.themeSource = config.ThemeConfig
		m.applyTheme(msg.name)
		m.showTheme = false
		m.themeMessage = ""
	case themeSaveFailedMsg:
		m.applyTheme(m.themeOriginal)
		m.themeMessage = "Could not save theme: " + msg.err.Error()
	case streamEndedMsg:
		m.resetPendingClick()
		m.resetTextSelection()
		if m.snapshot.State != "ended" {
			m.snapshot.State = "error"
			m.snapshot.Notice = "Prompt stream disconnected"
		}
	}
	return m, nil
}

func (m Model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

func (m Model) CloseRequested() bool {
	return m.closeViewer
}

func readSnapshot(decoder *json.Decoder) tea.Cmd {
	return func() tea.Msg {
		var snapshot ipc.Snapshot
		if err := decoder.Decode(&snapshot); err != nil {
			return streamEndedMsg{}
		}
		return snapshotMsg{snapshot: snapshot}
	}
}

func saveTheme(name string) tea.Cmd {
	return func() tea.Msg {
		if err := config.SaveTheme(name); err != nil {
			return themeSaveFailedMsg{err: err}
		}
		return themeSavedMsg{name: name}
	}
}

func (m *Model) applyTheme(name string) {
	m.themeName = name
	m.colors = theme.Derive(theme.Resolve(name, m.lightBackground))
}

func (m *Model) openThemePicker() {
	m.showTheme = true
	m.themeOriginal = m.themeName
	m.themeMessage = ""
	for index, name := range theme.Names() {
		if name == m.themeName {
			m.themeIndex = index
			break
		}
	}
}

func (m *Model) moveTheme(delta int) {
	names := theme.Names()
	m.themeIndex = (m.themeIndex + delta + len(names)) % len(names)
	m.applyTheme(names[m.themeIndex])
	m.themeMessage = ""
}

func (m *Model) scroll(delta int) {
	previous := m.offset
	m.offset += delta
	if m.offset < 0 {
		m.offset = 0
	}
	max := m.maxOffset()
	if m.offset > max {
		m.offset = max
	}
	if m.offset == previous {
		return
	}
	m.following = false
}

func (m *Model) clampOffset() {
	if m.following || m.offset > m.maxOffset() {
		m.offset = m.maxOffset()
	}
}

func (m *Model) scrollHelp(delta int) {
	m.helpOffset += delta
	m.clampHelpOffset()
}

func (m *Model) clampHelpOffset() {
	if m.helpOffset < 0 {
		m.helpOffset = 0
	}
	if maximum := m.helpMaxOffset(); m.helpOffset > maximum {
		m.helpOffset = maximum
	}
}

func (m Model) helpMaxOffset() int {
	maximum := len(m.helpLines()) - m.bodyHeight()
	return max(0, maximum)
}

func (m *Model) moveSelection(delta int) bool {
	if len(m.snapshot.Prompts) == 0 {
		return false
	}
	index := m.selectedIndex()
	if index < 0 {
		index = len(m.snapshot.Prompts) - 1
	}
	next := index + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.snapshot.Prompts) {
		next = len(m.snapshot.Prompts) - 1
	}
	return m.selectPrompt(next)
}

func (m *Model) selectPrompt(index int) bool {
	if index < 0 || index >= len(m.snapshot.Prompts) {
		return false
	}
	changed := m.selectedID != m.snapshot.Prompts[index].ID
	m.selectedID = m.snapshot.Prompts[index].ID
	m.following = index == len(m.snapshot.Prompts)-1
	if m.following {
		m.newCount = 0
		m.offset = m.maxOffset()
		return changed
	}
	m.revealPrompt(index)
	return changed
}

func (m *Model) selectPromptAtRow(row int) int {
	if m.width < 20 || row < 0 || row >= m.bodyHeight() || len(m.snapshot.Prompts) == 0 {
		return -1
	}
	layout := m.layoutBody()
	line := m.offset + row
	for index, prompt := range layout.prompts {
		end := prompt.end
		if index < len(layout.prompts)-1 {
			end++
		}
		if line < prompt.start || line >= end {
			continue
		}
		if index == len(m.snapshot.Prompts)-1 {
			m.selectPrompt(index)
			return index
		}
		m.selectedID = m.snapshot.Prompts[index].ID
		m.following = false
		return index
	}
	return -1
}

func (m *Model) togglePrompt(index int) bool {
	if index < 0 || !m.promptIsLong(index) {
		return false
	}
	id := m.snapshot.Prompts[index].ID
	m.selectedID = id
	m.copyExpanded()
	m.expanded[id] = !m.expanded[id]
	m.clampOffset()
	m.revealPrompt(index)
	return true
}

func (m *Model) resetPendingClick() {
	m.pendingClick = false
	m.pendingClickAt = textPoint{}
}

func (m *Model) beginTextSelection(x, y int) {
	point, ok := m.textSelectionPoint(x, y, false)
	if !ok {
		return
	}
	m.selecting = true
	m.selectionStart = point
	m.selectionEnd = point
}

func (m *Model) extendTextSelection(x, y int) {
	point, ok := m.textSelectionPoint(x, y, true)
	if !ok {
		return
	}
	m.selectionEnd = point
	if point != m.selectionStart {
		m.dragging = true
	}
}

func (m *Model) resetTextSelection() {
	m.selecting = false
	m.dragging = false
	m.textSelected = false
	m.selectionStart = textPoint{}
	m.selectionEnd = textPoint{}
}

func (m Model) textSelectionPoint(x, y int, clamp bool) (textPoint, bool) {
	if m.width < 20 || m.bodyHeight() < 1 {
		return textPoint{}, false
	}
	if clamp {
		x = min(max(x, 0), m.width-1)
		y = min(max(y, 0), m.bodyHeight()-1)
	} else if x < 0 || x >= m.width || y < 0 || y >= m.bodyHeight() {
		return textPoint{}, false
	}
	lines := m.visibleBodyLines()
	if !clamp && (y >= len(lines) || x >= ansi.StringWidth(lines[y])) {
		return textPoint{}, false
	}
	return textPoint{x: x, y: y}, true
}

func (m Model) selectionBounds() (textPoint, textPoint) {
	start := m.selectionStart
	end := m.selectionEnd
	if end.y < start.y || end.y == start.y && end.x < start.x {
		start, end = end, start
	}
	return start, end
}

func (m Model) selectedText() string {
	lines := m.visibleBodyLines()
	start, end := m.selectionBounds()
	if start.y < 0 || end.y >= len(lines) {
		return ""
	}
	selected := make([]string, 0, end.y-start.y+1)
	for row := start.y; row <= end.y; row++ {
		left, right := m.selectionColumns(lines[row], row)
		selected = append(selected, ansi.Strip(ansi.Cut(lines[row], left, right)))
	}
	return strings.Join(selected, "\n")
}

func (m Model) selectionColumns(line string, row int) (int, int) {
	start, end := m.selectionBounds()
	left := 0
	right := ansi.StringWidth(line)
	if row == start.y {
		left = start.x
	}
	if row == end.y {
		right = min(right, end.x+1)
	}
	return snapTextRange(line, left, right)
}

func snapTextRange(line string, left, right int) (int, int) {
	width := ansi.StringWidth(line)
	left = min(max(left, 0), width)
	right = min(max(right, left), width)
	plain := ansi.Strip(line)
	cell := 0
	for len(plain) > 0 {
		cluster, clusterWidth := ansi.FirstGraphemeCluster(plain, ansi.GraphemeWidth)
		next := cell + clusterWidth
		if left > cell && left < next {
			left = cell
		}
		if right > cell && right < next {
			right = next
		}
		cell = next
		plain = plain[len(cluster):]
	}
	return left, right
}

func (m Model) selectedIndex() int {
	for index, prompt := range m.snapshot.Prompts {
		if prompt.ID == m.selectedID {
			return index
		}
	}
	return -1
}

func (m Model) displaySelectedIndex() int {
	if index := m.selectedIndex(); index >= 0 {
		return index
	}
	if len(m.snapshot.Prompts) > 0 {
		return len(m.snapshot.Prompts) - 1
	}
	return -1
}

func (m *Model) revealPrompt(index int) {
	layout := m.layoutBody()
	if index < 0 || index >= len(layout.prompts) {
		return
	}
	prompt := layout.prompts[index]
	height := m.bodyHeight()
	if prompt.start < m.offset {
		m.offset = prompt.start
	} else if prompt.end > m.offset+height {
		if prompt.end-prompt.start > height {
			m.offset = prompt.start
		} else {
			m.offset = prompt.end - height
		}
	}
	m.clampOffset()
}

func (m Model) maxOffset() int {
	lines := m.bodyLines()
	max := len(lines) - m.bodyHeight()
	if max < 0 {
		return 0
	}
	return max
}

func (m Model) bodyHeight() int {
	height := m.height
	if m.showHelp {
		if m.height >= 3 {
			height--
		}
	} else if m.snapshot.Metrics != nil {
		if m.height >= 10 {
			height -= 4
		} else if m.height >= 6 {
			height -= 3
		} else if m.height >= 3 {
			height--
		}
	} else if m.height >= 8 {
		height -= 2
	} else if m.height >= 3 {
		height--
	}
	if height < 1 {
		return 1
	}
	return height
}

func (m Model) render() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if m.width < 20 {
		lines := []string{" Pane too narrow"}
		for len(lines) < max(1, m.height-1) {
			lines = append(lines, "")
		}
		if m.height >= 2 {
			lines = append(lines, m.renderFooter(true))
		}
		return fitLines(lines, m.width, m.height)
	}

	visible := m.visibleBodyLines()
	if m.textSelected || m.selecting && m.dragging {
		for row := range visible {
			visible[row] = m.renderTextSelection(visible[row], row)
		}
	}

	lines := visible
	if m.showHelp && m.height >= 3 {
		lines = append(lines, m.renderFooter(m.height < 8))
	} else if m.snapshot.Metrics != nil && m.height >= 10 {
		status := m.renderStatusLines()
		lines = append(lines, "", status[0], status[1], m.renderFooter(false))
	} else if m.snapshot.Metrics != nil && m.height >= 6 {
		status := m.renderStatusLines()
		lines = append(lines, status[0], status[1], m.renderFooter(true))
	} else if m.height >= 8 {
		lines = append(lines, "", m.renderFooter(false))
	} else if m.height >= 3 {
		lines = append(lines, m.renderFooter(true))
	}
	return fitLines(lines, m.width, m.height)
}

func (m Model) visibleBodyLines() []string {
	body := m.bodyLines()
	start := m.offset
	if m.showTheme {
		body = m.themeLines()
		selectedLine := 3 + m.themeIndex
		start = max(0, selectedLine-m.bodyHeight()+1)
	} else if m.showHelp {
		body = m.helpLines()
		start = m.helpOffset
	}
	end := min(len(body), start+m.bodyHeight())
	visible := append([]string(nil), body[start:end]...)
	for len(visible) < m.bodyHeight() {
		visible = append(visible, "")
	}
	return visible
}

func (m Model) renderTextSelection(line string, row int) string {
	start, end := m.selectionBounds()
	if row < start.y || row > end.y {
		return line
	}
	left, right := m.selectionColumns(line, row)
	if right <= left {
		return line
	}
	prefix := ansi.Cut(line, 0, left)
	selected := ansi.Strip(ansi.Cut(line, left, right))
	suffix := ansi.Cut(line, right, ansi.StringWidth(line))
	selectionStyle := lipgloss.NewStyle().Reverse(true)
	if !m.noColor {
		// Explicit cell colors avoid reverse-video continuation artifacts when
		// a selection ends on a double-width grapheme behind a multiplexer.
		colors := m.visualRoles()
		selectionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colors.Selection)).Background(lipgloss.Color(colors.Cell))
	}
	return prefix + selectionStyle.Render(selected) + suffix
}

func (m Model) renderFooter(compactHeight bool) string {
	left := " " + m.styleState("["+stateLabel(m.snapshot.State)+"]")
	if m.width < 20 {
		return left
	}

	actions := "h help"
	if m.snapshot.State == "ready" && m.width >= 32 {
		actions = "h troubleshoot"
	}
	if m.showTheme {
		actions = "↑↓ preview · Enter save · Esc cancel"
		if compactHeight || m.width < 44 {
			actions = "↑↓ · Enter · Esc"
		}
	} else if m.showHelp {
		actions = "↑↓ scroll · Esc close"
		if compactHeight || m.width < 32 {
			actions = "↑↓ · Esc"
		}
	} else if m.snapshot.Metrics != nil && m.height < 6 {
		actions = m.compactMetrics()
	} else if m.newCount > 0 {
		actions = fmt.Sprintf("%d new · End latest", m.newCount)
		if compactHeight || m.width < 32 {
			actions = fmt.Sprintf("%d new · End", m.newCount)
		}
	}
	right := m.styleMuted(actions + " ")
	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		right = ""
		gap = max(0, m.width-ansi.StringWidth(left))
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) compactMetrics() string {
	metrics := m.snapshot.Metrics
	if metrics == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	if metrics.TotalTokens > 0 {
		parts = append(parts, "T "+compactNumber(metrics.TotalTokens))
	}
	if metrics.FiveHour != nil {
		parts = append(parts, fmt.Sprintf("5h %.0f%%", metrics.FiveHour.UsedPercent))
	}
	if metrics.SevenDay != nil {
		parts = append(parts, fmt.Sprintf("7d %.0f%%", metrics.SevenDay.UsedPercent))
	}
	return strings.Join(parts, " · ")
}

func (m Model) helpLines() []string {
	entries := make([]string, 0, 20)
	if m.snapshot.State == "ready" {
		entries = append(entries,
			" Troubleshoot",
			"",
			" Hook is not confirmed yet.",
			" First prompt may start the session.",
			" If a prompt does not appear:",
			" 1. Run /hooks in Codex.",
			" 2. Review Prompt Pane.",
			" 3. Restart codex.pp.",
			"",
			" Shortcuts",
			"",
		)
	} else {
		entries = append(entries, " Help", "")
	}
	if m.width < 32 {
		entries = append(entries,
			" Ctrl+X  Close pane",
			" h/Esc   Close help",
			" ↑/k     Previous",
			" ↓/j     Next",
			" PgUp    Page up",
			" PgDn    Page down",
			" Home    First",
			" End     Latest",
			" Enter   Expand/fold",
			" Drag    Copy text",
			" c       Fold all",
			" t       Theme",
		)
	} else {
		entries = append(entries,
			helpEntry("Ctrl+X", "Close viewer pane"),
			helpEntry("h/Esc", "Close help"),
			helpEntry("↑/k", "Previous prompt"),
			helpEntry("↓/j", "Next prompt"),
			helpEntry("PgUp/PgDn", "Scroll page"),
			helpEntry("Home", "First prompt"),
			helpEntry("End", "Latest prompt"),
			helpEntry("Enter", "Expand or fold"),
			helpEntry("Drag", "Copy visible text"),
			helpEntry("c", "Fold all"),
			helpEntry("t", "Choose theme"),
		)
	}
	entries = append(entries, "", " Theme: "+m.themeName)
	for index := range entries {
		entries[index] = m.styleMuted(entries[index])
	}
	return entries
}

func (m Model) themeLines() []string {
	lines := []string{" Theme", "", " Choose a global color theme:"}
	for index, name := range theme.Names() {
		marker := "  "
		if index == m.themeIndex {
			marker = "› "
		}
		label := marker + name
		if m.noColor {
			if index == m.themeIndex {
				label = lipgloss.NewStyle().Bold(true).Render(label)
			}
		} else {
			palette := theme.Resolve(name, m.lightBackground)
			label = lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Sapphire)).Render(label)
		}
		lines = append(lines, " "+label)
	}
	if m.themeSource == config.ThemeEnvironment {
		lines = append(lines, "", m.styleWarning(" "+theme.Environment+" overrides saved settings"))
	} else if m.themeMessage != "" {
		lines = append(lines, "", m.styleWarning(" "+m.themeMessage))
	}
	return lines
}

func helpEntry(key, description string) string {
	return fmt.Sprintf(" %-9s  %s", key, description)
}

func (m Model) renderStatusLines() [2]string {
	if m.snapshot.Metrics == nil || m.width < 24 {
		return [2]string{}
	}
	metrics := m.snapshot.Metrics
	colors := m.visualRoles()
	available := max(1, m.width-1)
	project := m.renderProject(metrics)
	total := ""
	if metrics.TotalTokens > 0 {
		total = m.styleColor("Total: "+compactNumber(metrics.TotalTokens), colors.Token)
	}
	model := ""
	if metrics.Model != "" {
		label := metrics.Model
		if metrics.Effort != "" {
			label += " " + metrics.Effort
		}
		model = m.styleColor("Model: "+label, colors.Error)
	}
	line1 := fitStatusPieces(available, []string{project, total, model}, []int{2, 0})

	line2 := m.renderLimitLine(true, true)
	if ansi.StringWidth(line2) > available {
		line2 = m.renderLimitLine(false, true)
	}
	if ansi.StringWidth(line2) > available {
		line2 = m.renderLimitLine(false, false)
	}
	if ansi.StringWidth(line2) > available {
		line2 = ansi.Truncate(line2, available, "")
	}
	return [2]string{prefixStatus(line1), prefixStatus(line2)}
}

func (m Model) renderProject(metrics *provider.SessionMetrics) string {
	if metrics.Project == "" {
		return ""
	}
	colors := m.visualRoles()
	project := m.styleColor("["+metrics.Project+"]", colors.Project)
	if !m.noColor {
		project = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colors.Project)).Render("[" + metrics.Project + "]")
	}
	if metrics.Branch == "" {
		return project
	}
	git := m.styleColor(metrics.Branch, colors.Branch)
	if metrics.Added > 0 {
		git += " " + m.styleColor(fmt.Sprintf("+%d", metrics.Added), colors.Added)
	}
	if metrics.Deleted > 0 {
		git += " " + m.styleColor(fmt.Sprintf("-%d", metrics.Deleted), colors.Deleted)
	}
	if metrics.Untracked > 0 {
		git += " " + m.styleColor(fmt.Sprintf("?%d", metrics.Untracked), colors.Untracked)
	}
	return project + "(" + git + ")"
}

func (m Model) renderLimitLine(bars, contextVisible bool) string {
	metrics := m.snapshot.Metrics
	if metrics == nil {
		return ""
	}
	colors := m.visualRoles()
	pieces := make([]string, 0, 3)
	if metrics.FiveHour != nil {
		pieces = append(pieces, m.renderQuota("5h", metrics.FiveHour, bars))
	}
	if metrics.SevenDay != nil {
		pieces = append(pieces, m.renderQuota("7d", metrics.SevenDay, bars))
	}
	if contextVisible && metrics.ContextUsedPercent > 0 {
		label := "Ctx"
		if metrics.ContextWindow > 0 {
			label = compactNumber(metrics.ContextWindow) + " Ctx"
		}
		pieces = append(pieces, m.styleColor(label+" ", colors.Label)+m.renderPercent(metrics.ContextUsedPercent, bars))
	}
	if len(pieces) > 0 && (metrics.FiveHour != nil || metrics.SevenDay != nil) {
		pieces[0] = m.styleColor("Limit: ", colors.Label) + pieces[0]
	}
	return strings.Join(pieces, " | ")
}

func (m Model) renderQuota(label string, quota *provider.QuotaWindow, bars bool) string {
	text := m.styleColor(label+" ", m.visualRoles().Label) + m.renderPercent(quota.UsedPercent, bars)
	if bars && quota.ResetsAt > time.Now().Unix() && m.width >= 100 {
		text += m.styleMuted(" (reset " + formatDuration(quota.ResetsAt-time.Now().Unix()) + ")")
	}
	return text
}

func (m Model) renderPercent(percent float64, bars bool) string {
	percent = max(0, min(100, percent))
	color := quotaColor(percent, m.visualRoles())
	if !bars {
		return m.styleColor(fmt.Sprintf("%.0f%%", percent), color)
	}
	const width = 8
	filled := int(percent/100*width + 0.5)
	bar := m.styleColor(strings.Repeat("█", filled), color)
	empty := strings.Repeat("░", width-filled)
	if percent > 0 {
		empty = m.styleColor(empty, color)
	}
	return bar + empty + " " + m.styleColor(fmt.Sprintf("%.0f%%", percent), color)
}

func fitStatusPieces(width int, pieces []string, removalOrder []int) string {
	active := append([]string(nil), pieces...)
	join := func() string {
		filtered := make([]string, 0, len(active))
		for _, piece := range active {
			if piece != "" {
				filtered = append(filtered, piece)
			}
		}
		return strings.Join(filtered, " | ")
	}
	for _, index := range removalOrder {
		if ansi.StringWidth(join()) <= width {
			break
		}
		active[index] = ""
	}
	return ansi.Truncate(join(), width, "")
}

func prefixStatus(line string) string {
	if line == "" {
		return ""
	}
	return " " + line
}

func compactNumber(value int64) string {
	if value >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(value)/1_000_000)
	}
	if value < 1000 {
		return fmt.Sprintf("%d", value)
	}
	return fmt.Sprintf("%.0fk", float64(value)/1000)
}

func formatDuration(seconds int64) string {
	if seconds >= 86400 {
		return fmt.Sprintf("%dd%dh", seconds/86400, seconds%86400/3600)
	}
	if seconds >= 3600 {
		return fmt.Sprintf("%dh%dm", seconds/3600, seconds%3600/60)
	}
	return fmt.Sprintf("%dm", seconds/60)
}

func quotaColor(percent float64, colors theme.Roles) string {
	if percent >= 80 {
		return colors.Error
	}
	if percent >= 50 {
		return colors.Warning
	}
	return colors.Success
}

func (m Model) visualRoles() theme.Roles {
	if m.colors.Accent != "" {
		return m.colors
	}
	return theme.Derive(theme.Resolve(theme.Mocha, false))
}

func (m Model) styleColor(text, color string) string {
	if m.noColor {
		return text
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(text)
}

func (m Model) styleWarning(text string) string {
	return m.styleColor(text, m.visualRoles().Warning)
}

func (m Model) styleState(label string) string {
	if m.noColor {
		return label
	}
	switch m.snapshot.State {
	case "live":
		return m.styleColor(label, m.visualRoles().Success)
	case "ready":
		return m.styleColor(label, m.visualRoles().Success)
	case "error":
		return m.styleColor(label, m.visualRoles().Error)
	default:
		return m.styleMuted(label)
	}
}

func (m Model) styleMuted(text string) string {
	if m.noColor {
		return text
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(m.visualRoles().Muted)).Render(text)
}

func (m Model) styleSelected(text string) string {
	style := lipgloss.NewStyle().Bold(m.noColor)
	if !m.noColor {
		style = style.Foreground(lipgloss.Color(m.visualRoles().Accent))
	}
	return style.Render(text)
}

func (m Model) styleNotice(text string) string {
	switch m.snapshot.State {
	case "error":
		if m.noColor {
			return text
		}
		return m.styleColor(text, m.visualRoles().Error)
	case "live":
		return text
	default:
		return m.styleMuted(text)
	}
}

func (m Model) bodyLines() []string {
	return m.layoutBody().lines
}

func (m Model) layoutBody() bodyLayout {
	if len(m.snapshot.Prompts) == 0 {
		notice := stateNotice(m.snapshot.State)
		if m.snapshot.Notice != "" {
			notice = m.snapshot.Notice
		}
		lines := wrapText(notice, max(1, m.width-2))
		for index := range lines {
			lines[index] = m.styleNotice(" " + lines[index])
		}
		return bodyLayout{lines: lines}
	}

	digits := max(2, len(fmt.Sprintf("%d", len(m.snapshot.Prompts))))
	prefixWidth := digits + 2
	textWidth := m.width - 1 - prefixWidth
	if textWidth < 1 {
		textWidth = 1
	}
	selected := m.displaySelectedIndex()
	layout := bodyLayout{prompts: make([]promptRange, 0, len(m.snapshot.Prompts))}
	for index, prompt := range m.snapshot.Prompts {
		if index > 0 {
			layout.lines = append(layout.lines, "")
		}
		wrapped := wrapText(sanitize(prompt.Text), textWidth)
		isLong := len(wrapped) > collapsedLineLimit
		summaryLine := -1
		if isLong && !m.expanded[prompt.ID] {
			hidden := len(wrapped) - collapsedVisibleLines
			wrapped = append(wrapped[:collapsedVisibleLines], foldSummary(hidden, textWidth, index == selected))
			summaryLine = len(wrapped) - 1
		}
		start := len(layout.lines)
		for lineIndex, line := range wrapped {
			prefix := " " + strings.Repeat(" ", digits) + " "
			if lineIndex == 0 {
				prefix = fmt.Sprintf(" %*d ", digits, index+1)
			}
			if index == selected {
				if lineIndex == summaryLine {
					layout.lines = append(layout.lines, m.styleSelected(prefix)+m.styleMuted(line))
				} else {
					layout.lines = append(layout.lines, m.styleSelected(prefix+line))
				}
				continue
			}
			if lineIndex == summaryLine {
				line = m.styleMuted(line)
			}
			layout.lines = append(layout.lines, m.styleMuted(prefix)+line)
		}
		layout.prompts = append(layout.prompts, promptRange{start: start, end: len(layout.lines), long: isLong})
	}
	return layout
}

func foldSummary(hidden, width int, selected bool) string {
	full := fmt.Sprintf("… +%d lines", hidden)
	candidates := []string{full, fmt.Sprintf("… +%d", hidden)}
	if selected {
		candidates = []string{
			full + " · Enter expand",
			fmt.Sprintf("… +%d · Enter", hidden),
			fmt.Sprintf("… +%d", hidden),
		}
	}
	for _, candidate := range candidates {
		if ansi.StringWidth(candidate) <= width {
			return candidate
		}
	}
	return ansi.Truncate(candidates[len(candidates)-1], max(1, width), "")
}

func (m Model) promptIsLong(index int) bool {
	if m.width < 20 || index < 0 || index >= len(m.snapshot.Prompts) {
		return false
	}
	layout := m.layoutBody()
	return index < len(layout.prompts) && layout.prompts[index].long
}

func (m Model) anyExpanded() bool {
	for _, prompt := range m.snapshot.Prompts {
		if m.expanded[prompt.ID] {
			return true
		}
	}
	return false
}

func (m *Model) copyExpanded() {
	expanded := make(map[string]bool, len(m.expanded)+1)
	for id, value := range m.expanded {
		expanded[id] = value
	}
	m.expanded = expanded
}

func stateLabel(state string) string {
	switch state {
	case "ready":
		return "READY"
	case "live":
		return "LIVE"
	case "ended":
		return "ENDED"
	default:
		return "ERROR"
	}
}

func stateNotice(state string) string {
	switch state {
	case "ready":
		return "Waiting for your first prompt"
	case "ended":
		return "Session ended"
	case "error":
		return "Prompt stream unavailable"
	default:
		return "Waiting for your first prompt"
	}
}

func sanitize(text string) string {
	text = oscPattern.ReplaceAllString(text, "")
	text = csiPattern.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var result strings.Builder
	for _, r := range text {
		switch r {
		case '\n':
			result.WriteRune(r)
		case '\t':
			result.WriteString("    ")
		default:
			if !unicode.IsControl(r) {
				result.WriteRune(r)
			}
		}
	}
	return result.String()
}

func wrapText(text string, width int) []string {
	if width < 1 {
		return []string{""}
	}
	masked, marker := maskEdgeSpaces(text)
	wrapped := ansi.Wrap(masked, width, "")
	if marker != "" {
		wrapped = strings.ReplaceAll(wrapped, marker, " ")
	}
	return strings.Split(wrapped, "\n")
}

func maskEdgeSpaces(text string) (string, string) {
	// Word wrapping discards boundary spaces, so protect explicit line-edge
	// spaces with an unused printable one-cell marker until wrapping finishes.
	marker := '\ue000'
	for marker <= '\uf8ff' && strings.ContainsRune(text, marker) {
		marker++
	}
	if marker > '\uf8ff' {
		return text, ""
	}

	lines := strings.Split(text, "\n")
	for index, line := range lines {
		start := 0
		for start < len(line) && line[start] == ' ' {
			start++
		}
		end := len(line)
		for end > start && line[end-1] == ' ' {
			end--
		}
		if start == 0 && end == len(line) {
			continue
		}
		lines[index] = strings.Repeat(string(marker), start) + line[start:end] + strings.Repeat(string(marker), len(line)-end)
	}
	return strings.Join(lines, "\n"), string(marker)
}

func fitLines(lines []string, width, height int) string {
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		if ansi.StringWidth(line) > width {
			lines[i] = ansi.Truncate(line, width, "")
		}
	}
	return strings.Join(lines, "\n")
}
