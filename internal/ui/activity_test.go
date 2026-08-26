package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Natsume-kkk/prompt-pane/internal/config"
	"github.com/Natsume-kkk/prompt-pane/internal/ipc"
	"github.com/Natsume-kkk/prompt-pane/internal/provider"
	"github.com/Natsume-kkk/prompt-pane/internal/theme"
)

func TestChineseActivityCorpusIsBalancedStaticAndSafe(t *testing.T) {
	seen := make(map[string]struct{})
	counts := make(map[activityRegister]int)
	validRegisters := map[activityRegister]bool{
		activityColloquial: true,
		activityPlayful:    true,
		activityNortheast:  true,
		activitySichuan:    true,
		activityCantonese:  true,
		activityInternet:   true,
		activityClassical:  true,
		activityHybrid:     true,
	}
	for _, phrase := range chineseActivityPhrases {
		if !validRegisters[phrase.register] {
			t.Fatalf("activity phrase %q has unknown register %d", phrase.text, phrase.register)
		}
		counts[phrase.register]++
		if phrase.text != strings.TrimSpace(phrase.text) || phrase.text == "" {
			t.Fatalf("activity phrase has invalid whitespace: %q", phrase.text)
		}
		if _, exists := seen[phrase.text]; exists {
			t.Fatalf("duplicate activity phrase %q", phrase.text)
		}
		seen[phrase.text] = struct{}{}
		if !strings.HasSuffix(phrase.text, "…") {
			t.Fatalf("activity phrase lacks ellipsis: %q", phrase.text)
		}
		if strings.ContainsAny(phrase.text, "\r\n\t\x1b") {
			t.Fatalf("activity phrase contains a control character: %q", phrase.text)
		}
		if ansi.StringWidth(phrase.text) > 38 {
			t.Fatalf("activity phrase is too wide: %q", phrase.text)
		}
		for _, forbidden := range []string{
			"答案", "回答", "已经找到", "预计完成", "两岸", "台海",
			"甭催", "先别吵吵", "莫催", "唔使催",
			"众里再寻千百度", "欲穷千里目", "行到水穷处",
		} {
			if strings.Contains(phrase.text, forbidden) {
				t.Fatalf("activity phrase %q contains forbidden text %q", phrase.text, forbidden)
			}
		}
	}
	if len(chineseActivityPhrases) != 414 {
		t.Fatalf("Chinese activity corpus has %d phrases, want 414", len(chineseActivityPhrases))
	}
	if len(activityPhrasesChinesePoetry) != 24 {
		t.Fatalf("Chinese poetry activity corpus has %d phrases, want 24", len(activityPhrasesChinesePoetry))
	}
	for name, groups := range map[string][2][]activityPhrase{
		"northeast": {activityPhrasesChineseNortheastNatural, activityPhrasesChineseNortheastPlayful},
		"sichuan":   {activityPhrasesChineseSichuanNatural, activityPhrasesChineseSichuanPlayful},
		"cantonese": {activityPhrasesChineseCantoneseNatural, activityPhrasesChineseCantonesePlayful},
	} {
		if len(groups[0]) != 9 || len(groups[1]) != 21 {
			t.Fatalf("%s dialect corpus has %d natural and %d playful phrases, want 9 and 21", name, len(groups[0]), len(groups[1]))
		}
	}
	wantCounts := map[activityRegister]int{
		activityColloquial: 90,
		activityPlayful:    108,
		activityNortheast:  30,
		activitySichuan:    30,
		activityCantonese:  30,
		activityInternet:   54,
		activityClassical:  48,
		activityHybrid:     24,
	}
	for register, want := range wantCounts {
		if counts[register] != want {
			t.Fatalf("activity register %d has %d phrases, want %d", register, counts[register], want)
		}
	}
	weightCounts := make(map[activityRegister]int)
	for _, register := range activityRegisterOrder {
		weightCounts[register]++
	}
	wantWeights := map[activityRegister]int{
		activityColloquial: 6,
		activityPlayful:    8,
		activityNortheast:  1,
		activitySichuan:    1,
		activityCantonese:  1,
		activityInternet:   3,
		activityClassical:  2,
		activityHybrid:     1,
	}
	for register, want := range wantWeights {
		if weightCounts[register] != want {
			t.Fatalf("activity register %d has weight %d, want %d", register, weightCounts[register], want)
		}
	}
}

func TestChineseActivityObservedRegisterMixMatchesProductIntent(t *testing.T) {
	registerByPhrase := make(map[string]activityRegister, len(chineseActivityPhrases))
	for _, phrase := range chineseActivityPhrases {
		registerByPhrase[phrase.text] = phrase.register
	}
	planner := activityPlanner{randomState: 0x123456789abcdef}
	counts := make(map[activityRegister]int)
	const samples = 50_000
	for range samples {
		counts[registerByPhrase[planner.nextPhrase(config.InterfaceLanguageChinese)]]++
	}
	percent := func(registers ...activityRegister) float64 {
		count := 0
		for _, register := range registers {
			count += counts[register]
		}
		return float64(count) * 100 / samples
	}
	for name, result := range map[string]struct {
		got     float64
		minimum float64
		maximum float64
	}{
		"modern natural": {percent(activityColloquial), 23, 27},
		"modern playful": {percent(activityPlayful), 27, 32},
		"dialect":        {percent(activityNortheast, activitySichuan, activityCantonese), 14, 18},
		"internet":       {percent(activityInternet), 13, 17},
		"literary":       {percent(activityClassical), 8.5, 12},
		"hybrid":         {percent(activityHybrid), 4, 7},
	} {
		if result.got < result.minimum || result.got > result.maximum {
			t.Fatalf("%s observed share = %.2f%%, want %.1f%%..%.1f%%", name, result.got, result.minimum, result.maximum)
		}
	}
}

func TestChineseActivityPlannerAvoidsRecentPhrasesAndRegisterRuns(t *testing.T) {
	registerByPhrase := make(map[string]activityRegister, len(chineseActivityPhrases))
	for _, phrase := range chineseActivityPhrases {
		registerByPhrase[phrase.text] = phrase.register
	}
	for seed := uint64(1); seed <= 20; seed++ {
		planner := activityPlanner{randomState: seed}
		recentLimit := min(96, len(chineseActivityPhrases)/2)
		recent := make([]string, 0, recentLimit)
		var last activityRegister
		hasLast := false
		for index := 0; index < 300; index++ {
			phrase := planner.nextPhrase(config.InterfaceLanguageChinese)
			register, exists := registerByPhrase[phrase]
			if !exists {
				t.Fatalf("seed %d selected unknown phrase %q", seed, phrase)
			}
			if containsActivityPhrase(recent, phrase) {
				t.Fatalf("seed %d repeated recent phrase %q at index %d", seed, phrase, index)
			}
			if hasLast && register == last {
				t.Fatalf("seed %d repeated register %d at index %d", seed, register, index)
			}
			recent = append(recent, phrase)
			if len(recent) > recentLimit {
				recent = append([]string(nil), recent[1:]...)
			}
			last = register
			hasLast = true
		}
	}
}

func TestDotFramesCycleFromBlankWithAStableThreeCellSlot(t *testing.T) {
	want := []string{"   ", ".  ", ".. ", "..."}
	for index, frame := range activityDotFrames {
		if ansi.StringWidth(frame) != 3 {
			t.Fatalf("frame %d width = %d: %q", index, ansi.StringWidth(frame), frame)
		}
		if frame != want[index] {
			t.Fatalf("frame %d = %q, want %q", index, frame, want[index])
		}
	}
}

func TestActivityDotAnimationDoesNotMoveThePhrase(t *testing.T) {
	model := Model{noColor: true, activity: activityViewState{visible: true, phrase: "这团线得从里拆…"}}
	want := []string{"这团线得从里拆    ", "这团线得从里拆 .  ", "这团线得从里拆 .. ", "这团线得从里拆 ..."}
	width := -1
	for frame := range activityDotFrames {
		model.activity.frame = frame
		got := ansi.Strip(model.renderActivity(40))
		if got != want[frame] {
			t.Fatalf("frame %d = %q, want %q", frame, got, want[frame])
		}
		if current := ansi.StringWidth(got); width >= 0 && current != width {
			t.Fatalf("frame %d width = %d, want %d", frame, current, width)
		} else {
			width = current
		}
	}
}

func TestActivityPhraseAndDotsUseActivityThemeRole(t *testing.T) {
	for _, name := range theme.SelectableNames() {
		model := Model{width: 40, activity: activityViewState{visible: true, frame: 3, phrase: "Pondering…"}}
		model.applyTheme(name)
		roles := model.visualRoles()
		got := model.renderActivity(40)
		phrase := lipgloss.NewStyle().Foreground(lipgloss.Color(roles.ActivityIndicator)).Render("Pondering ")
		dots := lipgloss.NewStyle().Foreground(lipgloss.Color(roles.ActivityIndicator)).Render("...")
		if got != phrase+dots {
			t.Fatalf("theme=%s activity = %q, want %q", name, got, phrase+dots)
		}
	}
}

func TestActivityFrameMessagesCycleBackToBlank(t *testing.T) {
	model := Model{
		snapshot: ipc.Snapshot{State: "live", ActiveTurnID: "turn"},
		activity: activityViewState{promptID: "turn", generation: 4, visible: true},
	}
	for step, want := range []int{1, 2, 3, 0} {
		updated, command := model.Update(activityFrameMsg{promptID: "turn", generation: 4})
		model = updated.(Model)
		if model.activity.frame != want || command == nil {
			t.Fatalf("step %d frame = %d, command=%v; want frame %d with next tick", step, model.activity.frame, command, want)
		}
	}
}

func TestActivityUsesLatestPromptTailWithoutChangingLayoutHeight(t *testing.T) {
	model := Model{
		width: 48, height: 12, noColor: true, following: true,
		expanded: make(map[string]bool), interfaceLanguage: config.InterfaceLanguageChinese,
		planner: activityPlanner{randomState: 1}, snapshot: ipc.Snapshot{State: "ready"},
	}
	updated, _ := model.Update(snapshotMsg{snapshot: ipc.Snapshot{
		State: "live", ActiveTurnID: "turn-1",
		Prompts: []provider.UserPrompt{{ID: "turn-1", Text: "first"}},
	}})
	model = updated.(Model)
	if model.activity.visible {
		t.Fatal("activity became visible before its initial delay")
	}
	generation := model.activity.generation
	updated, _ = model.Update(activityStartMsg{promptID: "turn-1", generation: generation})
	model = updated.(Model)
	activeLayout := model.layoutBody()
	activityLine := ansi.Strip(activeLayout.lines[1])
	if activeLayout.activityLine != 1 || len(activeLayout.lines) != 2 || strings.Contains(activityLine, ".") || strings.ContainsAny(activityLine, "◆·") || model.activity.phrase == "" {
		t.Fatalf("activity did not occupy the prompt tail: %#v, %q", activeLayout, model.activity.phrase)
	}
	updated, _ = model.Update(activityFrameMsg{promptID: "turn-1", generation: generation})
	model = updated.(Model)
	if firstDot := ansi.Strip(model.layoutBody().lines[1]); !strings.HasSuffix(firstDot, ".  ") {
		t.Fatalf("activity did not advance from blank to one dot: %q", firstDot)
	}

	updated, _ = model.Update(snapshotMsg{snapshot: ipc.Snapshot{
		State: "live", Prompts: []provider.UserPrompt{{ID: "turn-1", Text: "first"}},
	}})
	model = updated.(Model)
	if !model.activity.settling || len(model.layoutBody().lines) != len(activeLayout.lines) {
		t.Fatalf("completion changed layout height or skipped settle: %#v", model.activity)
	}
	updated, _ = model.Update(activityClearMsg{generation: model.activity.generation})
	model = updated.(Model)
	idleLayout := model.layoutBody()
	if model.activity.visible || len(idleLayout.lines) != len(activeLayout.lines) || idleLayout.lines[1] != "" {
		t.Fatalf("idle tail did not return to a stable blank row: %#v, %#v", model.activity, idleLayout)
	}
}

func TestLateActivityMessagesCannotChangeNewTurn(t *testing.T) {
	model := Model{
		width: 48, height: 12, noColor: true, following: true,
		expanded: make(map[string]bool), interfaceLanguage: config.InterfaceLanguageChinese,
		planner: activityPlanner{randomState: 2}, snapshot: ipc.Snapshot{State: "live", ActiveTurnID: "turn-1",
			Prompts: []provider.UserPrompt{{ID: "turn-1", Text: "first"}}},
	}
	model.beginActivity("turn-1")
	oldGeneration := model.activity.generation
	updated, _ := model.Update(snapshotMsg{snapshot: ipc.Snapshot{State: "live", ActiveTurnID: "turn-2", Prompts: []provider.UserPrompt{
		{ID: "turn-1", Text: "first"}, {ID: "turn-2", Text: "second"},
	}}})
	model = updated.(Model)
	updated, _ = model.Update(activityStartMsg{promptID: "turn-1", generation: oldGeneration})
	model = updated.(Model)
	if model.activity.promptID != "turn-2" || model.activity.visible {
		t.Fatalf("late activity message changed new turn: %#v", model.activity)
	}
}

func TestSameTurnSubmissionMovesActivityToLatestPrompt(t *testing.T) {
	model := Model{
		width: 48, height: 12, noColor: true, following: true,
		expanded: make(map[string]bool), interfaceLanguage: config.InterfaceLanguageChinese,
		planner: activityPlanner{randomState: 2}, snapshot: ipc.Snapshot{
			State: "live", ActiveTurnID: "turn-shared", ActivePromptID: "prompt-1",
			Prompts: []provider.UserPrompt{{ID: "prompt-1", Text: "first"}},
		},
	}
	model.beginActivity("prompt-1")
	oldGeneration := model.activity.generation
	updated, _ := model.Update(snapshotMsg{snapshot: ipc.Snapshot{
		State: "live", ActiveTurnID: "turn-shared", ActivePromptID: "prompt-2",
		Prompts: []provider.UserPrompt{{ID: "prompt-1", Text: "first"}, {ID: "prompt-2", Text: "second"}},
	}})
	model = updated.(Model)
	if model.activity.promptID != "prompt-2" || model.selectedID != "prompt-2" {
		t.Fatalf("same-turn activity did not move to latest prompt: %#v", model.activity)
	}
	updated, _ = model.Update(activityStartMsg{promptID: "prompt-1", generation: oldGeneration})
	model = updated.(Model)
	if model.activity.promptID != "prompt-2" || model.activity.visible {
		t.Fatalf("stale prompt activity changed latest submission: %#v", model.activity)
	}
}

func TestSessionBoundaryAndEndClearActivityImmediately(t *testing.T) {
	model := Model{
		width: 48, height: 12, noColor: true, following: true,
		expanded: make(map[string]bool), snapshot: ipc.Snapshot{State: "live", ActiveTurnID: "turn",
			Prompts: []provider.UserPrompt{{ID: "turn", Text: "prompt"}}},
		activity: activityViewState{promptID: "turn", generation: 1, visible: true, phrase: "搁这儿寻思呢…"},
	}
	updated, _ := model.Update(snapshotMsg{snapshot: ipc.Snapshot{State: "live", Notice: "Session resumed. Showing new prompts only."}})
	model = updated.(Model)
	if model.activity.visible || model.activity.settling {
		t.Fatalf("resume retained activity: %#v", model.activity)
	}

	model.snapshot = ipc.Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "turn", Text: "prompt"}}}
	model.activity = activityViewState{generation: 3, visible: true, settling: true}
	updated, _ = model.Update(snapshotMsg{snapshot: ipc.Snapshot{State: "ended", Prompts: []provider.UserPrompt{{ID: "turn", Text: "prompt"}}}})
	model = updated.(Model)
	if model.activity.visible || model.activity.settling {
		t.Fatalf("SessionEnd retained settling activity: %#v", model.activity)
	}
}

func TestActivityTruncatesTextBeforeDotsAndIsExcludedFromSelection(t *testing.T) {
	model := Model{
		width: 20, height: 8, noColor: true, offset: 0,
		expanded: make(map[string]bool), snapshot: ipc.Snapshot{State: "live", ActiveTurnID: "turn-1",
			Prompts: []provider.UserPrompt{{ID: "turn-1", Text: "prompt"}}},
		activity: activityViewState{promptID: "turn-1", visible: true, phrase: "这句话不会完整放进窄栏里…"},
	}
	model.activity.frame = 1
	activity := ansi.Strip(model.renderActivity(15))
	if !strings.Contains(activity, "这句话") || !strings.HasSuffix(activity, ".  ") || strings.ContainsAny(activity, "◆·") || ansi.StringWidth(activity) > 15 {
		t.Fatalf("narrow activity did not preserve truncated text and dots: %q", activity)
	}
	model.selectionStart = textPoint{x: 0, y: 0}
	model.selectionEnd = textPoint{x: 19, y: 1}
	selected := model.selectedText()
	if strings.Contains(selected, "这句话") || strings.Contains(selected, "...") || !strings.Contains(selected, "prompt") {
		t.Fatalf("activity leaked into prompt selection: %q", selected)
	}
}

func TestReducedMotionIgnoresDotTicksAndShowsFullEllipsis(t *testing.T) {
	model := Model{reducedMotion: true, snapshot: ipc.Snapshot{State: "live", ActiveTurnID: "turn"}, activity: activityViewState{
		promptID: "turn", generation: 4, visible: true, frame: 0, phrase: "Pondering…",
	}}
	updated, command := model.Update(activityFrameMsg{promptID: "turn", generation: 4})
	model = updated.(Model)
	if model.activity.frame != 0 || command != nil {
		t.Fatalf("reduced motion advanced dots: frame=%d command=%v", model.activity.frame, command)
	}
	if got := ansi.Strip(model.renderActivity(20)); got != "Pondering ..." {
		t.Fatalf("reduced motion activity = %q", got)
	}
}

func TestActiveViewerFitsRequiredTerminalSizes(t *testing.T) {
	for _, size := range [][2]int{{20, 6}, {24, 10}, {32, 12}, {48, 20}, {80, 24}} {
		model := Model{
			width: size[0], height: size[1], noColor: true, following: true, selectedID: "turn",
			expanded: make(map[string]bool), snapshot: ipc.Snapshot{State: "live", ActiveTurnID: "turn",
				Prompts: []provider.UserPrompt{{ID: "turn", Text: "一条等待中的提示词"}}},
			activity: activityViewState{promptID: "turn", visible: true, frame: 1, phrase: "搁这儿寻思呢…"},
		}
		output := model.render()
		lines := strings.Split(output, "\n")
		if len(lines) != size[1] {
			t.Fatalf("%dx%d rendered %d lines", size[0], size[1], len(lines))
		}
		for index, line := range lines {
			if width := ansi.StringWidth(line); width > size[0] {
				t.Fatalf("%dx%d line %d width = %d: %q", size[0], size[1], index, width, line)
			}
		}
		if !strings.Contains(output, " .") || strings.ContainsAny(output, "◆·") {
			t.Fatalf("%dx%d did not render text and dots: %q", size[0], size[1], output)
		}
	}
}
