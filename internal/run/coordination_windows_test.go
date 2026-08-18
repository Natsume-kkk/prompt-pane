//go:build windows

package run

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Natsume-kkk/prompt-pane/internal/paths"
)

func TestWorkspaceActivityBlocksExclusiveUpdateAcrossProcesses(t *testing.T) {
	if os.Getenv("PROMPT_PANE_COORDINATION_HELPER") == "1" {
		lock, err := AcquireWorkspaceActivity()
		if err != nil {
			os.Exit(2)
		}
		defer lock.Close()
		ready := os.Getenv("PROMPT_PANE_COORDINATION_READY")
		release := os.Getenv("PROMPT_PANE_COORDINATION_RELEASE")
		if err := os.WriteFile(ready, []byte("ready"), 0o600); err != nil {
			os.Exit(3)
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(release); err == nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		os.Exit(4)
	}

	home := t.TempDir()
	t.Setenv(paths.EnvHome, home)
	ready := filepath.Join(home, "ready")
	release := filepath.Join(home, "release")
	command := exec.Command(os.Args[0], "-test.run=^TestWorkspaceActivityBlocksExclusiveUpdateAcrossProcesses$")
	command.Env = append(os.Environ(),
		paths.EnvHome+"="+home,
		"PROMPT_PANE_COORDINATION_HELPER=1",
		"PROMPT_PANE_COORDINATION_READY="+ready,
		"PROMPT_PANE_COORDINATION_RELEASE="+release,
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.WriteFile(release, nil, 0o600)
		_ = command.Wait()
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("workspace helper did not become ready")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := AcquireExclusiveWorkspaceActivity(); !errors.Is(err, ErrWorkspacesActive) {
		t.Fatalf("exclusive update error = %v, want ErrWorkspacesActive", err)
	}
	second, err := AcquireWorkspaceActivity()
	if err != nil {
		t.Fatalf("second shared workspace lock: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second shared workspace lock: %v", err)
	}
}

func TestUpdateGateSerializesRefreshChecks(t *testing.T) {
	t.Setenv(paths.EnvHome, t.TempDir())
	first, err := AcquireUpdateGate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := AcquireUpdateGate(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second update gate error = %v, want context deadline exceeded", err)
	}
}
