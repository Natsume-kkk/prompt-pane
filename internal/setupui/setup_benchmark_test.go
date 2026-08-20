package setupui

import (
	"testing"
	"time"
)

func BenchmarkRenderCanvas(b *testing.B) {
	model := Model{
		width: 80, now: time.Unix(1_700_000_000, 0), noColor: true,
		progress:   Progress{Step: 2, Steps: 4, Percent: 50},
		completion: SetupCompletion,
	}
	model.ensureParticles()
	for index := range model.particles {
		model.particles[index].activatedAt = model.now.Add(-time.Second)
	}
	b.ReportAllocs()
	for range b.N {
		_ = model.renderCanvas()
	}
}
