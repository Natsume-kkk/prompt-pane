package codex

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Natsume-kkk/prompt-pane/internal/provider"
)

func TestReadMetricsErrorDoesNotExposeTranscriptPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private-user-transcript.jsonl")
	_, err := readMetrics(path, "session", "", "")
	if err == nil {
		t.Fatal("missing transcript was accepted")
	}
	if strings.Contains(err.Error(), path) || strings.Contains(err.Error(), filepath.Base(path)) {
		t.Fatalf("transcript error exposed its path: %v", err)
	}
}

func TestReadMetricsAdaptsCurrentQuotaShapesWithoutGuessingModels(t *testing.T) {
	tests := []struct {
		name       string
		rateLimits string
		status     provider.QuotaStatus
		windows    []provider.QuotaWindow
	}{
		{
			name:       "weekly-only active snapshot",
			rateLimits: `,"rate_limits":{"limit_id":"codex","primary":{"used_percent":17,"window_minutes":10080,"resets_at":9999999999}}`,
			status:     provider.QuotaAvailable,
			windows:    []provider.QuotaWindow{{WindowMinutes: 10080, UsedPercent: 17, ResetsAt: 9999999999}},
		},
		{
			name:       "default codex map wins over sparse compatibility view",
			rateLimits: `,"rate_limits":{"limit_id":"codex"},"rate_limits_by_limit_id":{"codex":{"primary":{"used_percent":19,"window_minutes":10080,"resets_at":9999999999}},"codex_bengalfox":{"primary":{"used_percent":30,"window_minutes":300,"resets_at":9999999999},"secondary":{"used_percent":40,"window_minutes":10080,"resets_at":9999999999}}}`,
			status:     provider.QuotaAvailable,
			windows:    []provider.QuotaWindow{{WindowMinutes: 10080, UsedPercent: 19, ResetsAt: 9999999999}},
		},
		{
			name:       "default codex map wins over populated special bucket view",
			rateLimits: `,"rate_limits":{"limit_id":"codex_bengalfox","primary":{"used_percent":0,"window_minutes":300,"resets_at":9999999999},"secondary":{"used_percent":0,"window_minutes":10080,"resets_at":9999999999}},"rate_limits_by_limit_id":{"codex":{"primary":{"used_percent":19,"window_minutes":10080,"resets_at":9999999999}},"codex_bengalfox":{"primary":{"used_percent":0,"window_minutes":300,"resets_at":9999999999},"secondary":{"used_percent":0,"window_minutes":10080,"resets_at":9999999999}}}`,
			status:     provider.QuotaAvailable,
			windows:    []provider.QuotaWindow{{WindowMinutes: 10080, UsedPercent: 19, ResetsAt: 9999999999}},
		},
		{
			name:       "single multi-map entry is unambiguous",
			rateLimits: `,"rate_limits_by_limit_id":{"codex":{"primary":{"used_percent":22,"window_minutes":10080,"resets_at":9999999999}}}`,
			status:     provider.QuotaAvailable,
			windows:    []provider.QuotaWindow{{WindowMinutes: 10080, UsedPercent: 22, ResetsAt: 9999999999}},
		},
		{
			name:       "default codex key wins in multi-bucket snapshot",
			rateLimits: `,"rate_limits_by_limit_id":{"codex":{"primary":{"used_percent":23,"window_minutes":10080,"resets_at":9999999999}},"codex_bengalfox":{"primary":{"used_percent":31,"window_minutes":300,"resets_at":9999999999}}}`,
			status:     provider.QuotaAvailable,
			windows:    []provider.QuotaWindow{{WindowMinutes: 10080, UsedPercent: 23, ResetsAt: 9999999999}},
		},
		{
			name:       "only special bucket is unavailable",
			rateLimits: `,"rate_limits":{"limit_id":"codex_bengalfox","primary":{"used_percent":0,"window_minutes":300,"resets_at":9999999999}},"rate_limits_by_limit_id":{"codex_bengalfox":{"primary":{"used_percent":0,"window_minutes":300,"resets_at":9999999999}}}`,
			status:     provider.QuotaUnavailable,
		},
		{
			name:   "missing quota fields are unavailable",
			status: provider.QuotaUnavailable,
		},
		{
			name:       "expired quota keeps window but resets usage",
			rateLimits: `,"rate_limits":{"limit_id":"codex","primary":{"used_percent":88,"window_minutes":300,"resets_at":1}}`,
			status:     provider.QuotaAvailable,
			windows:    []provider.QuotaWindow{{WindowMinutes: 300, UsedPercent: 0, ResetsAt: 1}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "current.jsonl")
			transcript := strings.Join([]string{
				`{"type":"session_meta","payload":{"id":"thr_exact"}}`,
				`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":1250},"last_token_usage":{"input_tokens":32000},"model_context_window":128000}` + test.rateLimits + `}}`,
			}, "\n")
			if err := os.WriteFile(path, []byte(transcript), 0o600); err != nil {
				t.Fatal(err)
			}
			metrics, err := readMetrics(path, "thr_exact", "", "gpt-5.6")
			if err != nil {
				t.Fatal(err)
			}
			if metrics.TotalTokens != 1250 || metrics.ContextWindow != 128000 || metrics.ContextUsedPercent != 25 {
				t.Fatalf("non-quota metrics were lost: %#v", metrics)
			}
			if metrics.QuotaStatus != test.status || len(metrics.Quotas) != len(test.windows) {
				t.Fatalf("quota state = %#v, want status %q windows %#v", metrics, test.status, test.windows)
			}
			for index := range test.windows {
				if metrics.Quotas[index] != test.windows[index] {
					t.Fatalf("quota %d = %#v, want %#v", index, metrics.Quotas[index], test.windows[index])
				}
			}
		})
	}
}

func TestAddGitMetricsReportsBranchChangesAndUntrackedFiles(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not available")
	}
	repository := t.TempDir()
	runGitTestCommand(t, git, repository, "init", "-b", "main")
	tracked := filepath.Join(repository, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("one\nremove\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, git, repository, "add", "tracked.txt")
	runGitTestCommand(t, git, repository, "-c", "user.name=Prompt Pane", "-c", "user.email=prompt-pane@example.invalid", "commit", "-m", "initial")
	if err := os.WriteFile(tracked, []byte("one\nadded\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "untracked.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	metrics := &provider.SessionMetrics{}
	addGitMetrics(metrics, repository)
	if metrics.Branch != "main*" || metrics.Added != 1 || metrics.Deleted != 1 || metrics.Untracked != 1 {
		t.Fatalf("git metrics = %#v", metrics)
	}
}

func TestAddGitMetricsIgnoresMissingAndNonRepositoryDirectories(t *testing.T) {
	for _, directory := range []string{"", t.TempDir()} {
		metrics := &provider.SessionMetrics{}
		addGitMetrics(metrics, directory)
		if metrics.Branch != "" || metrics.Added != 0 || metrics.Deleted != 0 || metrics.Untracked != 0 {
			t.Fatalf("directory %q produced git metrics %#v", directory, metrics)
		}
	}
}

func runGitTestCommand(t *testing.T, git, repository string, arguments ...string) {
	t.Helper()
	command := exec.Command(git, append([]string{"-C", repository}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
