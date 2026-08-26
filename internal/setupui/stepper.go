package setupui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Natsume-kkk/prompt-pane/internal/theme"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m Model) renderSteps() []string {
	total := max(m.progress.Steps, len(m.plan))
	lines := make([]string, 0, total+2)
	for index := range total {
		label := planLabel(m.plan, index)
		switch {
		case m.failed && index == m.progress.Step-1:
			lines = append(lines, m.failedStageLine())
			lines = append(lines, m.failureDetail()...)
		case m.done && !m.failed || index < m.progress.Step-1 || index == m.progress.Step-1 && stageComplete(m.progress.Stage):
			lines = append(lines, fitLine(m.stepperGraphicStyle(m.colors.Success).Render("✓")+" "+m.stepperBodyStyle(false).Render(label), m.width))
		case index == m.progress.Step-1:
			lines = append(lines, m.currentStageLine())
		default:
			lines = append(lines, fitLine(m.stepperGraphicStyle(m.colors.Muted).Render("○")+" "+m.stepperBodyStyle(false).Render(label), m.width))
		}
	}
	return lines
}

func (m Model) renderCurrentStage() []string {
	if m.failed {
		return append([]string{m.failedStageLine()}, m.failureDetail()...)
	}
	return []string{m.currentStageLine()}
}

func (m Model) currentStageLine() string {
	marker := m.stepperGraphicStyle(m.colors.FocusMarker).Render(spinnerFrames[m.spinner])
	count := m.stepperSemanticStyle(m.colors.FocusMarker, true).Render(fmt.Sprintf("[%d/%d]", m.progress.Step, m.progress.Steps))
	base := marker + " " + count + " " + m.stepperBodyStyle(false).Render(m.progress.Stage)
	return fitLine(m.appendDownload(base), m.width)
}

func (m Model) failedStageLine() string {
	marker := m.stepperGraphicStyle(m.colors.Error).Render("×")
	status := m.stepperSemanticStyle(m.colors.Error, true).Render("[FAIL]")
	count := m.stepperSemanticStyle(m.colors.FocusMarker, true).Render(fmt.Sprintf("[%d/%d]", m.progress.Step, m.progress.Steps))
	return fitLine(marker+" "+status+" "+count+" "+m.stepperBodyStyle(false).Render(m.progress.Stage), m.width)
}

func (m Model) failureDetail() []string {
	if m.workErr == nil {
		return nil
	}
	plain := wrapPlain("Error: "+m.workErr.Error(), max(1, m.width))
	for index := range plain {
		plain[index] = m.stepperBodyStyle(false).Render(plain[index])
	}
	return plain
}

func (m Model) appendDownload(base string) string {
	if m.progress.Total <= 0 {
		if m.progress.Downloaded > 0 {
			return base + "  " + m.stepperBodyStyle(false).Render(formatBytes(m.progress.Downloaded))
		}
		return base
	}
	downloaded := min(max(int64(0), m.progress.Downloaded), m.progress.Total)
	percent := int(downloaded * 100 / m.progress.Total)
	bytes := fmt.Sprintf("%s / %s", formatBytes(downloaded), formatBytes(m.progress.Total))
	for width := 16; width >= 4; width-- {
		filled := int(float64(downloaded) / float64(m.progress.Total) * float64(width))
		bar := "[" + m.stepperGraphicStyle(m.colors.ProgressFill).Render(strings.Repeat("█", filled)) +
			m.stepperGraphicStyle(m.colors.Muted).Render(strings.Repeat("░", width-filled)) + "]"
		detail := fmt.Sprintf("  %s %d%% %s", bar, percent, m.stepperBodyStyle(false).Render(bytes))
		if m.width <= 0 || ansi.StringWidth(base+detail) <= m.width {
			return base + detail
		}
	}
	for _, detail := range []string{
		fmt.Sprintf("  %d%% %s", percent, m.stepperBodyStyle(false).Render(bytes)),
		fmt.Sprintf("  %d%%", percent),
	} {
		if m.width <= 0 || ansi.StringWidth(base+detail) <= m.width {
			return base + detail
		}
	}
	return base
}

func (m Model) renderStepperCompletion() string {
	title, next := completionCopy(m.completion)
	combined := m.stepperBodyStyle(true).Render(title) + " · " + m.stepperBodyStyle(false).Render(next)
	if m.width <= 0 || ansi.StringWidth(combined) <= m.width {
		return combined
	}
	return fitLine(m.stepperBodyStyle(true).Render(title), m.width) + "\n" + fitLine(m.stepperBodyStyle(false).Render(next), m.width)
}

func (m Model) stepperBodyStyle(bold bool) lipgloss.Style {
	return m.stepperSemanticStyle(m.colors.BodyText, bold)
}

func (m Model) stepperSemanticStyle(color string, bold bool) lipgloss.Style {
	style := lipgloss.NewStyle().Bold(bold)
	if !m.noColor && (m.background == "" || theme.MeetsContrast(color, m.background, theme.MinimumTextContrast)) {
		style = style.Foreground(lipgloss.Color(color))
	}
	return style
}

func (m Model) stepperGraphicStyle(color string) lipgloss.Style {
	style := lipgloss.NewStyle()
	if !m.noColor {
		style = style.Foreground(lipgloss.Color(color))
	}
	return style
}

func defaultPlan(steps int) []string {
	plan := make([]string, max(1, steps))
	for index := range plan {
		plan[index] = fmt.Sprintf("Step %d", index+1)
	}
	return plan
}

func planLabel(plan []string, index int) string {
	if index >= 0 && index < len(plan) && strings.TrimSpace(plan[index]) != "" {
		return plan[index]
	}
	return fmt.Sprintf("Step %d", index+1)
}

func stageComplete(stage string) bool {
	return strings.HasSuffix(stage, " ready") || strings.HasSuffix(stage, " verified")
}

func wrapPlain(text string, width int) []string {
	if width <= 0 || text == "" {
		return []string{text}
	}
	var lines []string
	current := ""
	flush := func() {
		if current != "" {
			lines = append(lines, current)
			current = ""
		}
	}
	for _, word := range strings.Fields(text) {
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if ansi.StringWidth(candidate) <= width {
			current = candidate
			continue
		}
		flush()
		for ansi.StringWidth(word) > width {
			part := ansi.Truncate(word, width, "")
			if part == "" {
				break
			}
			lines = append(lines, part)
			word = strings.TrimPrefix(word, part)
		}
		current = word
	}
	flush()
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}
