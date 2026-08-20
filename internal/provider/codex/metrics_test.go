package codex

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Natsume-kkk/prompt-pane/internal/provider"
)

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
