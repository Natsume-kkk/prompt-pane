//go:build windows

package command

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Natsume-kkk/prompt-pane/internal/ipc"
	runcontext "github.com/Natsume-kkk/prompt-pane/internal/run"
)

func TestCodexHookReachesAuthenticatedViewer(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	server := ipc.NewServer(run)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	for name, value := range map[string]string{
		runcontext.EnvRunID:    run.ID,
		runcontext.EnvToken:    run.Token,
		runcontext.EnvEndpoint: run.Endpoint,
	} {
		t.Setenv(name, value)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, decoder, err := ipc.Subscribe(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var snapshot ipc.Snapshot
	if err := decoder.Decode(&snapshot); err != nil || snapshot.State != "ready" {
		t.Fatalf("initial snapshot: %#v, %v", snapshot, err)
	}

	inputs := []string{
		`{"session_id":"thr_integration","hook_event_name":"SessionStart","source":"startup"}`,
		`{"session_id":"thr_integration","hook_event_name":"UserPromptSubmit","turn_id":"turn_1","prompt":"中文 prompt\nsecond line"}`,
	}
	for _, input := range inputs {
		var output bytes.Buffer
		app := App{In: strings.NewReader(input), Out: &output, Err: &output}
		if code := app.Execute([]string{"_hook", "codex"}); code != 0 {
			t.Fatalf("hook exit code = %d, output = %q", code, output.String())
		}
		if output.Len() != 0 {
			t.Fatalf("successful hook output = %q", output.String())
		}
	}

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for len(snapshot.Prompts) == 0 {
		if err := decoder.Decode(&snapshot); err != nil {
			t.Fatal(err)
		}
	}
	if snapshot.State != "live" || snapshot.Prompts[0].Text != "中文 prompt\nsecond line" {
		data, _ := json.Marshal(snapshot)
		t.Fatalf("unexpected viewer snapshot: %s", data)
	}
}
