package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestOutputEnforcesLimitWithoutBlockingTheCommand(t *testing.T) {
	output, err := helperOutput(t, "large", 32, time.Second)
	if !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("error = %v, want output limit", err)
	}
	if len(output) != 32 {
		t.Fatalf("output length = %d, want 32", len(output))
	}
}

func TestOutputEnforcesContextDeadline(t *testing.T) {
	_, err := helperOutput(t, "sleep", 32, 50*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
}

func TestOutputCombinesStreamsAndPreservesExitCode(t *testing.T) {
	output, err := helperOutput(t, "failure", 32, time.Second)
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 7 {
		t.Fatalf("error = %v, want exit code 7", err)
	}
	for _, want := range []string{"stdout", "stderr"} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("combined output %q is missing %q", output, want)
		}
	}
}

func helperOutput(t *testing.T, mode string, limit int, timeout time.Duration) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return Output(ctx, os.Args[0], []string{"-test.run=^TestOutputHelperProcess$"}, OutputOptions{
		Env:   append(os.Environ(), "PROMPT_PANE_PROCESS_HELPER="+mode),
		Limit: limit,
	})
}

func TestOutputHelperProcess(t *testing.T) {
	switch os.Getenv("PROMPT_PANE_PROCESS_HELPER") {
	case "large":
		fmt.Print(strings.Repeat("x", 4096))
		os.Exit(0)
	case "sleep":
		time.Sleep(2 * time.Second)
		os.Exit(0)
	case "failure":
		fmt.Fprint(os.Stdout, "stdout")
		fmt.Fprint(os.Stderr, "stderr")
		os.Exit(7)
	}
}
