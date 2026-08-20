package setupui

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

func TestLogoVariantsFitTheirWidth(t *testing.T) {
	for _, test := range []struct {
		width   int
		variant string
	}{
		{width: 120, variant: "wide"},
		{width: 80, variant: "normal"},
		{width: 40, variant: "compact"},
	} {
		variant, cells, logoWidth := logoForWidth(test.width)
		if variant != test.variant {
			t.Fatalf("width %d variant = %q", test.width, variant)
		}
		if len(cells) == 0 || logoWidth > test.width {
			t.Fatalf("width %d logo width = %d, cells = %d", test.width, logoWidth, len(cells))
		}
	}
}

func TestLogoUsesTextAsNegativeSpace(t *testing.T) {
	_, cells, _ := logoForWidth(80)
	occupied := make(map[cell]bool, len(cells))
	for _, cell := range cells {
		occupied[cell] = true
	}

	if !occupied[cell{x: 0, y: 0}] {
		t.Fatal("plaque background is missing its outer corner")
	}
	if occupied[cell{x: 1, y: 1}] {
		t.Fatal("the first P stroke was filled instead of being cut out")
	}
	if !occupied[cell{x: 2, y: 2}] {
		t.Fatal("the first P counter did not retain the plaque background")
	}
}

func TestParticleFallsBouncesAndSettles(t *testing.T) {
	start := time.Unix(10, 0)
	particle := particle{x: 1, startY: -2, targetY: 7, activatedAt: start, duration: 400 * time.Millisecond, bounce: 220 * time.Millisecond}
	if _, visible, _ := particlePosition(particle, start.Add(-time.Millisecond)); visible {
		t.Fatal("particle was visible before activation")
	}
	if y, visible, settled := particlePosition(particle, start.Add(430*time.Millisecond)); !visible || settled || y != 6 {
		t.Fatalf("bounce position = %d, visible = %v, settled = %v", y, visible, settled)
	}
	if y, visible, settled := particlePosition(particle, start.Add(550*time.Millisecond)); !visible || settled || y != 7 {
		t.Fatalf("return position = %d, visible = %v, settled = %v", y, visible, settled)
	}
	if y, visible, settled := particlePosition(particle, start.Add(time.Second)); !visible || !settled || y != 7 {
		t.Fatalf("settled position = %d, visible = %v, settled = %v", y, visible, settled)
	}
}

func TestParticlesAccumulateFromBottomWithScatteredColumns(t *testing.T) {
	model := NewModel(nil, SetupCompletion, Progress{Step: 1, Steps: 1, Stage: "Checking environment"})
	model.width = 80
	model.ensureParticles()
	if len(model.particles) < 5 {
		t.Fatalf("particles = %d", len(model.particles))
	}

	for index := 1; index < len(model.particles); index++ {
		previous := model.particles[index-1]
		current := model.particles[index]
		if current.targetY > previous.targetY {
			t.Fatalf("particle %d target row %d followed lower row %d", index-1, previous.targetY, current.targetY)
		}
		if current.threshold < previous.threshold {
			t.Fatalf("particle %d threshold %d followed %d", index-1, previous.threshold, current.threshold)
		}
	}

	minimumX, maximumX := model.particles[0].x, model.particles[0].x
	for _, particle := range model.particles[1:5] {
		minimumX = min(minimumX, particle.x)
		maximumX = max(maximumX, particle.x)
	}
	if maximumX-minimumX < 10 {
		t.Fatalf("first wave columns were not scattered: %d..%d", minimumX, maximumX)
	}
}

func TestInstallCadenceAccelerates(t *testing.T) {
	model := NewModel(nil, SetupCompletion, Progress{Step: 1, Steps: 5, Stage: "Checking environment"})
	model.width = 80
	model.ensureParticles()
	first := model.particles[0]
	last := model.particles[len(model.particles)-1]
	if first.duration <= last.duration {
		t.Fatalf("fall duration did not accelerate: first=%s last=%s", first.duration, last.duration)
	}
	earlySize, earlyGap := model.waveTiming(0)
	lateSize, lateGap := model.waveTiming(len(model.particles) - 1)
	if earlySize != 3 || lateSize != 6 || earlyGap <= lateGap {
		t.Fatalf("wave timing did not accelerate: early=%d/%s late=%d/%s", earlySize, earlyGap, lateSize, lateGap)
	}
	eligibleAtHalf := 0
	for _, particle := range model.particles {
		if particle.threshold <= 50 {
			eligibleAtHalf++
		}
	}
	if eligibleAtHalf >= len(model.particles)/2 {
		t.Fatalf("first half of progress activated too many blocks: %d/%d", eligibleAtHalf, len(model.particles))
	}

	updated, _ := model.Update(workEvent{done: true})
	model = updated.(Model)
	finish := model.now
	for _, particle := range model.particles {
		settles := particle.activatedAt.Add(particle.duration + particle.bounce)
		if settles.After(finish) {
			finish = settles
		}
	}
	if catchUp := finish.Sub(model.now); catchUp > 800*time.Millisecond {
		t.Fatalf("install catch-up = %s, maximum 800ms", catchUp)
	}
}

func TestCompletionInitializesAndAcceleratesParticlesBeforeFirstTick(t *testing.T) {
	model := NewModel(nil, SetupCompletion, Progress{Step: 1, Steps: 5, Stage: "Checking environment"})
	updated, _ := model.Update(workEvent{done: true})
	model = updated.(Model)
	if len(model.particles) == 0 {
		t.Fatal("completion did not initialize particles")
	}
	finish := model.now
	for _, particle := range model.particles {
		settles := particle.activatedAt.Add(particle.duration + particle.bounce)
		if settles.After(finish) {
			finish = settles
		}
	}
	if catchUp := finish.Sub(model.now); catchUp > 800*time.Millisecond {
		t.Fatalf("pre-tick install catch-up = %s, maximum 800ms", catchUp)
	}
}

func TestCompletedModelRendersHollowLogoAndCommand(t *testing.T) {
	for _, test := range []struct {
		mode  CompletionMode
		title string
		next  string
	}{
		{SetupCompletion, "Installation ready", "Running final checks"},
		{RepairCompletion, "Repair complete", "Starting Codex…"},
	} {
		for _, width := range []int{20, 40, 80, 120} {
			model := NewModel(nil, test.mode, Progress{Step: 1, Steps: 5, Stage: "Checking environment"})
			model.width = width
			model.progress = Progress{Step: 5, Steps: 5, Stage: "Verifying installation", Percent: 100}
			model.ensureParticles()
			now := time.Now()
			for index := range model.particles {
				model.particles[index].activatedAt = now.Add(-time.Second)
			}
			model.now = now
			model.finalFrame = true
			view := model.View().Content
			if !strings.Contains(view, test.title) || !strings.Contains(view, test.next) {
				t.Fatalf("width %d completed view = %q", width, view)
			}
			if test.mode == SetupCompletion && !strings.Contains(view, "█") {
				t.Fatalf("width %d install completion lost the logo: %q", width, view)
			}
			if test.mode != SetupCompletion && strings.Contains(view, "█") {
				t.Fatalf("width %d compact completion retained the canvas: %q", width, view)
			}
			if test.mode != SetupCompletion {
				wantLines := model.canvasHeight() + 1
				if gotLines := strings.Count(view, "\n") + 1; gotLines != wantLines {
					t.Fatalf("width %d compact completion lines = %d, want %d: %q", width, gotLines, wantLines, view)
				}
			}
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > width {
					t.Fatalf("width %d line exceeds width: %q", width, line)
				}
			}
		}
	}
}

func TestProgressActivatesMultipleColumnsInAcceleratingWaves(t *testing.T) {
	model := NewModel(nil, SetupCompletion, Progress{Step: 1, Steps: 1, Stage: "Checking environment"})
	model.width = 80
	model.progress.Percent = 40
	model.ensureParticles()
	now := time.Now()
	model.activateEligible(now)
	columns := make(map[int]bool)
	var activated []particle
	for _, particle := range model.particles {
		if !particle.activatedAt.IsZero() {
			columns[particle.x] = true
			activated = append(activated, particle)
		}
	}
	if len(activated) < 6 || len(columns) < 3 {
		t.Fatalf("activated = %d, columns = %d", len(activated), len(columns))
	}
	if activated[3].activatedAt.Sub(activated[0].activatedAt) < 100*time.Millisecond {
		t.Fatal("the second opening wave did not start slowly")
	}
}

func TestRepairUsesCompactCatchUp(t *testing.T) {
	model := NewModel(nil, RepairCompletion, Progress{Step: 1, Steps: 3, Stage: "Checking environment"})
	model.width = 80
	model.ensureParticles()
	if rows := strings.Count(model.View().Content, "\n") + 1; rows > repairCanvas+1 {
		t.Fatalf("repair used %d rows, maximum %d", rows, repairCanvas+1)
	}
	updated, _ := model.Update(workEvent{done: true})
	model = updated.(Model)
	finish := model.now
	for _, particle := range model.particles {
		settles := particle.activatedAt.Add(particle.duration + particle.bounce)
		if settles.After(finish) {
			finish = settles
		}
	}
	if catchUp := finish.Sub(model.now) + model.finalHold(); catchUp > 300*time.Millisecond {
		t.Fatalf("repair catch-up = %s, maximum 300ms", catchUp)
	}
}

func TestNonInteractiveProgressUsesPlainLines(t *testing.T) {
	var output bytes.Buffer
	err := Run(&output, SetupCompletion, Progress{Step: 1, Steps: 5, Stage: "Checking environment"}, func(report Reporter) error {
		report(Progress{Step: 1, Steps: 5, Stage: "Checking environment"})
		report(Progress{Step: 2, Steps: 5, Stage: "Downloading Zellij", Percent: 50, Downloaded: 1024, Total: 2048})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Count(got, "[1/5] Checking environment  0%") != 1 {
		t.Fatalf("initial progress was not emitted exactly once: %q", got)
	}
	if !strings.Contains(got, "[2/5] Downloading Zellij  50%  1.0 KB / 2.0 KB") {
		t.Fatalf("plain progress = %q", got)
	}
}

func TestRefreshProgressUsesPlainLines(t *testing.T) {
	var output bytes.Buffer
	err := Run(&output, RefreshCompletion, Progress{Step: 1, Steps: 2, Stage: "Checking environment"}, func(report Reporter) error {
		report(Progress{Step: 2, Steps: 2, Stage: "Installation verified", Percent: 100})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{"[1/2] Checking environment  0%", "[2/2] Installation verified  100%", "Refresh ready\nRunning final checks\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("refresh progress missing %q: %q", want, got)
		}
	}
}

func TestNonInteractiveFailureDoesNotClaimCompletion(t *testing.T) {
	var output bytes.Buffer
	wantErr := "synthetic failure"
	err := Run(&output, SetupCompletion, Progress{Step: 1, Steps: 5, Stage: "Checking environment"}, func(report Reporter) error {
		report(Progress{Step: 3, Steps: 5, Stage: "Installing Codex plugin", Percent: 78})
		return fmt.Errorf("%s", wantErr)
	})
	if err == nil || err.Error() != wantErr {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(output.String(), "Installation ready") {
		t.Fatalf("failure claimed completion: %q", output.String())
	}
}

func TestRepairCompletionStartsCodexWithoutRunInstruction(t *testing.T) {
	var output bytes.Buffer
	err := Run(&output, RepairCompletion, Progress{Step: 1, Steps: 2, Stage: "Checking environment"}, func(report Reporter) error {
		report(Progress{Step: 2, Steps: 2, Stage: "Installation verified", Percent: 100})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "Repair complete") || !strings.Contains(got, "Starting Codex…") || strings.Contains(got, "Run: codex.pp") {
		t.Fatalf("repair completion = %q", got)
	}
}

func TestStatusTruncationIsCenteredAndUTF8Safe(t *testing.T) {
	model := NewModel(nil, SetupCompletion, Progress{Step: 1, Steps: 3, Stage: "Checking environment"})
	model.width = 17
	model.progress = Progress{Step: 2, Steps: 3, Stage: "检查 Unicode environment", Percent: 67}
	line := centerLine(model.renderStatus(), model.width)
	if !utf8.ValidString(line) || lipgloss.Width(line) > model.width {
		t.Fatalf("status line is invalid or too wide: %q", line)
	}
	if got := centerLine("ready", 11); got != "   ready" {
		t.Fatalf("centered line = %q", got)
	}
}

func TestInitialProgressUsesTheActualStageCount(t *testing.T) {
	initial := Progress{Step: 1, Steps: 5, Stage: "Checking environment", Percent: 0}
	model := NewModel(nil, SetupCompletion, initial)
	if model.progress != initial || strings.Contains(model.renderStatus(), "Preparing") {
		t.Fatalf("initial progress = %#v, status = %q", model.progress, model.renderStatus())
	}
}

func TestPixelCanvasClipsCoordinatesAndExpandsWidePixels(t *testing.T) {
	canvas := newPixelCanvas(4, 2)
	setPixel(canvas, -1, 0, "wide", 1)
	setPixel(canvas, 4, 0, "wide", 1)
	setPixel(canvas, 1, 0, "wide", 1)
	setPixel(canvas, 1, 0, "wide", 2)
	if got := canvas.row(0); !slices.Equal(got, []uint8{0, 2, 2, 0}) {
		t.Fatalf("wide row = %v", got)
	}
	if got := canvas.row(1); !slices.Equal(got, []uint8{0, 0, 0, 0}) {
		t.Fatalf("untouched row = %v", got)
	}
}

func TestAllSettledRequiresEveryActivatedParticleToFinish(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	model := Model{now: now}
	if !model.allSettled() {
		t.Fatal("empty particle set was not settled")
	}
	model.particles = []particle{{duration: time.Second}}
	if model.allSettled() {
		t.Fatal("inactive particle was treated as settled")
	}
	model.particles[0].activatedAt = now.Add(-2 * time.Second)
	if !model.allSettled() {
		t.Fatal("finished particle was not settled")
	}
}

func TestFailureStatusStaysExplicitAndDimsSettledBlocks(t *testing.T) {
	for _, noColor := range []bool{false, true} {
		for _, width := range []int{8, 20, 40, 80} {
			model := NewModel(nil, SetupCompletion, Progress{Step: 3, Steps: 5, Stage: "Installing Codex plugin", Percent: 78})
			model.width = width
			model.noColor = noColor
			model.failed = true
			model.done = true
			model.ensureParticles()
			now := time.Now()
			if len(model.particles) > 0 {
				model.particles[0].activatedAt = now.Add(-time.Second)
			}
			model.now = now
			view := model.View().Content
			if !strings.Contains(view, "[FAIL]") || strings.Contains(view, "Installation ready") {
				t.Fatalf("color=%v width=%d failure view = %q", !noColor, width, view)
			}
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > width {
					t.Fatalf("color=%v width=%d line exceeds width: %q", !noColor, width, line)
				}
			}
			if !noColor && width >= 20 && len(model.particles) > 0 && !strings.Contains(view, activeStyle(false).Render("█")) {
				t.Fatalf("width=%d settled failure block was not dimmed: %q", width, view)
			}
		}
	}
}
