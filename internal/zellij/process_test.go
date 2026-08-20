package zellij

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	runcontext "github.com/Natsume-kkk/prompt-pane/internal/run"
)

const (
	zellijHelperMode     = "PROMPT_PANE_ZELLIJ_TEST_HELPER"
	zellijHelperOutput   = "PROMPT_PANE_ZELLIJ_TEST_OUTPUT"
	zellijHelperExitCode = "PROMPT_PANE_ZELLIJ_TEST_EXIT_CODE"
)

func TestMain(m *testing.M) {
	if os.Getenv(zellijHelperMode) != "" {
		fmt.Fprint(os.Stdout, os.Getenv(zellijHelperOutput))
		code, _ := strconv.Atoi(os.Getenv(zellijHelperExitCode))
		os.Exit(code)
	}
	os.Exit(m.Run())
}

func TestCompatibleRequiresExactSupportedVersion(t *testing.T) {
	t.Setenv(zellijHelperMode, "version")
	for _, test := range []struct {
		name   string
		output string
		exit   string
		want   bool
	}{
		{name: "supported", output: "zellij " + Version, want: true},
		{name: "different version", output: "zellij 0.44.2"},
		{name: "failed command", output: "zellij " + Version, exit: "7"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(zellijHelperOutput, test.output)
			t.Setenv(zellijHelperExitCode, test.exit)
			if got := compatible(os.Args[0]); got != test.want {
				t.Fatalf("compatible() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestFindUsesCompatibleExecutableFromPath(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, executableName())
	copyTestExecutable(t, executable)
	t.Setenv("PATH", directory)
	t.Setenv(zellijHelperMode, "version")
	t.Setenv(zellijHelperOutput, "zellij "+Version)
	t.Setenv(zellijHelperExitCode, "0")

	found, err := Find()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(found, executable) {
		t.Fatalf("Find() = %q, want %q", found, executable)
	}
}

func TestLaunchAndClosePanePreserveProcessResult(t *testing.T) {
	t.Setenv(zellijHelperMode, "command")
	t.Setenv(zellijHelperOutput, "helper output")
	t.Setenv(zellijHelperExitCode, "0")
	run := runcontext.Context{ID: strings.Repeat("1", 32), Token: strings.Repeat("2", 64), Endpoint: `\\.\pipe\prompt-pane-test`}
	var stdout bytes.Buffer
	if err := Launch(os.Args[0], os.Args[0], run, []string{"resume"}, strings.NewReader(""), &stdout, &stdout); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if stdout.String() != "helper output" {
		t.Fatalf("Launch() output = %q", stdout.String())
	}
	if err := ClosePane(os.Args[0], "terminal_7"); err != nil {
		t.Fatalf("ClosePane() error = %v", err)
	}

	t.Setenv(zellijHelperExitCode, "7")
	if err := Launch(os.Args[0], os.Args[0], run, nil, strings.NewReader(""), io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "run Zellij workspace") {
		t.Fatalf("Launch() error = %v", err)
	}
	if err := ClosePane(os.Args[0], "terminal_7"); err == nil || !strings.Contains(err.Error(), "close viewer pane") {
		t.Fatalf("ClosePane() error = %v", err)
	}
}

func copyTestExecutable(t *testing.T, destination string) {
	t.Helper()
	source, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	target, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(target, source); err != nil {
		_ = target.Close()
		t.Fatal(err)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
}
