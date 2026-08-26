package ui

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Natsume-kkk/prompt-pane/internal/config"
	"github.com/Natsume-kkk/prompt-pane/internal/ipc"
	"github.com/Natsume-kkk/prompt-pane/internal/provider"
)

const (
	activityInitialDelay = 300 * time.Millisecond
	activityFrameDelay   = 500 * time.Millisecond
	activitySettleDelay  = 200 * time.Millisecond
)

var activityDotFrames = [...]string{
	"   ",
	".  ",
	".. ",
	"...",
}

type activityStartMsg struct {
	promptID   string
	generation uint64
}

type activityFrameMsg struct {
	promptID   string
	generation uint64
}

type activityPhraseMsg struct {
	promptID   string
	generation uint64
}

type activityClearMsg struct{ generation uint64 }

type interfaceLanguageSavedMsg struct{ language string }
type interfaceLanguageSaveFailedMsg struct{ err error }

type activityViewState struct {
	promptID   string
	phrase     string
	frame      int
	generation uint64
	visible    bool
	settling   bool
	changes    int
}

type activityPlanner struct {
	randomState     uint64
	lastRegister    activityRegister
	hasLastRegister bool
	recent          []string
}

func newActivityPlanner() activityPlanner {
	var seed [8]byte
	if _, err := cryptorand.Read(seed[:]); err == nil {
		return activityPlanner{randomState: binary.LittleEndian.Uint64(seed[:])}
	}
	return activityPlanner{randomState: uint64(time.Now().UnixNano())}
}

func (p *activityPlanner) nextRandom(limit int) int {
	if limit <= 1 {
		return 0
	}
	if p.randomState == 0 {
		p.randomState = 0x9e3779b97f4a7c15
	}
	p.randomState ^= p.randomState << 13
	p.randomState ^= p.randomState >> 7
	p.randomState ^= p.randomState << 17
	return int(p.randomState % uint64(limit))
}

func (p *activityPlanner) beginTurn(language string) string {
	return p.nextPhrase(language)
}

func (p *activityPlanner) nextPhrase(language string) string {
	if language == config.InterfaceLanguageEnglish {
		return p.chooseRecentSafe(englishActivityPhrases)
	}
	return p.chooseChinesePhrase()
}

func (p *activityPlanner) chooseChinesePhrase() string {
	if len(chineseActivityPhrases) == 0 {
		return "琢磨着呢…"
	}
	recentLimit := min(96, max(1, len(chineseActivityPhrases)/2))
	for {
		registers := p.availableChineseRegisters()
		if len(registers) > 0 {
			register := registers[p.nextRandom(len(registers))]
			phrases := p.availableChinesePhrases(register)
			choice := phrases[p.nextRandom(len(phrases))]
			p.lastRegister = register
			p.hasLastRegister = true
			p.rememberPhrase(choice.text, recentLimit)
			return choice.text
		}
		if len(p.recent) == 0 {
			p.hasLastRegister = false
			continue
		}
		p.recent = append([]string(nil), p.recent[1:]...)
	}
}

func (p *activityPlanner) availableChineseRegisters() []activityRegister {
	registers := make([]activityRegister, 0, len(activityRegisterOrder))
	for _, register := range activityRegisterOrder {
		if p.hasLastRegister && register == p.lastRegister {
			continue
		}
		if len(p.availableChinesePhrases(register)) > 0 {
			registers = append(registers, register)
		}
	}
	return registers
}

func (p *activityPlanner) availableChinesePhrases(register activityRegister) []activityPhrase {
	phrases := make([]activityPhrase, 0)
	for _, phrase := range chineseActivityPhrases {
		if phrase.register == register && !containsActivityPhrase(p.recent, phrase.text) {
			phrases = append(phrases, phrase)
		}
	}
	return phrases
}

func (p *activityPlanner) chooseRecentSafe(lines []string) string {
	if len(lines) == 0 {
		return "Pondering…"
	}
	start := p.nextRandom(len(lines))
	choice := lines[start]
	for offset := 0; offset < len(lines); offset++ {
		candidate := lines[(start+offset)%len(lines)]
		if !containsActivityPhrase(p.recent, candidate) {
			choice = candidate
			break
		}
	}
	p.rememberPhrase(choice, 96)
	return choice
}

func (p *activityPlanner) rememberPhrase(phrase string, limit int) {
	recent := append(append([]string(nil), p.recent...), phrase)
	if len(recent) > limit {
		recent = append([]string(nil), recent[len(recent)-limit:]...)
	}
	p.recent = recent
}

func containsActivityPhrase(lines []string, target string) bool {
	for _, line := range lines {
		if line == target {
			return true
		}
	}
	return false
}

func activityStartCommand(promptID string, generation uint64) tea.Cmd {
	return tea.Tick(activityInitialDelay, func(time.Time) tea.Msg {
		return activityStartMsg{promptID: promptID, generation: generation}
	})
}

func activityFrameCommand(promptID string, generation uint64) tea.Cmd {
	return tea.Tick(activityFrameDelay, func(time.Time) tea.Msg {
		return activityFrameMsg{promptID: promptID, generation: generation}
	})
}

func activityPhraseCommand(promptID string, generation uint64, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return activityPhraseMsg{promptID: promptID, generation: generation}
	})
}

func activityClearCommand(generation uint64) tea.Cmd {
	return tea.Tick(activitySettleDelay, func(time.Time) tea.Msg {
		return activityClearMsg{generation: generation}
	})
}

func saveInterfaceLanguage(language string) tea.Cmd {
	return func() tea.Msg {
		if err := config.SaveInterfaceLanguage(language); err != nil {
			return interfaceLanguageSaveFailedMsg{err: err}
		}
		return interfaceLanguageSavedMsg{language: language}
	}
}

func activityPhraseDelay(planner *activityPlanner, changes int, reducedMotion bool) time.Duration {
	minimum := 8
	span := 7
	if changes >= 5 {
		minimum = 15
		span = 11
	}
	if reducedMotion {
		minimum = 20
		span = 11
	}
	return time.Duration(minimum+planner.nextRandom(span)) * time.Second
}

func (m *Model) beginActivity(promptID string) tea.Cmd {
	generation := m.activity.generation + 1
	m.activity = activityViewState{promptID: promptID, generation: generation}
	return activityStartCommand(promptID, generation)
}

func (m *Model) endActivity(settle bool) tea.Cmd {
	generation := m.activity.generation + 1
	if settle && m.activity.visible {
		m.activity.promptID = ""
		m.activity.generation = generation
		m.activity.frame = len(activityDotFrames) - 1
		m.activity.settling = true
		return activityClearCommand(generation)
	}
	m.activity = activityViewState{generation: generation}
	return nil
}

func (m Model) activityMessageCurrent(promptID string, generation uint64) bool {
	return promptID != "" && promptID == activePromptID(m.snapshot) &&
		promptID == m.activity.promptID && generation == m.activity.generation
}

func activePromptID(snapshot ipc.Snapshot) string {
	if snapshot.ActivePromptID != "" {
		return snapshot.ActivePromptID
	}
	return snapshot.ActiveTurnID
}

func samePromptSequence(left, right []provider.UserPrompt) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].ID != right[index].ID {
			return false
		}
	}
	return true
}
