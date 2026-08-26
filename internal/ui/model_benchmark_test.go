package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Natsume-kkk/prompt-pane/internal/ipc"
	"github.com/Natsume-kkk/prompt-pane/internal/provider"
	"github.com/Natsume-kkk/prompt-pane/internal/theme"
)

func BenchmarkRenderPromptList(b *testing.B) {
	model := benchmarkModel(200)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = model.render()
	}
}

func BenchmarkRenderThemePage(b *testing.B) {
	model := benchmarkModel(20)
	model.overlay = overlayTheme
	model.beginThemePreview()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = model.render()
	}
}

func BenchmarkWrapMixedText(b *testing.B) {
	text := strings.Repeat("中文 prompt with emoji 🚀 and a long URL https://example.com/path?q=1\n", 20)
	b.ReportAllocs()
	for range b.N {
		_ = wrapMixedText(text, 44)
	}
}

func BenchmarkWrapLongUnbrokenText(b *testing.B) {
	text := strings.Repeat("a", 64<<10)
	b.ReportAllocs()
	for range b.N {
		_ = wrapMixedText(text, 44)
	}
}

func benchmarkModel(promptCount int) Model {
	prompts := make([]provider.UserPrompt, promptCount)
	for index := range prompts {
		prompts[index] = provider.UserPrompt{
			ID:   fmt.Sprintf("prompt-%d", index),
			Text: fmt.Sprintf("%d 中文 prompt with emoji 🚀\nsecond line with https://example.com/%d", index, index),
		}
	}
	model := Model{
		width: 48, height: 20, following: false,
		selectedID: prompts[promptCount-1].ID,
		snapshot: ipc.Snapshot{State: "live", Prompts: prompts, Metrics: &provider.SessionMetrics{
			Branch: "main", Model: "gpt-5.6", TotalTokens: 2_400_000, ContextUsedPercent: 66,
			Quotas: []provider.QuotaWindow{{WindowMinutes: 300, UsedPercent: 42}, {WindowMinutes: 10080, UsedPercent: 73}}, QuotaStatus: provider.QuotaAvailable,
		}},
	}
	model.applyTheme(theme.Mocha)
	return model
}
