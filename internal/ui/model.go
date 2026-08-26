package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Natsume-kkk/prompt-pane/internal/config"
	"github.com/Natsume-kkk/prompt-pane/internal/ipc"
	"github.com/Natsume-kkk/prompt-pane/internal/provider"
	"github.com/Natsume-kkk/prompt-pane/internal/theme"
	appversion "github.com/Natsume-kkk/prompt-pane/internal/version"
	"github.com/Natsume-kkk/prompt-pane/internal/zellij"
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
	lines        []string
	prompts      []promptRange
	activityLine int
}

type textPoint struct {
	x int
	y int
}

type overlayPage uint8

const (
	overlayNone overlayPage = iota
	overlaySettings
	overlayHelp
	overlayAbout
	overlayTheme
)

type Model struct {
	decoder           *json.Decoder
	snapshot          ipc.Snapshot
	width             int
	height            int
	offset            int
	following         bool
	noColor           bool
	selectedID        string
	expanded          map[string]bool
	overlay           overlayPage
	settingsIndex     int
	settingsMessage   string
	overlayOffset     int
	themeName         string
	themeSource       config.ThemeSource
	themeIndex        int
	themeOriginal     string
	themeMessage      string
	interfaceLanguage string
	reducedMotion     bool
	activity          activityViewState
	planner           activityPlanner
	lightBackground   bool
	backgroundColor   string
	colors            theme.Roles
	closeViewer       bool
	pendingClick      bool
	pendingClickAt    textPoint
	selecting         bool
	dragging          bool
	textSelected      bool
	selectionStart    textPoint
	selectionEnd      textPoint
}

func New(decoder *json.Decoder) Model {
	_, noColor := os.LookupEnv("NO_COLOR")
	themeName, source, err := config.LoadTheme()
	if err != nil {
		themeName, source = theme.Auto, config.ThemeDefault
	}
	interfaceLanguage, _ := config.LoadInterfaceLanguage()
	_, reducedMotion := os.LookupEnv("PROMPT_PANE_REDUCED_MOTION")
	model := Model{
		decoder: decoder, following: true, noColor: noColor,
		expanded: make(map[string]bool), snapshot: ipc.Snapshot{State: "ready"},
		themeName: themeName, themeSource: source, interfaceLanguage: interfaceLanguage,
		reducedMotion: reducedMotion, planner: newActivityPlanner(),
	}
	model.applyTheme(themeName)
	return model
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		readSnapshot(m.decoder),
		func() tea.Msg { return tea.RequestBackgroundColor() },
	)
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.BackgroundColorMsg:
		m.lightBackground = !msg.IsDark()
		m.backgroundColor = theme.ColorHex(msg.Color)
		m.applyTheme(m.themeName)
	case tea.WindowSizeMsg:
		m.resetPendingClick()
		m.resetTextSelection()
		m.width = msg.Width
		m.height = msg.Height
		m.clampOffset()
		m.clampOverlayOffset()
		if m.overlay == overlaySettings {
			m.revealSetting()
		}
	case tea.KeyPressMsg:
		m.resetPendingClick()
		m.resetTextSelection()
		key := msg.String()
		if key == "ctrl+x" {
			m.closeViewer = true
			return m, tea.Quit
		}
		switch m.overlay {
		case overlayHelp, overlayAbout:
			switch key {
			case "s", "esc":
				m.returnToSettings()
			case "up", "pgup":
				m.scrollOverlay(-m.bodyHeight())
			case "down", "pgdown":
				m.scrollOverlay(m.bodyHeight())
			}
			return m, nil
		case overlayTheme:
			switch key {
			case "s", "esc":
				m.cancelThemePreview()
				m.returnToSettings()
			case "up":
				m.moveTheme(-1)
				m.revealTheme()
			case "down":
				m.moveTheme(1)
				m.revealTheme()
			case "enter":
				if m.themeSource == config.ThemeEnvironment {
					m.themeMessage = theme.Environment + m.uiText(" 已启用", " is active")
					return m, nil
				}
				name := theme.SelectableNames()[m.themeIndex]
				return m, saveTheme(name)
			case "pgup":
				m.scrollOverlay(-m.bodyHeight())
			case "pgdown":
				m.scrollOverlay(m.bodyHeight())
			}
			return m, nil
		case overlaySettings:
			switch key {
			case "s", "esc":
				m.overlay = overlayNone
				m.overlayOffset = 0
			case "up", "k":
				m.moveSetting(-1)
			case "down", "j":
				m.moveSetting(1)
			case "enter":
				return m.activateSetting()
			}
			return m, nil
		}
		switch key {
		case "s":
			m.overlay = overlaySettings
			m.overlayOffset = 0
			m.revealSetting()
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
		if click && m.overlay == overlayTheme {
			m.selectThemeAtRow(msg.Y)
		} else if click && m.overlay == overlaySettings {
			m.selectSettingAtRow(msg.Y)
		} else if click && m.overlay == overlayNone {
			m.selectPromptAtRow(msg.Y)
		}
	case tea.MouseWheelMsg:
		m.resetPendingClick()
		m.resetTextSelection()
		if m.overlayActive() {
			if msg.Button == tea.MouseWheelUp {
				m.scrollOverlay(-3)
			} else if msg.Button == tea.MouseWheelDown {
				m.scrollOverlay(3)
			}
		} else if msg.Button == tea.MouseWheelUp {
			m.scroll(-3)
		} else if msg.Button == tea.MouseWheelDown {
			m.scroll(3)
		}
	case snapshotMsg:
		m.resetPendingClick()
		m.resetTextSelection()
		previousSnapshot := m.snapshot
		wasFollowing := m.following
		wasAtBottom := m.offset >= m.maxOffset()
		promptAdded := len(msg.snapshot.Prompts) > len(m.snapshot.Prompts)
		previousActivePromptID := activePromptID(previousSnapshot)
		m.snapshot = msg.snapshot
		m.clampOverlayOffset()
		if len(m.snapshot.Prompts) == 0 {
			m.selectedID = ""
			m.expanded = make(map[string]bool)
			m.offset = 0
			m.following = true
		} else if wasFollowing {
			m.selectedID = m.snapshot.Prompts[len(m.snapshot.Prompts)-1].ID
			m.offset = m.maxOffset()
		} else {
			if m.selectedIndex() < 0 {
				m.selectedID = m.snapshot.Prompts[len(m.snapshot.Prompts)-1].ID
			}
			if promptAdded && wasAtBottom {
				m.offset = m.maxOffset()
			}
			m.clampOffset()
		}
		commands := []tea.Cmd{readSnapshot(m.decoder)}
		currentActivePromptID := activePromptID(m.snapshot)
		if currentActivePromptID != previousActivePromptID {
			if currentActivePromptID != "" {
				commands = append(commands, m.beginActivity(currentActivePromptID))
			} else if previousActivePromptID != "" {
				settle := m.snapshot.State == "live" && samePromptSequence(previousSnapshot.Prompts, m.snapshot.Prompts)
				if command := m.endActivity(settle); command != nil {
					commands = append(commands, command)
				}
			}
		} else if currentActivePromptID == "" && m.activity.visible &&
			(m.snapshot.State != "live" || !samePromptSequence(previousSnapshot.Prompts, m.snapshot.Prompts)) {
			m.endActivity(false)
		}
		return m, tea.Batch(commands...)
	case activityStartMsg:
		if !m.activityMessageCurrent(msg.promptID, msg.generation) {
			break
		}
		m.activity.visible = true
		m.activity.settling = false
		m.activity.frame = 0
		m.activity.changes = 0
		m.activity.phrase = m.planner.beginTurn(m.interfaceLanguage)
		commands := []tea.Cmd{activityPhraseCommand(msg.promptID, msg.generation, activityPhraseDelay(&m.planner, 0, m.reducedMotion))}
		if !m.reducedMotion {
			commands = append(commands, activityFrameCommand(msg.promptID, msg.generation))
		}
		return m, tea.Batch(commands...)
	case activityFrameMsg:
		if !m.activityMessageCurrent(msg.promptID, msg.generation) || !m.activity.visible || m.activity.settling || m.reducedMotion {
			break
		}
		m.activity.frame = (m.activity.frame + 1) % len(activityDotFrames)
		return m, activityFrameCommand(msg.promptID, msg.generation)
	case activityPhraseMsg:
		if !m.activityMessageCurrent(msg.promptID, msg.generation) || !m.activity.visible || m.activity.settling {
			break
		}
		m.activity.changes++
		m.activity.phrase = m.planner.nextPhrase(m.interfaceLanguage)
		return m, activityPhraseCommand(msg.promptID, msg.generation, activityPhraseDelay(&m.planner, m.activity.changes, m.reducedMotion))
	case activityClearMsg:
		if msg.generation == m.activity.generation && m.activity.settling {
			m.activity = activityViewState{generation: msg.generation}
		}
	case interfaceLanguageSavedMsg:
		m.interfaceLanguage = msg.language
		m.settingsMessage = ""
		if m.activity.visible && !m.activity.settling && activePromptID(m.snapshot) != "" {
			m.activity.phrase = m.planner.beginTurn(m.interfaceLanguage)
			m.activity.changes = 0
		}
	case interfaceLanguageSaveFailedMsg:
		m.settingsMessage = m.uiText("无法保存语言设置：", "Could not save language: ") + msg.err.Error()
	case themeSavedMsg:
		m.themeName = msg.name
		m.themeSource = config.ThemeConfig
		m.applyTheme(msg.name)
		m.themeOriginal = msg.name
		m.themeMessage = ""
		m.returnToSettings()
	case themeSaveFailedMsg:
		m.applyTheme(m.themeOriginal)
		m.syncThemeIndex(m.themeOriginal)
		m.themeMessage = m.uiText("无法保存主题：", "Could not save theme: ") + msg.err.Error()
	case streamEndedMsg:
		m.resetPendingClick()
		m.resetTextSelection()
		if m.snapshot.State != "ended" {
			m.snapshot.State = "error"
			m.snapshot.Notice = "Prompt stream disconnected"
		}
		m.snapshot.ActiveTurnID = ""
		m.snapshot.ActivePromptID = ""
		m.activity.generation++
		m.activity = activityViewState{generation: m.activity.generation}
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

func (m *Model) beginThemePreview() {
	m.themeOriginal = m.themeName
	m.themeMessage = ""
	previewName := m.themeName
	if previewName == theme.Auto {
		previewName = theme.Resolve(theme.Auto, m.lightBackground).Name
		m.applyTheme(previewName)
	}
	m.syncThemeIndex(previewName)
}

func (m *Model) syncThemeIndex(name string) {
	for index, candidate := range theme.SelectableNames() {
		if candidate == name {
			m.themeIndex = index
			return
		}
	}
}

func (m *Model) cancelThemePreview() {
	m.applyTheme(m.themeOriginal)
	m.themeMessage = ""
}

func (m *Model) returnToSettings() {
	m.overlay = overlaySettings
	m.overlayOffset = 0
	m.revealSetting()
}

func (m *Model) moveSetting(delta int) {
	settingCount := len(m.settingLabels())
	m.settingsIndex = (m.settingsIndex + delta + settingCount) % settingCount
	m.settingsMessage = ""
	m.revealSetting()
}

func (m Model) activateSetting() (tea.Model, tea.Cmd) {
	switch m.settingsIndex {
	case 0:
		m.overlay = overlayTheme
		m.overlayOffset = 0
		m.beginThemePreview()
	case 1:
		language := config.InterfaceLanguageEnglish
		if m.interfaceLanguage == config.InterfaceLanguageEnglish {
			language = config.InterfaceLanguageChinese
		}
		return m, saveInterfaceLanguage(language)
	case 2:
		m.overlay = overlayHelp
		m.overlayOffset = 0
	case 3:
		m.overlay = overlayAbout
		m.overlayOffset = 0
	}
	return m, nil
}

func (m Model) settingLabels() []string {
	return []string{
		m.uiText("主题", "Theme"),
		m.uiText("界面语言", "Language"),
		m.uiText("帮助", "Help"),
		m.uiText("关于", "About"),
	}
}

func (m *Model) selectSettingAtRow(row int) int {
	if m.overlay != overlaySettings || row < 0 || row >= m.bodyHeight() {
		return -1
	}
	line := m.overlayOffset + row
	lines := m.settingsLines()
	if line < 0 || line >= len(lines) {
		return -1
	}
	text := strings.TrimSpace(ansi.Strip(lines[line]))
	text = strings.TrimPrefix(text, "› ")
	for index, label := range m.settingLabels() {
		if strings.HasPrefix(text, label) {
			m.settingsIndex = index
			m.settingsMessage = ""
			return index
		}
	}
	return -1
}

func (m *Model) revealSetting() {
	labels := m.settingLabels()
	if m.settingsIndex < 0 || m.settingsIndex >= len(labels) {
		return
	}
	target := labels[m.settingsIndex]
	selected := -1
	for index, line := range m.settingsLines() {
		text := strings.TrimSpace(ansi.Strip(line))
		text = strings.TrimPrefix(text, "› ")
		if strings.HasPrefix(text, target) {
			selected = index
			break
		}
	}
	if selected < 0 {
		return
	}
	m.revealOverlayLine(selected)
}

func (m *Model) moveTheme(delta int) {
	names := theme.SelectableNames()
	m.themeIndex = (m.themeIndex + delta + len(names)) % len(names)
	m.applyTheme(names[m.themeIndex])
	m.themeMessage = ""
}

func (m *Model) revealTheme() {
	selected := -1
	for index, line := range m.themeLines() {
		if strings.Contains(ansi.Strip(line), "› "+m.themeName) {
			selected = index
			break
		}
	}
	if selected < 0 {
		return
	}
	m.revealOverlayLine(selected)
}

func (m *Model) revealOverlayLine(selected int) {
	height := m.bodyHeight()
	if selected < m.overlayOffset {
		m.overlayOffset = selected
	} else if selected >= m.overlayOffset+height {
		m.overlayOffset = selected - height + 1
	}
	m.clampOverlayOffset()
}

func (m *Model) selectThemeAtRow(row int) int {
	if m.overlay != overlayTheme || row < 0 || row >= m.bodyHeight() {
		return -1
	}
	line := m.overlayOffset + row
	lines := m.themeLines()
	if line < 0 || line >= len(lines) {
		return -1
	}
	text := strings.TrimSpace(ansi.Strip(lines[line]))
	for index, name := range theme.SelectableNames() {
		if text == name || strings.HasPrefix(text, name+" ") || strings.HasPrefix(text, "› "+name) {
			m.themeIndex = index
			m.applyTheme(name)
			m.themeMessage = ""
			m.revealTheme()
			return index
		}
	}
	return -1
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

func (m *Model) scrollOverlay(delta int) {
	m.overlayOffset += delta
	m.clampOverlayOffset()
}

func (m *Model) clampOverlayOffset() {
	if m.overlayOffset < 0 {
		m.overlayOffset = 0
	}
	if maximum := m.overlayMaxOffset(); m.overlayOffset > maximum {
		m.overlayOffset = maximum
	}
}

func (m Model) overlayMaxOffset() int {
	maximum := len(m.overlayLines()) - m.bodyHeight()
	return max(0, maximum)
}

func (m Model) overlayActive() bool {
	return m.overlay != overlayNone
}

func (m Model) overlayLines() []string {
	switch m.overlay {
	case overlayTheme:
		return m.themeLines()
	case overlayHelp:
		return m.helpLines()
	case overlayAbout:
		return m.aboutLines()
	case overlaySettings:
		return m.settingsLines()
	default:
		return nil
	}
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
		if line < prompt.start || line >= prompt.end {
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
	if !m.overlayActive() && m.visibleActivityRow(y) {
		return textPoint{}, false
	}
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
		if !m.overlayActive() && m.visibleActivityRow(row) {
			selected = append(selected, "")
			continue
		}
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

func (m Model) promptsBelow(bodyHeight int) (int, bool) {
	return m.promptsBelowLayout(m.layoutBody(), bodyHeight)
}

func (m Model) promptsBelowLayout(layout bodyLayout, bodyHeight int) (int, bool) {
	if m.overlayActive() || m.following || m.width < 20 || len(m.snapshot.Prompts) == 0 {
		return 0, false
	}
	maximum := max(0, len(layout.lines)-bodyHeight)
	offset := min(max(0, m.offset), maximum)
	if offset >= maximum {
		return 0, false
	}
	viewportEnd := offset + bodyHeight
	lastVisible := -1
	for index, prompt := range layout.prompts {
		if prompt.start < viewportEnd && prompt.end > offset {
			lastVisible = index
		}
	}
	if lastVisible < 0 {
		return len(layout.prompts), true
	}
	return len(layout.prompts) - lastVisible - 1, true
}

func (m Model) belowNotice() string {
	return m.belowNoticeLayout(m.layoutBody(), m.bodyHeight())
}

func (m Model) belowNoticeLayout(layout bodyLayout, bodyHeight int) string {
	count, visible := m.promptsBelowLayout(layout, bodyHeight)
	if !visible {
		return ""
	}
	return " " + m.styleAction(m.belowText(count))
}

func (m Model) viewerNoticeLayout(layout bodyLayout, bodyHeight int) string {
	if m.snapshot.State == "error" && len(m.snapshot.Prompts) > 0 {
		notice := m.stateNotice("error")
		if m.snapshot.Notice != "" {
			notice = m.localizedNotice(m.snapshot.Notice)
		}
		return " " + m.styleNotice(notice)
	}
	return m.belowNoticeLayout(layout, bodyHeight)
}

func (m Model) hasViewerErrorNotice() bool {
	return m.snapshot.State == "error" && len(m.snapshot.Prompts) > 0
}

func (m Model) bodyHeight() int {
	height := m.height
	if m.overlayActive() {
		if m.height >= 3 {
			height--
		}
	} else {
		if m.height >= 10 {
			height -= len(m.renderStatusBlock(2)) + 1
		} else if m.height >= 6 {
			reserved := len(m.renderStatusBlock(2))
			if m.hasViewerErrorNotice() {
				reserved = max(reserved, 2)
			}
			height -= reserved
		} else if m.height >= 3 {
			height--
			if m.hasViewerErrorNotice() {
				height--
			} else if _, visible := m.promptsBelow(max(1, height)); visible {
				height--
			}
		}
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
		padding := ""
		noticeWidth := m.width
		if m.width > 1 {
			padding = " "
			noticeWidth--
		}
		lines := strings.Split(wrapMixedText(m.uiText("窗格太窄", "Pane too narrow"), noticeWidth), "\n")
		for index := range lines {
			lines[index] = padding + lines[index]
		}
		bodyRows := max(1, m.height-1)
		if len(lines) > bodyRows {
			lines = lines[:bodyRows]
		}
		for len(lines) < bodyRows {
			lines = append(lines, "")
		}
		if m.height >= 2 {
			lines = append(lines, m.renderFooter())
		}
		return fitLines(lines, m.width, m.height)
	}

	bodyHeight := m.bodyHeight()
	fullStatus := []string(nil)
	if !m.overlayActive() && m.height >= 6 {
		fullStatus = m.renderStatusBlock(2)
		bodyHeight = m.height - len(fullStatus)
		if m.height >= 10 {
			bodyHeight--
		} else if m.hasViewerErrorNotice() {
			bodyHeight--
		}
		bodyHeight = max(1, bodyHeight)
	}

	start := m.offset
	body := []string(nil)
	notice := ""
	if m.overlayActive() {
		body = m.overlayLines()
		start = m.overlayOffset
	} else {
		layout := m.layoutBody()
		body = layout.lines
		notice = m.viewerNoticeLayout(layout, bodyHeight)
	}
	visible := visibleLines(body, start, bodyHeight)
	if m.textSelected || m.selecting && m.dragging {
		for row := range visible {
			visible[row] = m.renderTextSelection(visible[row], row)
		}
	}

	lines := visible
	if m.overlayActive() && m.height >= 3 {
		lines = append(lines, m.renderFooter())
	} else if m.height >= 10 {
		lines = append(lines, notice)
		lines = append(lines, fullStatus...)
	} else if m.height >= 6 {
		if notice != "" {
			lines = append(lines, notice)
			lines = append(lines, m.renderStatusBlock(1)...)
		} else {
			lines = append(lines, fullStatus...)
		}
	} else if m.height >= 3 {
		if notice != "" {
			lines = append(lines, notice)
		}
		lines = append(lines, m.renderFooter())
	}
	return fitLines(lines, m.width, m.height)
}

func (m Model) visibleBodyLines() []string {
	body := m.bodyLines()
	start := m.offset
	if m.overlayActive() {
		body = m.overlayLines()
		start = m.overlayOffset
	}
	return visibleLines(body, start, m.bodyHeight())
}

func visibleLines(body []string, start, height int) []string {
	height = max(0, height)
	start = min(max(0, start), len(body))
	end := min(len(body), start+height)
	visible := append([]string(nil), body[start:end]...)
	for len(visible) < height {
		visible = append(visible, "")
	}
	return visible
}

func (m Model) renderTextSelection(line string, row int) string {
	start, end := m.selectionBounds()
	if row < start.y || row > end.y {
		return line
	}
	if !m.overlayActive() && m.visibleActivityRow(row) {
		return line
	}
	left, right := m.selectionColumns(line, row)
	if right <= left {
		return line
	}
	prefix := ansi.Cut(line, 0, left)
	selected := ansi.Strip(ansi.Cut(line, left, right))
	suffix := ansi.Cut(line, right, ansi.StringWidth(line))
	return prefix + m.styleTextSelection(selected) + suffix
}

func (m Model) styleTextSelection(text string) string {
	selectionStyle := lipgloss.NewStyle().Reverse(true)
	if !m.noColor {
		// Explicit cell colors avoid reverse-video continuation artifacts when
		// a selection ends on a double-width grapheme behind a multiplexer.
		colors := m.visualRoles()
		selectionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colors.SelectionText)).Background(lipgloss.Color(colors.SelectionSurface))
		if m.backgroundColor != "" && !theme.MeetsContrast(colors.SelectionSurface, m.backgroundColor, theme.MinimumSelectionBoundaryContrast) {
			selectionStyle = selectionStyle.Underline(true)
		}
	}
	return selectionStyle.Render(text)
}

func (m Model) renderFooter() string {
	left := ""
	if m.width < 20 {
		return left
	}

	actions := m.settingsAction()
	switch m.overlay {
	case overlayHelp:
		label, multiplePages := m.overlayPageInfo(m.uiText("帮助", "Help"))
		left = " " + m.styleAction(label)
		actions = m.overlayScrollActions(multiplePages)
	case overlayAbout:
		label, multiplePages := m.overlayPageInfo("")
		left = ""
		if label != "" {
			left = " " + m.styleAction(label)
		}
		actions = m.overlayScrollActions(multiplePages)
	case overlayTheme:
		label, _ := m.overlayPageInfo("")
		left = ""
		if label != "" {
			left = " " + m.styleAction(label)
		}
		actions = m.uiText("↑/↓ 预览 · Enter 保存 · Esc 取消", "↑/↓ preview · Enter save · Esc cancel")
		if m.width < 52 {
			actions = m.uiText("↑/↓ · Enter 保存 · Esc 取消", "↑/↓ · Enter save · Esc cancel")
		}
		if m.width < 38 {
			actions = "↑/↓ · Enter · Esc"
		}
		if m.width < 26 {
			actions = "↑↓ Enter Esc"
		}
	case overlaySettings:
		left = ""
		actions = m.uiText("Esc 关闭", "Esc close")
	default:
		if m.belowNotice() != "" && !m.hasViewerErrorNotice() {
			actions = ""
		} else if m.snapshot.Metrics != nil && m.height < 6 {
			actions = m.compactMetrics()
		}
	}
	right := m.styleAction(actions + " ")
	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		right = ""
		gap = max(0, m.width-ansi.StringWidth(left))
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) overlayPageInfo(name string) (string, bool) {
	separator := " "
	if name != "" && m.width < 26 {
		name = string([]rune(name)[0])
		if m.chineseUI() {
			separator = ""
		}
	}
	height := max(1, m.bodyHeight())
	total := max(1, (len(m.overlayLines())+height-1)/height)
	if total == 1 {
		return name, false
	}
	current := min(total, m.overlayOffset/height+1)
	if m.overlayOffset == m.overlayMaxOffset() {
		current = total
	}
	position := fmt.Sprintf("%d/%d", current, total)
	if name == "" {
		return position, true
	}
	return name + separator + position, true
}

func (m Model) overlayScrollActions(multiplePages bool) string {
	if !multiplePages {
		return m.uiText("Esc 返回", "Esc back")
	}
	actions := m.uiText("↑/↓ 翻页 · PgUp/PgDn · Esc 返回", "↑/↓ scroll · PgUp/PgDn · Esc back")
	if m.width < 56 {
		actions = m.uiText("↑/↓ 翻页 · Esc 返回", "↑/↓ scroll · Esc back")
	}
	if m.width < 32 {
		actions = "↑/↓ · Esc"
	}
	if m.width < 26 {
		actions = "↑↓ Esc"
	}
	return actions
}

func (m Model) compactMetrics() string {
	metrics := m.snapshot.Metrics
	if metrics == nil {
		return ""
	}
	parts := make([]string, 0, 1+len(metrics.Quotas))
	if metrics.TotalTokens > 0 {
		parts = append(parts, "T "+compactNumber(metrics.TotalTokens))
	}
	for _, quota := range metrics.Quotas {
		parts = append(parts, fmt.Sprintf("%s %.0f%%", quotaWindowLabel(quota.WindowMinutes), quota.UsedPercent))
	}
	return strings.Join(parts, " · ")
}

func (m Model) settingsLines() []string {
	entries := []string{m.styleAction(" " + m.uiText("设置", "Settings"))}
	entries = append(entries, "")
	labels := m.settingLabels()
	values := []string{m.themeName, m.languageLabel(m.interfaceLanguage), "", ""}
	labelWidth := ansi.StringWidth(labels[1])
	for index, label := range labels {
		marker := "  "
		if index == m.settingsIndex {
			marker = "› "
		}
		line := " " + marker + label
		if values[index] != "" {
			if m.width >= 32 {
				line = " " + marker + padRightCells(label, labelWidth) + "  " + values[index]
			} else {
				line += "  " + values[index]
			}
		}
		if index == m.settingsIndex {
			line = m.styleSettingSelection(line)
		}
		entries = append(entries, line)
	}
	if m.settingsMessage != "" {
		entries = append(entries, "", m.styleError("   "+m.settingsMessage))
	}
	return entries
}

func (m Model) helpLines() []string {
	entries := []string{}
	connectionHeading := m.uiText(" 连接", " Connection")
	helpControlsHeading := m.uiText(" 帮助页操作", " Help controls")
	settingsControlsHeading := m.uiText(" 设置页操作", " Settings controls")
	promptControlsHeading := m.uiText(" 提示词操作 · 帮助页外", " Prompt controls · outside Help")
	metricsHeading := m.uiText(" 指标说明", " Metrics")
	gitHeading := m.uiText(" Git 状态", " Git status")
	paneHeading := m.uiText(" 窗格操作", " Pane controls")
	helpScrollAction := m.uiText("整页滚动帮助", "Scroll help by page")
	settingsEnterAction := m.uiText("打开页面或切换语言", "Open page or switch language")
	settingsCloseAction := m.uiText("关闭设置", "Close settings")
	if m.width < 52 {
		helpScrollAction = m.uiText("翻页", "Page scroll")
		settingsEnterAction = m.uiText("打开/切换", "Open/switch")
		settingsCloseAction = m.uiText("关闭", "Close")
	}
	if m.width < 24 {
		promptControlsHeading = m.uiText(" 提示词 · 页外", " Prompt · outside")
	} else if m.width < 32 {
		promptControlsHeading = m.uiText(" 提示词 · 帮助页外", " Prompt · outside Help")
	}
	if m.snapshot.State == "ready" {
		entries = append(entries, connectionHeading)
		if m.width < 32 {
			entries = append(entries,
				m.uiText("   先提交提示词。", "   Submit a prompt."),
				m.uiText("   若没有显示：", "   If it is missing:"),
				m.uiText("   1. 打开 /hooks。", "   1. Open /hooks."),
				m.uiText("   2. 信任它。", "   2. Trust it."),
				m.uiText("   3. 重启", "   3. Restart"),
				"      codex.pp",
				"",
			)
		} else if m.width < 52 {
			entries = append(entries,
				m.uiText("   先提交第一条提示词。", "   Submit your first prompt."),
				m.uiText("   若没有显示：", "   If it does not appear:"),
				m.uiText("   1. 打开 /hooks。", "   1. Open /hooks."),
				m.uiText("   2. 信任 Prompt Pane。", "   2. Trust Prompt Pane."),
				m.uiText("   3. 重启 codex.pp", "   3. Restart codex.pp"),
				"",
			)
		} else {
			entries = append(entries,
				m.uiText("   先提交第一条提示词。", "   Submit your first prompt."),
				m.uiText("   若提示词没有显示：", "   If a prompt does not appear:"),
				m.uiText("   1. 在 Codex 中打开 /hooks。", "   1. Open /hooks in Codex."),
				m.uiText("   2. 信任 Prompt Pane。", "   2. Trust Prompt Pane."),
				m.uiText("   3. 重启 codex.pp。", "   3. Restart codex.pp."),
				"",
			)
		}
	}
	if m.width < 32 {
		entries = append(entries,
			helpControlsHeading,
			m.uiText("   ↑/↓    翻页", "   ↑/↓    Scroll"),
			m.uiText("   PgUp/PgDn 翻页", "   PgUp/PgDn Scroll"),
			m.uiText("   Esc    返回", "   Esc    Back"),
			m.uiText("   Ctrl+X 关闭窗格", "   Ctrl+X Close pane"),
			"",
			settingsControlsHeading,
			m.uiText("   ↑/↓   选择设置", "   ↑/↓   Select"),
			m.uiText("   单击   选择设置", "   Click  Select"),
			m.uiText("   Enter 打开/切换", "   Enter Open/switch"),
			m.uiText("   Esc   关闭", "   Esc   Close"),
			"",
			promptControlsHeading,
			m.uiText("   ↑/k    上一条", "   ↑/k    Previous"),
			m.uiText("   ↓/j    下一条", "   ↓/j    Next"),
			m.uiText("   PgUp/PgDn 滚动", "   PgUp/PgDn Scroll"),
			m.uiText("   Home   第一条", "   Home   First"),
			m.uiText("   End    最新一条", "   End    Latest"),
			m.uiText("   Enter 展开/折叠", "   Enter Expand/fold"),
			m.uiText("   拖动   复制文字", "   Drag Copy text"),
			m.uiText("   c      全部折叠", "   c      Fold all"),
			m.uiText("   s      设置", "   s      Settings"),
			"",
			metricsHeading,
			m.uiText("   额度未显示", "   Quota hidden"),
			m.uiText("   暂无可靠额度数据", "   No reliable quota"),
			m.uiText("   不影响 Codex 使用", "   Codex still works"),
			"",
			gitHeading,
			"   main* +N -N ?N",
			m.uiText("   分支   当前分支", "   branch Current"),
			m.uiText("   * 已跟踪改动", "   * Tracked edits"),
			m.uiText("   +N/-N 改动行数", "   +N/-N Lines"),
			m.uiText("   ?N 未跟踪文件", "   ?N Untracked"),
			"",
			paneHeading,
			m.uiText("   Alt+←/→ 聚焦", "   Alt+←/→ Focus"),
			m.uiText("   拖动边缘 调整", "   Drag edge Resize"),
			m.uiText("   Alt+=/- 调整", "   Alt+=/- Resize"),
			m.uiText("   Ctrl+p→f 全屏", "   Ctrl+p→f Full"),
			m.uiText("   自定义键可能不同", "   Custom keys vary"),
		)
	} else {
		entries = append(entries,
			helpControlsHeading,
			m.helpEntry("↑/↓", helpScrollAction),
			m.helpEntry("PgUp/PgDn", helpScrollAction),
			m.helpEntry("Esc", m.uiText("返回设置", "Back to settings")),
			m.helpEntry("Ctrl+X", m.uiText("关闭当前窗格", "Close this pane")),
			"",
			settingsControlsHeading,
			m.helpEntry("↑/↓", m.uiText("选择设置", "Select setting")),
			m.helpEntry(m.uiText("单击", "Click"), m.uiText("选择设置", "Select setting")),
			m.helpEntry("Enter", settingsEnterAction),
			m.helpEntry("Esc", settingsCloseAction),
			"",
			promptControlsHeading,
			m.helpEntry("↑/k", m.uiText("上一条提示词", "Previous prompt")),
			m.helpEntry("↓/j", m.uiText("下一条提示词", "Next prompt")),
			m.helpEntry("PgUp/PgDn", m.uiText("滚动提示词", "Scroll prompt")),
			m.helpEntry("Home", m.uiText("第一条提示词", "First prompt")),
			m.helpEntry("End", m.uiText("最新提示词", "Latest prompt")),
			m.helpEntry("Enter", m.uiText("展开或折叠", "Expand or fold")),
			m.helpEntry(m.uiText("拖动", "Drag"), m.uiText("复制选中文字", "Copy selected text")),
			m.helpEntry("c", m.uiText("全部折叠", "Fold all")),
			m.helpEntry("s", m.uiText("设置", "Settings")),
			"",
			metricsHeading,
		)
		if m.width < 52 {
			entries = append(entries,
				m.uiText("   额度未显示", "   Quota hidden"),
				m.uiText("   暂无可靠额度数据", "   No reliable data"),
				m.uiText("   不影响 Codex 使用", "   Codex still works"),
			)
		} else {
			entries = append(entries,
				m.helpEntry(m.uiText("额度未显示", "Quota hidden"), m.uiText("Codex 暂未提供可靠的额度数据", "Codex did not provide reliable quota data")),
				m.styleMuted(m.uiText("   不影响 Codex 使用", "   Codex still works")),
			)
		}
		entries = append(entries, "", gitHeading)
		if m.width < 52 {
			entries = append(entries,
				m.helpEntry(m.uiText("示例", "example"), "main* +12 -3 ?1"),
				m.helpEntry(m.uiText("分支", "branch"), m.uiText("当前分支", "Current branch")),
				m.helpEntry("*", m.uiText("已跟踪改动", "Tracked edits")),
				m.helpEntry("+N/-N", m.uiText("改动行数", "Lines changed")),
				m.helpEntry("?N", m.uiText("未跟踪文件", "Untracked files")),
			)
		} else {
			entries = append(entries,
				m.helpEntry(m.uiText("示例", "example"), "main* +12 -3 ?1"),
				m.helpEntry(m.uiText("分支", "branch"), m.uiText("当前分支", "Current branch")),
				m.helpEntry("*", m.uiText("已跟踪文件有改动", "Tracked files changed")),
				m.helpEntry("+N/-N", m.uiText("相对最近提交的改动行数", "Lines since last commit")),
				m.helpEntry("?N", m.uiText("未跟踪文件", "Untracked files")),
			)
		}
		entries = append(entries, "", paneHeading,
			m.helpEntry("Alt+←/→", m.uiText("聚焦窗格", "Focus pane")),
			m.helpEntry(m.uiText("拖动边缘", "Drag edge"), m.uiText("调整窗格大小", "Resize panes")),
		)
		if m.width < 52 {
			entries = append(entries,
				m.helpEntry("Alt+=/-", m.uiText("调整当前窗格", "Resize focus pane")),
				m.helpEntry("Ctrl+p→f", m.uiText("当前窗格全屏", "Fullscreen pane")),
				m.styleMuted(m.uiText("   Zellij 默认按键", "   Zellij defaults")),
				m.styleMuted(m.uiText("   自定义绑定可能不同", "   Custom bindings may differ")),
			)
		} else {
			entries = append(entries,
				m.helpEntry("Alt+=/-", m.uiText("调整当前聚焦窗格", "Resize focused pane")),
				m.helpEntry("Ctrl+p→f", m.uiText("当前聚焦窗格全屏", "Fullscreen focused pane")),
				m.styleMuted(m.uiText("   Zellij 默认按键；自定义绑定可能不同", "   Zellij defaults; custom bindings may differ")),
			)
		}
	}
	headings := map[string]struct{}{
		strings.TrimSpace(connectionHeading):       {},
		strings.TrimSpace(helpControlsHeading):     {},
		strings.TrimSpace(settingsControlsHeading): {},
		strings.TrimSpace(promptControlsHeading):   {},
		strings.TrimSpace(metricsHeading):          {},
		strings.TrimSpace(gitHeading):              {},
		strings.TrimSpace(paneHeading):             {},
	}
	for index, entry := range entries {
		if _, heading := headings[strings.TrimSpace(entry)]; heading {
			entries[index] = m.styleAction(entry)
		}
	}
	return entries
}

func (m Model) aboutLines() []string {
	versionLine := "   Prompt Pane v" + appversion.Current
	if ansi.StringWidth(versionLine) > m.width {
		versionLine = "   Prompt Pane " + appversion.Current
	}
	if ansi.StringWidth(versionLine) > m.width {
		versionLine = "   v" + strings.TrimPrefix(appversion.Current, "v")
	}
	environmentHeading := m.uiText(" 支持环境", " Supported environment")
	if ansi.StringWidth(environmentHeading) > m.width {
		environmentHeading = m.uiText(" 支持环境", " Environment")
	}
	return []string{
		m.styleAction(" " + m.uiText("关于", "About")),
		"",
		versionLine,
		"",
		m.styleAction(environmentHeading),
		"   Windows x64",
		"   PowerShell 5.1/7",
		"   Codex CLI",
		"   Zellij " + zellij.Version,
		"",
		m.styleAction(" " + m.uiText("视觉参考", "Visual reference")),
		"   Token Tracker",
	}
}

func (m Model) themeLines() []string {
	entries := []string{m.styleAction(" " + m.uiText("主题", "Theme"))}
	if instruction := m.themeInstruction(); instruction != "" {
		entries = append(entries, instruction)
	}
	entries = append(entries, "")
	names := theme.SelectableNames()
	nameWidth := themeNameWidth()
	for index, name := range names {
		marker := "  "
		if index == m.themeIndex {
			marker = "› "
		}
		label := " " + marker + padRightCells(name, nameWidth)
		if index == m.themeIndex {
			label = m.styleThemeSelection(label)
		}
		entries = append(entries, label+m.themeSwatches(name))
	}
	previewLabel := m.uiText("界面预览", "Interface preview")
	previewTitle := m.styleAction(" " + previewLabel)
	if ansi.StringWidth(" "+previewLabel+" · "+m.themeName) <= m.width {
		previewTitle += " · " + m.themeName
	}
	entries = append(entries, "", previewTitle)
	entries = append(entries, m.themePreviewLines()...)
	if m.themeSource == config.ThemeEnvironment {
		entries = append(entries, "", m.styleWarning("   "+theme.Environment+m.uiText(" 正在覆盖已保存设置", " overrides saved settings")))
	} else if m.themeMessage != "" {
		entries = append(entries, "", m.styleError("   "+m.themeMessage))
	}
	return entries
}

func (m Model) themeInstruction() string {
	if m.height <= 6 {
		return ""
	}
	candidates := []string{
		m.uiText("   ↑/↓ 或单击预览 · Enter 保存 · Esc 取消", "   ↑/↓ or click to preview · Enter save · Esc cancel"),
		m.uiText("   ↑/↓ 或单击 · Enter 保存 · Esc 取消", "   ↑/↓ or click preview · Enter save · Esc cancel"),
		m.uiText("   ↑/↓ 或单击 · Enter 保存", "   ↑/↓ or click · Enter save"),
		m.uiText("   ↑/↓/单击 · Enter", "   ↑/↓/click · Enter"),
		"   ↑/↓ · Enter",
	}
	for _, candidate := range candidates {
		if ansi.StringWidth(candidate) <= m.width {
			return candidate
		}
	}
	return ""
}

func (m Model) themePreviewLines() []string {
	selectedPrompt := m.uiText("检查这个实现", "Check this code")
	activeLead := m.uiText("再看", "Test ")
	activeSelection := m.uiText("边界条件", "edge cases")
	selectedID := "theme-preview-selected"
	activeID := "theme-preview-active"
	preview := m
	preview.overlay = overlayNone
	preview.selectedID = selectedID
	preview.expanded = make(map[string]bool)
	preview.snapshot = ipc.Snapshot{
		State:          "live",
		ActiveTurnID:   activeID,
		ActivePromptID: activeID,
		Prompts: []provider.UserPrompt{
			{ID: selectedID, Text: selectedPrompt},
			{ID: activeID, Text: activeLead + activeSelection},
		},
		Metrics: &provider.SessionMetrics{
			Branch:             "main*",
			Added:              12,
			Deleted:            3,
			Untracked:          1,
			TotalTokens:        2_400_000,
			Model:              "gpt-5.6",
			ContextWindow:      258_000,
			ContextUsedPercent: 42,
			Quotas:             []provider.QuotaWindow{{WindowMinutes: 300, UsedPercent: 66}},
			QuotaStatus:        provider.QuotaAvailable,
		},
	}
	preview.activity = activityViewState{
		promptID: activeID,
		phrase:   m.uiText("搁这儿寻思呢…", "Pondering…"),
		frame:    len(activityDotFrames) - 1,
		visible:  true,
	}

	layout := preview.layoutBody()
	lines := append([]string(nil), layout.lines...)
	if len(layout.prompts) > 1 {
		lineIndex := layout.prompts[1].start
		left := 4 + ansi.StringWidth(activeLead)
		right := left + ansi.StringWidth(activeSelection)
		line := lines[lineIndex]
		prefix := ansi.Cut(line, 0, left)
		selected := ansi.Strip(ansi.Cut(line, left, right))
		suffix := ansi.Cut(line, right, ansi.StringWidth(line))
		lines[lineIndex] = prefix + m.styleTextSelection(selected) + suffix
	}
	lines = append(lines,
		" "+m.styleAction(m.belowText(3)),
		"",
	)
	lines = append(lines, preview.renderStatusBlock(2)...)
	for index := range lines {
		lines[index] = ansi.Truncate(lines[index], max(1, m.width), "")
	}
	return lines
}

func (m Model) themeSwatches(name string) string {
	palette := theme.Resolve(name, m.lightBackground)
	colors := []string{
		palette.Green, palette.Yellow, palette.Peach, palette.Red,
		palette.Blue, palette.Sapphire, palette.Mauve, palette.Pink,
	}
	separator := ""
	if m.width >= 30 {
		separator = " "
	}
	if m.noColor {
		return "  " + strings.TrimSuffix(strings.Repeat("■"+separator, len(colors)), separator)
	}
	var swatches strings.Builder
	swatches.WriteString("  ")
	for index, color := range colors {
		if index > 0 {
			swatches.WriteString(separator)
		}
		swatches.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render("■"))
	}
	return swatches.String()
}

func (m Model) helpEntry(key, description string) string {
	return "   " + padRightCells(key, m.helpLabelWidth()) + "  " + description
}

func (m Model) helpLabelWidth() int {
	width := 0
	for _, label := range []string{
		"Ctrl+X", "Esc", "s", "↑/↓", "↑/k", "↓/j", "PgUp/PgDn", "Home", "End",
		"Enter", m.uiText("单击", "Click"), m.uiText("拖动", "Drag"), "c", m.uiText("示例", "example"), m.uiText("分支", "branch"), "*", "+N/-N", "?N",
		"Alt+←/→", m.uiText("拖动边缘", "Drag edge"), "Alt+=/-", "Ctrl+p→f",
	} {
		width = max(width, ansi.StringWidth(label))
	}
	return width
}

func themeNameWidth() int {
	width := 0
	for _, name := range theme.SelectableNames() {
		width = max(width, ansi.StringWidth(name))
	}
	return width
}

func padRightCells(text string, width int) string {
	return text + strings.Repeat(" ", max(0, width-ansi.StringWidth(text)))
}

func (m Model) renderStatusBlock(maxLines int) []string {
	if maxLines < 1 {
		return nil
	}
	if maxLines == 1 {
		return []string{m.renderFooter()}
	}
	lines := []string{m.renderStatusHeader()}
	if m.snapshot.State == "error" {
		return lines
	}
	if m.snapshot.Metrics == nil {
		if m.snapshot.State == "ready" || m.snapshot.State == "live" {
			waiting := " " + m.uiText("首次回复后显示指标", "Metrics available after first response")
			return append(lines, ansi.Truncate(waiting, m.width, ""))
		}
		return lines
	}
	remaining := maxLines - 1
	rows := m.renderMetricRows(remaining)
	if len(rows) > remaining {
		rows = rows[:remaining]
	}
	for index := range rows {
		rows[index] = ansi.Truncate(rows[index], m.width, "")
	}
	return append(lines, rows...)
}

func (m Model) renderStatusHeader() string {
	metrics := m.snapshot.Metrics
	if m.snapshot.State == "error" {
		metrics = nil
	}
	available := max(1, m.width-1)
	right := ""
	if m.width >= 20 {
		right = m.styleAction(m.settingsAction())
	}
	total := ""
	if metrics != nil {
		value := "--"
		if metrics.TotalTokens > 0 {
			value = compactNumber(metrics.TotalTokens)
		}
		total = m.styleColor("Total: "+value, m.visualRoles().Token)
	}
	model := ""
	if metrics != nil && metrics.Model != "" {
		label := metrics.Model
		if metrics.Effort != "" {
			label += " " + metrics.Effort
		}
		model = m.styleColor(label, m.visualRoles().Model)
	}
	leftAvailable := available
	if right != "" {
		leftAvailable -= ansi.StringWidth(right) + 1
	}
	branch := m.renderBranch(metrics, true)
	left := joinStatusPieces(strings.TrimSpace(branch), total, model)
	if ansi.StringWidth(left) > leftAvailable {
		left = joinStatusPieces(strings.TrimSpace(branch), total)
	}
	if ansi.StringWidth(left) > leftAvailable {
		branch = m.renderBranch(metrics, false)
		left = joinStatusPieces(strings.TrimSpace(branch), total)
	}
	if ansi.StringWidth(left) > leftAvailable {
		left = total
	}
	if ansi.StringWidth(left) > leftAvailable && right != "" {
		right = ""
		leftAvailable = available
		left = total
	}
	left = ansi.Truncate(left, leftAvailable, "")
	if right == "" {
		return prefixStatus(left)
	}
	gap := max(1, available-ansi.StringWidth(left)-ansi.StringWidth(right))
	return prefixStatus(left + strings.Repeat(" ", gap) + right)
}

func (m Model) settingsAction() string {
	if m.width < 21 {
		return "[s]"
	}
	return m.uiText("[s] 设置", "[s] settings")
}

func joinStatusPieces(pieces ...string) string {
	filtered := make([]string, 0, len(pieces))
	for _, piece := range pieces {
		if piece != "" {
			filtered = append(filtered, piece)
		}
	}
	return strings.Join(filtered, " | ")
}

func (m Model) renderBranch(metrics *provider.SessionMetrics, details bool) string {
	if metrics == nil || metrics.Branch == "" {
		return ""
	}
	colors := m.visualRoles()
	branchName := strings.TrimSuffix(metrics.Branch, "*")
	branch := m.styleColor(branchName, colors.Branch)
	if branchName != metrics.Branch {
		branch += m.styleColor("*", colors.Branch)
	}
	if details {
		if metrics.Added > 0 {
			branch += " " + m.styleColor(fmt.Sprintf("+%d", metrics.Added), colors.Added)
		}
		if metrics.Deleted > 0 {
			branch += " " + m.styleColor(fmt.Sprintf("-%d", metrics.Deleted), colors.Deleted)
		}
		if metrics.Untracked > 0 {
			branch += " " + m.styleColor(fmt.Sprintf("?%d", metrics.Untracked), colors.Untracked)
		}
	}
	return branch
}

func (m Model) renderMetricRows(maxRows int) []string {
	if maxRows < 1 || m.snapshot.Metrics == nil {
		return nil
	}
	available := max(1, m.width-1)
	return []string{prefixStatus(m.compactMetricRow(available))}
}

func (m Model) compactMetricRow(available int) string {
	barWidth := 8
	if m.width >= 110 {
		barWidth = 12
	}
	type layout struct {
		showContext  bool
		showCapacity bool
		showReset    bool
	}
	build := func(width int, layout layout) string {
		limit := m.limitMetricText(width, layout.showReset)
		parts := make([]string, 0, 2)
		if limit != "" {
			parts = append(parts, limit)
		}
		if layout.showContext || limit == "" {
			parts = append(parts, m.contextMetricText(width, layout.showCapacity))
		}
		return strings.Join(parts, " | ")
	}
	tryBars := func(layout layout) string {
		for width := barWidth; width >= 4; width-- {
			if row := build(width, layout); ansi.StringWidth(row) <= available {
				return row
			}
		}
		return ""
	}
	if row := tryBars(layout{showContext: true, showCapacity: true, showReset: true}); row != "" {
		return row
	}
	compactContext := layout{showContext: true}
	if row := tryBars(compactContext); row != "" {
		return row
	}
	if row := build(0, compactContext); ansi.StringWidth(row) <= available {
		return row
	}
	quotaOnly := layout{}
	if row := tryBars(quotaOnly); row != "" {
		return row
	}
	return ansi.Truncate(build(0, quotaOnly), available, "")
}

func (m Model) limitMetricText(barWidth int, resetVisible bool) string {
	metrics := m.snapshot.Metrics
	limits := make([]string, 0, len(metrics.Quotas))
	for _, quota := range metrics.Quotas {
		limits = append(limits, m.renderQuota(quotaWindowLabel(quota.WindowMinutes), quota, barWidth, resetVisible))
	}
	if len(limits) == 0 {
		return ""
	}
	return strings.Join(limits, "  ")
}

func (m Model) contextMetricText(barWidth int, capacityVisible bool) string {
	metrics := m.snapshot.Metrics
	if metrics.ContextUsedPercent < 0 || metrics.ContextUsedPercent == 0 && metrics.ContextWindow <= 0 {
		return m.styleColor("Ctx: ", m.visualRoles().Label) + "--"
	}
	value := ""
	if capacityVisible && metrics.ContextWindow > 0 {
		value = compactNumber(metrics.ContextWindow) + "  "
	}
	return m.styleColor("Ctx: "+value, m.visualRoles().Label) + m.renderPercent(metrics.ContextUsedPercent, barWidth)
}

func (m Model) renderQuota(label string, quota provider.QuotaWindow, barWidth int, resetVisible bool) string {
	text := m.styleColor(label+": ", m.visualRoles().Label) + m.renderPercent(quota.UsedPercent, barWidth)
	if resetVisible && quota.ResetsAt > time.Now().Unix() {
		text += m.styleColor(" ("+formatDuration(quota.ResetsAt-time.Now().Unix())+")", m.visualRoles().Label)
	}
	return text
}

func quotaWindowLabel(windowMinutes int64) string {
	if windowMinutes > 0 && windowMinutes%(24*60) == 0 {
		return fmt.Sprintf("%dd", windowMinutes/(24*60))
	}
	if windowMinutes > 0 && windowMinutes%60 == 0 {
		return fmt.Sprintf("%dh", windowMinutes/60)
	}
	return fmt.Sprintf("%dm", windowMinutes)
}

func (m Model) renderPercent(percent float64, barWidth int) string {
	percent = max(0, min(100, percent))
	color := quotaColor(percent, m.visualRoles())
	if barWidth <= 0 {
		return m.styleColor(fmt.Sprintf("%.0f%%", percent), color)
	}
	filled := int(percent/100*float64(barWidth) + 0.5)
	bar := m.styleGraphicColor(strings.Repeat("█", filled), color)
	empty := strings.Repeat("░", barWidth-filled)
	if percent > 0 {
		empty = m.styleGraphicColor(empty, color)
	}
	return bar + empty + " " + m.styleColor(fmt.Sprintf("%.0f%%", percent), color)
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
		return fmt.Sprintf("%dd", seconds/86400)
	}
	if seconds >= 3600 {
		return fmt.Sprintf("%dh", seconds/3600)
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
	if m.noColor || !m.semanticColorVisible(color) {
		return text
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(text)
}

func (m Model) styleGraphicColor(text, color string) string {
	if m.noColor {
		return text
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(text)
}

func (m Model) semanticColorVisible(color string) bool {
	return m.backgroundColor == "" || theme.MeetsContrast(color, m.backgroundColor, theme.MinimumTextContrast)
}

func (m Model) styleWarning(text string) string {
	return m.styleColor(text, m.visualRoles().Warning)
}

func (m Model) styleError(text string) string {
	return m.styleColor(text, m.visualRoles().Error)
}

func (m Model) styleMuted(text string) string {
	return m.styleColor(text, m.visualRoles().Muted)
}

func (m Model) styleBodyText(text string) string {
	return m.styleColor(text, m.visualRoles().BodyText)
}

func (m Model) styleAction(text string) string {
	accent := m.visualRoles().Accent
	if m.noColor || !m.semanticColorVisible(accent) {
		return lipgloss.NewStyle().Bold(true).Render(text)
	}
	return m.styleColor(text, accent)
}

func (m Model) styleSettingSelection(text string) string {
	return lipgloss.NewStyle().Bold(true).Render(text)
}

func (m Model) styleThemeSelection(text string) string {
	style := lipgloss.NewStyle().Bold(true)
	selection := m.visualRoles().ThemePick
	if !m.noColor && m.semanticColorVisible(selection) {
		style = style.Foreground(lipgloss.Color(selection))
	}
	return style.Render(text)
}

func (m Model) styleActivityText(text string) string {
	return m.styleColor(text, m.visualRoles().ActivityIndicator)
}

func (m Model) styleActivityIndicator(text string) string {
	return m.styleGraphicColor(text, m.visualRoles().ActivityIndicator)
}

func (m Model) styleEmphasizedPrompt(text string) string {
	body := m.visualRoles().BodyText
	visible := !m.noColor && m.semanticColorVisible(body)
	style := lipgloss.NewStyle().Bold(true)
	if visible {
		style = style.Foreground(lipgloss.Color(body))
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
		notice := m.stateNotice(m.snapshot.State)
		if m.snapshot.Notice != "" {
			notice = m.localizedNotice(m.snapshot.Notice)
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
	activeID := activePromptID(m.snapshot)
	layout := bodyLayout{prompts: make([]promptRange, 0, len(m.snapshot.Prompts)), activityLine: -1}
	for index, prompt := range m.snapshot.Prompts {
		wrapped := wrapText(sanitize(prompt.Text), textWidth)
		isActive := index == len(m.snapshot.Prompts)-1 && prompt.ID == activeID
		isEmphasized := index == selected || isActive
		isLong := len(wrapped) > collapsedLineLimit
		summaryLine := -1
		if isLong && !m.expanded[prompt.ID] {
			hidden := len(wrapped) - collapsedVisibleLines
			wrapped = append(wrapped[:collapsedVisibleLines], m.foldSummary(hidden, textWidth, index == selected))
			summaryLine = len(wrapped) - 1
		}
		start := len(layout.lines)
		for lineIndex, line := range wrapped {
			prefix := " " + strings.Repeat(" ", digits) + " "
			if lineIndex == 0 {
				prefix = fmt.Sprintf(" %*d ", digits, index+1)
			}
			styledPrefix := m.styleBodyText(prefix)
			if isEmphasized {
				styledPrefix = m.styleEmphasizedPrompt(prefix)
			}
			if lineIndex == summaryLine {
				layout.lines = append(layout.lines, styledPrefix+m.styleMuted(line))
				continue
			}
			styledLine := m.styleBodyText(line)
			if isEmphasized {
				styledLine = m.styleEmphasizedPrompt(line)
			}
			layout.lines = append(layout.lines, styledPrefix+styledLine)
		}
		layout.prompts = append(layout.prompts, promptRange{start: start, end: len(layout.lines), long: isLong})
		tail := ""
		if index == len(m.snapshot.Prompts)-1 && m.activity.visible {
			layout.activityLine = len(layout.lines)
			if activity := m.renderActivity(textWidth); activity != "" {
				tail = " " + strings.Repeat(" ", digits) + " " + activity
			}
		}
		layout.lines = append(layout.lines, tail)
	}
	return layout
}

func (m Model) renderActivity(width int) string {
	if width < 1 || !m.activity.visible {
		return ""
	}
	frameIndex := m.activity.frame % len(activityDotFrames)
	if m.reducedMotion || m.activity.settling {
		frameIndex = len(activityDotFrames) - 1
	}
	dots := activityDotFrames[frameIndex]
	if width < 4 {
		return m.styleActivityIndicator(ansi.Truncate(strings.TrimRight(dots, " "), width, ""))
	}
	phrase := strings.TrimSpace(strings.TrimSuffix(m.activity.phrase, "…"))
	phrase = strings.TrimSpace(strings.TrimSuffix(phrase, "..."))
	phrase = ansi.Truncate(phrase, width-4, "")
	if phrase == "" {
		return m.styleActivityIndicator(dots)
	}
	return m.styleActivityText(phrase+" ") + m.styleActivityIndicator(dots)
}

func (m Model) visibleActivityRow(row int) bool {
	layout := m.layoutBody()
	return layout.activityLine >= 0 && m.offset+row == layout.activityLine
}

func (m Model) foldSummary(hidden, width int, selected bool) string {
	full := fmt.Sprintf(m.uiText("… 另有 %d 行", "… +%d lines"), hidden)
	candidates := []string{full, fmt.Sprintf("… +%d", hidden)}
	if selected {
		candidates = []string{
			full + m.uiText(" · Enter 展开", " · Enter expand"),
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
	wrapped := wrapMixedText(masked, width)
	if marker != "" {
		wrapped = strings.ReplaceAll(wrapped, marker, " ")
	}
	return strings.Split(wrapped, "\n")
}

type wrapToken struct {
	text  string
	width int
	space bool
}

func wrapMixedText(text string, width int) string {
	var lines []string
	for _, sourceLine := range strings.Split(text, "\n") {
		tokens := mixedWrapTokens(sourceLine)
		var line strings.Builder
		lineWidth := 0
		pendingSpace := ""
		pendingWidth := 0
		flush := func() {
			lines = append(lines, line.String())
			line.Reset()
			lineWidth = 0
			pendingSpace = ""
			pendingWidth = 0
		}
		appendClusters := func(text string) {
			for text != "" {
				asciiEnd := 0
				for asciiEnd < len(text) && text[asciiEnd] >= ' ' && text[asciiEnd] <= '~' {
					asciiEnd++
				}
				if asciiEnd > 0 {
					for consumed := 0; consumed < asciiEnd; {
						if line.Len() > 0 && lineWidth >= width {
							flush()
						}
						take := min(asciiEnd-consumed, width-lineWidth)
						line.WriteString(text[consumed : consumed+take])
						lineWidth += take
						consumed += take
					}
					text = text[asciiEnd:]
					continue
				}
				cluster, clusterWidth := firstWrapCluster(text)
				if line.Len() > 0 && lineWidth+clusterWidth > width {
					flush()
				}
				line.WriteString(cluster)
				lineWidth += clusterWidth
				text = text[len(cluster):]
			}
		}
		for _, token := range tokens {
			if token.space {
				pendingSpace += token.text
				pendingWidth += token.width
				continue
			}
			if line.Len() > 0 && lineWidth+pendingWidth+token.width > width {
				flush()
			}
			if line.Len() == 0 && token.width > width {
				appendClusters(token.text)
				continue
			}
			if lineWidth+pendingWidth+token.width > width {
				flush()
			}
			line.WriteString(pendingSpace)
			line.WriteString(token.text)
			lineWidth += pendingWidth + token.width
			pendingSpace = ""
			pendingWidth = 0
		}
		if pendingSpace != "" {
			appendClusters(pendingSpace)
		}
		lines = append(lines, line.String())
	}
	return strings.Join(lines, "\n")
}

func mixedWrapTokens(text string) []wrapToken {
	// Most lines contain far fewer semantic runs than bytes. A small initial
	// capacity avoids reserving dozens of wrapToken structs for every prompt.
	tokens := make([]wrapToken, 0, min(len(text), 16))
	runStart := -1
	runWidth := 0
	runSpace := false
	flushRun := func(end int) {
		if runStart < 0 {
			return
		}
		tokens = append(tokens, wrapToken{
			text:  text[runStart:end],
			width: runWidth,
			space: runSpace,
		})
		runStart = -1
		runWidth = 0
	}
	for offset := 0; offset < len(text); {
		if text[offset] >= ' ' && text[offset] <= '~' {
			space := text[offset] == ' '
			end := offset + 1
			for end < len(text) && text[end] >= ' ' && text[end] <= '~' && (text[end] == ' ') == space {
				end++
			}
			if runStart >= 0 && runSpace != space {
				flushRun(offset)
			}
			if runStart < 0 {
				runStart = offset
				runSpace = space
			}
			runWidth += end - offset
			offset = end
			continue
		}
		cluster, width := firstWrapCluster(text[offset:])
		r, _ := utf8.DecodeRuneInString(cluster)
		space := unicode.IsSpace(r)
		breakable := isCJK(r) || r >= utf8.RuneSelf && !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r)
		if breakable && !space {
			flushRun(offset)
			tokens = append(tokens, wrapToken{text: cluster, width: width})
			offset += len(cluster)
			continue
		}
		if runStart >= 0 && runSpace != space {
			flushRun(offset)
		}
		if runStart < 0 {
			runStart = offset
			runSpace = space
		}
		runWidth += width
		offset += len(cluster)
	}
	flushRun(len(text))
	return tokens
}

func firstWrapCluster(text string) (string, int) {
	if text[0] >= ' ' && text[0] <= '~' {
		return text[:1], 1
	}
	return ansi.FirstGraphemeCluster(text, ansi.GraphemeWidth)
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
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
