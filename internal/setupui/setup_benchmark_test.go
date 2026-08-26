package setupui

import (
	"testing"
)

func BenchmarkRenderSteps(b *testing.B) {
	model := Model{
		width: 80, noColor: true,
		progress:   Progress{Step: 2, Steps: 4, Stage: "Downloading Zellij", Downloaded: 1024, Total: 2048},
		plan:       []string{"Environment", "Zellij 0.44.3", "Codex plugin", "Installation verification"},
		completion: SetupCompletion,
	}
	b.ReportAllocs()
	for range b.N {
		_ = model.renderSteps()
	}
}
