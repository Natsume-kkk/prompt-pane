package setupui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Natsume-kkk/prompt-pane/internal/config"
	"github.com/Natsume-kkk/prompt-pane/internal/theme"
)

const (
	frameInterval = 100 * time.Millisecond
	installHold   = 250 * time.Millisecond
	repairHold    = 50 * time.Millisecond
)

type Progress struct {
	Step       int
	Steps      int
	Stage      string
	Downloaded int64
	Total      int64
	Plan       []string
}

type Reporter func(Progress)

type CompletionMode uint8

const (
	SetupCompletion CompletionMode = iota
	RefreshCompletion
	RepairCompletion
)

type workEvent struct {
	progress *Progress
	done     bool
	err      error
}

type tickMsg time.Time

type Model struct {
	events     <-chan workEvent
	width      int
	progress   Progress
	plan       []string
	spinner    int
	now        time.Time
	done       bool
	failed     bool
	workErr    error
	completed  time.Time
	noColor    bool
	finalFrame bool
	completion CompletionMode
	themeName  string
	background string
	colors     theme.Roles
}

func Run(out io.Writer, completion CompletionMode, initial Progress, work func(Reporter) error) error {
	if completion == RefreshCompletion || !interactive(out) {
		return runPlain(out, completion, initial, work)
	}
	// Absorb normal download bursts so stage transitions stay responsive when
	// progress callbacks briefly outpace terminal redraws.
	events := make(chan workEvent, 256)
	workResult := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		err := work(func(progress Progress) {
			select {
			case events <- workEvent{progress: &progress}:
			case <-stopped:
			default:
			}
		})
		workResult <- err
		select {
		case events <- workEvent{done: true, err: err}:
		case <-stopped:
		}
	}()

	model := NewModel(events, completion, initial)
	// Bubble Tea queries terminal capabilities while starting the renderer. Keep
	// its input reader active so replies are consumed before control returns to
	// the shell; the model intentionally ignores ordinary key messages.
	program := tea.NewProgram(model, tea.WithOutput(out))
	finalModel, err := program.Run()
	close(stopped)
	if err != nil {
		fmt.Fprintln(out, "Progress animation stopped; installation continued with no visual updates.")
		return <-workResult
	}
	return finalModel.(Model).workErr
}

func NewModel(events <-chan workEvent, completion CompletionMode, initial Progress) Model {
	_, noColor := os.LookupEnv("NO_COLOR")
	themeName, _, err := config.LoadTheme()
	if err != nil {
		themeName = theme.Auto
	}
	now := time.Now()
	model := Model{
		events:     events,
		width:      80,
		now:        now,
		progress:   normalize(initial),
		plan:       append([]string(nil), initial.Plan...),
		noColor:    noColor,
		completion: completion,
		themeName:  themeName,
	}
	model.colors = theme.Derive(theme.Resolve(themeName, false))
	if len(model.plan) == 0 {
		model.plan = defaultPlan(model.progress.Steps)
	}
	return model
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(waitForEvent(m.events), nextTick(), func() tea.Msg { return tea.RequestBackgroundColor() })
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.BackgroundColorMsg:
		m.background = theme.ColorHex(msg.Color)
		m.colors = theme.Derive(theme.Resolve(m.themeName, !msg.IsDark()))
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case workEvent:
		if msg.progress != nil {
			progress := normalize(*msg.progress)
			if len(progress.Plan) > 0 {
				m.plan = append([]string(nil), progress.Plan...)
			}
			progress.Plan = nil
			m.progress = progress
			return m, waitForEvent(m.events)
		}
		if msg.done {
			m.done = true
			m.workErr = msg.err
			m.failed = msg.err != nil
			if m.failed {
				m.finalFrame = true
				return m, tea.Quit
			}
			m.finalFrame = true
			m.completed = m.now
			return m, nil
		}
	case tickMsg:
		m.now = time.Time(msg)
		m.spinner = (m.spinner + 1) % len(spinnerFrames)
		if m.done && !m.failed && m.now.Sub(m.completed) >= m.finalHold() {
			return m, tea.Quit
		}
		return m, nextTick()
	}
	return m, nil
}

func (m Model) View() tea.View {
	if m.completion == RepairCompletion {
		if m.finalFrame && !m.failed {
			return tea.NewView(m.renderStepperCompletion())
		}
		return tea.NewView(strings.Join(m.renderCurrentStage(), "\n"))
	}
	lines := []string{fitLine(m.stepperBodyStyle(true).Render("Prompt Pane setup"), m.width)}
	lines = append(lines, m.renderSteps()...)
	if m.finalFrame && !m.failed {
		title, next := completionCopy(m.completion)
		lines = append(lines, "", fitLine(m.stepperBodyStyle(true).Render(title), m.width), fitLine(m.stepperBodyStyle(false).Render(next), m.width))
	}
	return tea.NewView(strings.Join(lines, "\n"))
}

func runPlain(out io.Writer, completion CompletionMode, initial Progress, work func(Reporter) error) error {
	last := ""
	report := func(progress Progress) {
		progress = normalize(progress)
		line := fmt.Sprintf("[%d/%d] %s", progress.Step, progress.Steps, progress.Stage)
		if progress.Total > 0 {
			downloaded := min(max(int64(0), progress.Downloaded), progress.Total)
			line += fmt.Sprintf("  %d%%  %s / %s", downloaded*100/progress.Total, formatBytes(downloaded), formatBytes(progress.Total))
		} else if progress.Downloaded > 0 {
			line += "  " + formatBytes(progress.Downloaded)
		}
		if line != last {
			fmt.Fprintln(out, line)
			last = line
		}
	}
	report(initial)
	err := work(report)
	if err == nil {
		title, next := completionCopy(completion)
		fmt.Fprintln(out, title)
		fmt.Fprintln(out, next)
	}
	return err
}

func completionCopy(mode CompletionMode) (string, string) {
	if mode == RepairCompletion {
		return "Repair complete", "Starting Codex…"
	}
	if mode == RefreshCompletion {
		return "Refresh ready", "Running final checks"
	}
	return "Installation ready", "Running final checks"
}

func (m Model) finalHold() time.Duration {
	switch m.completion {
	case RepairCompletion:
		return repairHold
	default:
		return installHold
	}
}

func fitLine(line string, width int) string {
	if width <= 0 {
		return line
	}
	return ansi.Truncate(line, width, "")
}

func waitForEvent(events <-chan workEvent) tea.Cmd {
	return func() tea.Msg { return <-events }
}

func nextTick() tea.Cmd {
	return tea.Tick(frameInterval, func(now time.Time) tea.Msg { return tickMsg(now) })
}

func interactive(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func normalize(progress Progress) Progress {
	progress.Step = max(1, progress.Step)
	progress.Steps = max(progress.Step, progress.Steps)
	progress.Plan = append([]string(nil), progress.Plan...)
	return progress
}

func formatBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	divisor := int64(unit)
	suffix := "KB"
	if value >= unit*unit {
		divisor = unit * unit
		suffix = "MB"
	}
	return fmt.Sprintf("%.1f %s", float64(value)/float64(divisor), suffix)
}
