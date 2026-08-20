package setupui

import (
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Natsume-kkk/prompt-pane/internal/config"
	"github.com/Natsume-kkk/prompt-pane/internal/theme"
)

const (
	frameInterval = 50 * time.Millisecond
	installHold   = 600 * time.Millisecond
	repairHold    = 50 * time.Millisecond
	installCanvas = 10
	repairCanvas  = 4
	plaqueHeight  = 7
	installBounce = 220 * time.Millisecond
	repairBounce  = 60 * time.Millisecond
)

type Progress struct {
	Step       int
	Steps      int
	Stage      string
	Percent    int
	Downloaded int64
	Total      int64
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

type particle struct {
	x           int
	targetY     int
	startY      int
	threshold   int
	activatedAt time.Time
	duration    time.Duration
	bounce      time.Duration
}

type Model struct {
	events      <-chan workEvent
	width       int
	progress    Progress
	particles   []particle
	variant     string
	layoutWidth int
	now         time.Time
	done        bool
	failed      bool
	workErr     error
	completed   time.Time
	noColor     bool
	finalFrame  bool
	completion  CompletionMode
	themeName   string
	colors      theme.Roles
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
		noColor:    noColor,
		completion: completion,
		themeName:  themeName,
	}
	model.colors = theme.Derive(theme.Resolve(themeName, false))
	return model
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(waitForEvent(m.events), nextTick(), func() tea.Msg { return tea.RequestBackgroundColor() })
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.BackgroundColorMsg:
		m.colors = theme.Derive(theme.Resolve(m.themeName, !msg.IsDark()))
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.ensureParticles()
	case workEvent:
		if msg.progress != nil {
			m.progress = normalize(*msg.progress)
			m.ensureParticles()
			m.activateEligible(m.now)
			return m, waitForEvent(m.events)
		}
		if msg.done {
			m.done = true
			m.workErr = msg.err
			m.failed = msg.err != nil
			m.ensureParticles()
			if m.failed {
				return m, tea.Quit
			}
			m.progress.Percent = 100
			m.prepareFinish()
			m.activateEligible(m.now)
			return m, nil
		}
	case tickMsg:
		m.now = time.Time(msg)
		m.ensureParticles()
		m.activateEligible(m.now)
		if m.done && !m.failed && m.allSettled() {
			if m.completed.IsZero() {
				m.completed = m.now
				m.finalFrame = true
			}
			if m.now.Sub(m.completed) >= m.finalHold() {
				return m, tea.Quit
			}
		}
		return m, nextTick()
	}
	return m, nil
}

func (m Model) View() tea.View {
	if m.finalFrame && m.completion == RepairCompletion {
		return tea.NewView(m.renderCompactCompletion())
	}
	var lines []string
	if m.width >= 20 {
		lines = append(lines, m.renderCanvas()...)
	}
	lines = append(lines, centerLine(m.renderStatus(), m.width))
	if m.finalFrame {
		title, next := completionCopy(m.completion)
		lines = append(lines, "", centerLine(successStyle(m.noColor, m.colors).Render(title), m.width), centerLine(next, m.width))
	}
	return tea.NewView(strings.Join(lines, "\n"))
}

func (m Model) renderCompactCompletion() string {
	title, next := completionCopy(m.completion)
	styledTitle := successStyle(m.noColor, m.colors).Render(title)
	combined := styledTitle + " · " + next
	completion := []string{centerLine(combined, m.width)}
	if ansi.StringWidth(combined) > m.width {
		completion = []string{centerLine(styledTitle, m.width), centerLine(next, m.width)}
	}

	// Keep the inline renderer at the animation frame height until it exits.
	// Shrinking directly to the completion copy can leave old canvas rows in
	// terminals that do not fully erase a resized inline buffer.
	lines := make([]string, m.canvasHeight()+1)
	for index := range lines {
		lines[index] = " "
	}
	start := max(0, (len(lines)-len(completion))/2)
	copy(lines[start:], completion)
	return strings.Join(lines, "\n")
}

func (m *Model) ensureParticles() {
	if m.completion == RepairCompletion {
		m.ensureRepairParticles()
		return
	}
	variant, cells, logoWidth := logoForWidth(m.width)
	if variant == m.variant && m.layoutWidth == m.width {
		return
	}
	m.variant = variant
	m.layoutWidth = m.width
	m.particles = make([]particle, 0, len(cells))
	offset := max(0, (m.width-logoWidth)/2)
	for index, cell := range bottomUpCells(cells) {
		m.particles = append(m.particles, particle{x: offset + cell.x, targetY: (installCanvas-plaqueHeight)/2 + cell.y, startY: -(1 + (index*7)%4)})
	}
	m.configureParticles()
}

func (m *Model) ensureRepairParticles() {
	width := min(18, max(8, m.width-2))
	if m.variant == "repair" && m.layoutWidth == width {
		return
	}
	m.variant = "repair"
	m.layoutWidth = width
	m.particles = make([]particle, width)
	offset := max(0, (m.width-width)/2)
	for index := range m.particles {
		m.particles[index] = particle{x: offset + index, targetY: repairCanvas - 2, startY: -(1 + index%3)}
	}
	m.configureParticles()
}

func (m *Model) configureParticles() {
	animated := make([]int, len(m.particles))
	for index := range animated {
		animated[index] = index
	}
	for rank, index := range animated {
		fraction := float64(rank) / float64(max(1, len(animated)-1))
		threshold := 5 + int(math.Sqrt(fraction)*95)
		duration, bounce := m.particleTiming(rank, len(animated))
		m.particles[index].threshold = threshold
		m.particles[index].duration = duration
		m.particles[index].bounce = bounce
	}
	for index := range m.particles {
		if m.particles[index].duration != 0 {
			continue
		}
		duration, bounce := m.particleTiming(index, len(m.particles))
		m.particles[index].duration = duration
		m.particles[index].bounce = bounce
		m.particles[index].activatedAt = m.now.Add(-duration - bounce - time.Second)
	}
	if m.done {
		m.prepareFinish()
	}
}

func (m Model) particleTiming(index, total int) (time.Duration, time.Duration) {
	fraction := float64(index) / float64(max(1, total-1))
	switch m.completion {
	case RepairCompletion:
		return time.Duration(100-fraction*30) * time.Millisecond, repairBounce
	default:
		return time.Duration(680-fraction*360) * time.Millisecond, installBounce
	}
}

func bottomUpCells(cells []cell) []cell {
	ordered := make([]cell, 0, len(cells))
	for y := plaqueHeight - 1; y >= 0; y-- {
		row := make([]cell, 0)
		for _, cell := range cells {
			if cell.y == y {
				row = append(row, cell)
			}
		}
		// Scatter adjacent columns while keeping every lower row ahead of the
		// next row, so the plaque accumulates upward without filling in stripes.
		sort.SliceStable(row, func(i, j int) bool {
			left := (row[i].x*37 + y*17) % 101
			right := (row[j].x*37 + y*17) % 101
			if left == right {
				return row[i].x < row[j].x
			}
			return left < right
		})
		ordered = append(ordered, row...)
	}
	return ordered
}

func (m *Model) activateEligible(now time.Time) {
	eligible := make([]int, 0)
	for index := range m.particles {
		particle := &m.particles[index]
		if particle.activatedAt.IsZero() && particle.threshold <= m.progress.Percent {
			eligible = append(eligible, index)
		}
	}
	delay := time.Duration(0)
	inWave := 0
	for _, index := range eligible {
		waveSize, gap := m.waveTiming(index)
		if inWave == waveSize {
			delay += gap
			inWave = 0
		}
		m.particles[index].activatedAt = now.Add(delay + time.Duration(inWave)*10*time.Millisecond)
		inWave++
	}
}

func (m Model) waveTiming(index int) (int, time.Duration) {
	if m.done {
		if m.completion == SetupCompletion {
			return 6, 8 * time.Millisecond
		}
		return 6, 5 * time.Millisecond
	}
	if m.completion != SetupCompletion {
		return 6, 20 * time.Millisecond
	}
	fraction := float64(index) / float64(max(1, len(m.particles)-1))
	size := 3 + int(fraction*3)
	gap := time.Duration(110-fraction*80) * time.Millisecond
	return size, gap
}

func (m *Model) prepareFinish() {
	for index := range m.particles {
		particle := &m.particles[index]
		if !particle.activatedAt.IsZero() {
			continue
		}
		switch m.completion {
		case RepairCompletion:
			particle.duration = min(particle.duration, 70*time.Millisecond)
			particle.bounce = min(particle.bounce, 40*time.Millisecond)
		default:
			particle.duration = min(particle.duration, 180*time.Millisecond)
			particle.bounce = min(particle.bounce, 120*time.Millisecond)
		}
	}
}

func (m Model) allSettled() bool {
	if len(m.particles) == 0 {
		return true
	}
	for _, particle := range m.particles {
		if particle.activatedAt.IsZero() || m.now.Before(particle.activatedAt.Add(particle.duration+particle.bounce)) {
			return false
		}
	}
	return true
}

func (m Model) renderCanvas() []string {
	canvas := newPixelCanvas(max(1, m.width), m.canvasHeight())
	previewed := 0
	for index, particle := range m.particles {
		if !particle.activatedAt.IsZero() || m.done || previewed >= 3 {
			continue
		}
		y := previewPosition(index, m.now)
		setPixel(canvas, particle.x, y, m.variant, 1)
		previewed++
	}
	for _, particle := range m.particles {
		y, visible, settled := particlePosition(particle, m.now)
		if !visible {
			continue
		}
		state := uint8(1)
		if settled {
			state = 2
		}
		setPixel(canvas, particle.x, y, m.variant, state)
	}
	lines := make([]string, canvas.height)
	activePixel := activeStyle(m.noColor, m.colors).Render("█")
	settledPixel := activePixel
	if !m.failed {
		settledPixel = logoStyle(m.noColor, m.finalFrame, m.colors).Render("█")
	}
	for row := range canvas.height {
		cells := canvas.row(row)
		last := -1
		for column, state := range cells {
			if state != 0 {
				last = column
			}
		}
		if last < 0 {
			lines[row] = " "
			continue
		}
		var line strings.Builder
		for _, state := range cells[:last+1] {
			switch state {
			case 1:
				line.WriteString(activePixel)
			case 2:
				line.WriteString(settledPixel)
			default:
				line.WriteByte(' ')
			}
		}
		lines[row] = line.String()
	}
	return lines
}

type pixelCanvas struct {
	cells         []uint8
	width, height int
}

func newPixelCanvas(width, height int) pixelCanvas {
	return pixelCanvas{cells: make([]uint8, width*height), width: width, height: height}
}

func (canvas pixelCanvas) row(y int) []uint8 {
	start := y * canvas.width
	return canvas.cells[start : start+canvas.width]
}

func setPixel(canvas pixelCanvas, x, y int, variant string, state uint8) {
	if y < 0 || y >= canvas.height || x < 0 || x >= canvas.width {
		return
	}
	row := canvas.row(y)
	row[x] = max(row[x], state)
	if variant == "wide" && x+1 < canvas.width {
		row[x+1] = max(row[x+1], state)
	}
}

func previewPosition(index int, now time.Time) int {
	phase := (now.UnixMilli()/80 + int64(index*7)) % 24
	return int(phase * phase * 3 / (23 * 23))
}

func (m Model) renderStatus() string {
	progress := fmt.Sprintf("[%d/%d] %s  %d%%", m.progress.Step, m.progress.Steps, m.progress.Stage, m.progress.Percent)
	if m.progress.Downloaded > 0 {
		progress += "  " + formatBytes(m.progress.Downloaded)
		if m.progress.Total > 0 {
			progress += " / " + formatBytes(m.progress.Total)
		}
	}
	available := m.width
	prefix := ""
	if m.failed {
		prefix = "[FAIL] "
		if available > 0 {
			available = max(0, available-ansi.StringWidth(prefix))
		}
	}
	if available > 0 && ansi.StringWidth(progress) > available {
		progress = fmt.Sprintf("[%d/%d] %d%% %s", m.progress.Step, m.progress.Steps, m.progress.Percent, compactStage(m.progress.Stage))
	}
	progress = prefix + progress
	if m.width > 0 {
		progress = ansi.Truncate(progress, m.width, "")
	}
	if m.failed {
		return failureStyle(m.noColor, m.colors).Render(progress)
	}
	return progress
}

func compactStage(stage string) string {
	switch stage {
	case "Checking environment", "Environment ready":
		return "Checking"
	case "Downloading Zellij", "Zellij ready":
		return "Zellij"
	case "Installing Codex plugin", "Codex plugin ready":
		return "Plugin"
	case "Installing codex.pp", "codex.pp ready":
		return "codex.pp"
	case "Verifying installation", "Installation verified":
		return "Verifying"
	default:
		return stage
	}
}

func particlePosition(particle particle, now time.Time) (int, bool, bool) {
	if particle.activatedAt.IsZero() || now.Before(particle.activatedAt) {
		return 0, false, false
	}
	elapsed := now.Sub(particle.activatedAt)
	if elapsed < particle.duration {
		ratio := float64(elapsed) / float64(particle.duration)
		position := float64(particle.startY) + float64(particle.targetY-particle.startY)*ratio*ratio
		return int(position + 0.5), true, false
	}
	if elapsed < particle.duration+particle.bounce {
		half := particle.bounce / 2
		if elapsed-particle.duration < half {
			return particle.targetY - 1, true, false
		}
		return particle.targetY, true, false
	}
	return particle.targetY, true, true
}

func runPlain(out io.Writer, completion CompletionMode, initial Progress, work func(Reporter) error) error {
	last := ""
	report := func(progress Progress) {
		progress = normalize(progress)
		line := fmt.Sprintf("[%d/%d] %s  %d%%", progress.Step, progress.Steps, progress.Stage, progress.Percent)
		if progress.Total > 0 {
			line += fmt.Sprintf("  %s / %s", formatBytes(progress.Downloaded), formatBytes(progress.Total))
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

func (m Model) canvasHeight() int {
	if m.completion == RepairCompletion {
		return repairCanvas
	}
	return installCanvas
}

func (m Model) finalHold() time.Duration {
	switch m.completion {
	case RepairCompletion:
		return repairHold
	default:
		return installHold
	}
}

func centerLine(line string, width int) string {
	if width <= 0 {
		return line
	}
	line = ansi.Truncate(line, width, "")
	padding := max(0, (width-ansi.StringWidth(line))/2)
	return strings.Repeat(" ", padding) + line
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
	progress.Percent = min(100, max(0, progress.Percent))
	progress.Step = max(1, progress.Step)
	progress.Steps = max(progress.Step, progress.Steps)
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

func logoStyle(noColor, bright bool, roles ...theme.Roles) lipgloss.Style {
	style := lipgloss.NewStyle()
	if !noColor {
		style = style.Foreground(lipgloss.Color(setupRoles(roles).Success))
	}
	if bright {
		style = style.Bold(true)
	}
	return style
}

func activeStyle(noColor bool, roles ...theme.Roles) lipgloss.Style {
	style := lipgloss.NewStyle().Faint(true)
	if !noColor {
		style = style.Foreground(lipgloss.Color(setupRoles(roles).Muted))
	}
	return style
}

func successStyle(noColor bool, roles ...theme.Roles) lipgloss.Style {
	style := lipgloss.NewStyle().Bold(true)
	if !noColor {
		style = style.Foreground(lipgloss.Color(setupRoles(roles).Success))
	}
	return style
}

func failureStyle(noColor bool, roles ...theme.Roles) lipgloss.Style {
	style := lipgloss.NewStyle().Bold(true)
	if !noColor {
		style = style.Foreground(lipgloss.Color(setupRoles(roles).Error))
	}
	return style
}

func setupRoles(roles []theme.Roles) theme.Roles {
	if len(roles) > 0 && roles[0].Success != "" {
		return roles[0]
	}
	return theme.Derive(theme.Resolve(theme.Mocha, false))
}

type cell struct{ x, y int }

func logoForWidth(width int) (string, []cell, int) {
	text := "PROMPT PANE"
	scale := 1
	variant := "normal"
	if width >= 110 {
		scale = 2
		variant = "wide"
	} else if width < 60 {
		text = "PP"
		variant = "compact"
	}
	patterns := map[rune][]string{
		'P': {"###.", "#..#", "###.", "#...", "#..."},
		'R': {"###.", "#..#", "###.", "#.#.", "#..#"},
		'O': {".##.", "#..#", "#..#", "#..#", ".##."},
		'M': {"#..#", "####", "#..#", "#..#", "#..#"},
		'T': {"####", ".##.", ".##.", ".##.", ".##."},
		'A': {".##.", "#..#", "####", "#..#", "#..#"},
		'N': {"#..#", "##.#", "#.##", "#..#", "#..#"},
		'E': {"####", "#...", "###.", "#...", "####"},
	}
	cutout := make(map[cell]struct{})
	x := 0
	for index, letter := range text {
		if letter == ' ' {
			x += 3
			continue
		}
		if index > 0 && rune(text[index-1]) != ' ' {
			x++
		}
		for y, row := range patterns[letter] {
			for column, value := range row {
				if value == '#' {
					cutout[cell{x: x + column, y: y}] = struct{}{}
				}
			}
		}
		x += 4
	}

	const padding = 1
	plaqueWidth := x + padding*2
	cells := make([]cell, 0, plaqueWidth*plaqueHeight-len(cutout))
	for y := 0; y < plaqueHeight; y++ {
		for column := 0; column < plaqueWidth; column++ {
			if _, isText := cutout[cell{x: column - padding, y: y - padding}]; isText {
				continue
			}
			cells = append(cells, cell{x: column * scale, y: y})
		}
	}
	return variant, cells, plaqueWidth * scale
}
