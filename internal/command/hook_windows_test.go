//go:build windows

package command

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Natsume-kkk/prompt-pane/internal/ipc"
	"github.com/Natsume-kkk/prompt-pane/internal/provider"
	"github.com/Natsume-kkk/prompt-pane/internal/provider/codex"
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
		`{"session_id":"thr_integration","hook_event_name":"UserPromptSubmit","turn_id":"turn_1","prompt":"queued follow-up"}`,
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
	for len(snapshot.Prompts) < 2 {
		if err := decoder.Decode(&snapshot); err != nil {
			t.Fatal(err)
		}
	}
	if snapshot.State != "live" || snapshot.ActiveTurnID != "turn_1" || snapshot.Prompts[0].Text != "中文 prompt\nsecond line" || snapshot.Prompts[1].Text != "queued follow-up" || snapshot.Prompts[0].ID == snapshot.Prompts[1].ID || snapshot.ActivePromptID != snapshot.Prompts[1].ID {
		data, _ := json.Marshal(snapshot)
		t.Fatalf("unexpected viewer snapshot: %s", data)
	}
}

func TestInterruptedTurnObserverClearsViewerActivity(t *testing.T) {
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
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var snapshot ipc.Snapshot
	if err := decoder.Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if err := ipc.SendEvent(ctx, run, provider.Event{Kind: provider.SessionStarted, SessionID: "thr_interrupt", Source: provider.SessionSourceStartup}); err != nil {
		t.Fatal(err)
	}
	if err := ipc.SendEvent(ctx, run, provider.Event{
		Kind: provider.PromptSubmitted, SessionID: "thr_interrupt",
		Prompt: &provider.UserPrompt{ID: "turn_interrupt", Text: "synthetic prompt"},
	}); err != nil {
		t.Fatal(err)
	}
	for snapshot.ActiveTurnID != "turn_interrupt" {
		if err := decoder.Decode(&snapshot); err != nil {
			t.Fatal(err)
		}
	}
	type snapshotResult struct {
		snapshot ipc.Snapshot
		err      error
	}
	nextSnapshot := make(chan snapshotResult, 1)
	go func() {
		var next ipc.Snapshot
		decodeErr := decoder.Decode(&next)
		nextSnapshot <- snapshotResult{snapshot: next, err: decodeErr}
	}()

	path := filepath.Join(t.TempDir(), "current.jsonl")
	initial := []byte("{\"type\":\"session_meta\",\"payload\":{\"id\":\"thr_interrupt\"}}\n")
	if err := os.WriteFile(path, initial, 0o600); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- observeTurn(run, codex.TurnObservation{
			SessionID: "thr_interrupt", TurnID: "turn_interrupt", TranscriptPath: path, Offset: int64(len(initial)),
		})
	}()

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString("{\"type\":\"event_msg\",\"payload\":{\"type\":\"turn_aborted\",\"turn_id\":\"turn_interrupt\",\"reason\":\"interrupted\"}}\n")
	closeErr := file.Close()
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("observer failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("observer did not exit after the interrupted turn")
	}
	completed := <-nextSnapshot
	if completed.err != nil {
		t.Fatal(completed.err)
	}
	if completed.snapshot.ActiveTurnID != "" {
		t.Fatalf("interrupted turn remained active: %#v", completed.snapshot)
	}
}

func TestTurnObserverExitsWhenStopCompletesWithoutTranscriptRecord(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	server := ipc.NewServer(run)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := ipc.SendEvent(ctx, run, provider.Event{Kind: provider.SessionStarted, SessionID: "thr_stop", Source: provider.SessionSourceStartup}); err != nil {
		t.Fatal(err)
	}
	if err := ipc.SendEvent(ctx, run, provider.Event{
		Kind: provider.PromptSubmitted, SessionID: "thr_stop", TurnID: "turn_stop",
		Prompt: &provider.UserPrompt{Text: "synthetic prompt"},
	}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "current.jsonl")
	initial := []byte("{\"type\":\"session_meta\",\"payload\":{\"id\":\"thr_stop\"}}\n")
	if err := os.WriteFile(path, initial, 0o600); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- observeTurn(run, codex.TurnObservation{
			SessionID: "thr_stop", TurnID: "turn_stop", TranscriptPath: path, Offset: int64(len(initial)),
		})
	}()
	if err := ipc.SendEvent(ctx, run, provider.Event{Kind: provider.TurnCompleted, SessionID: "thr_stop", TurnID: "turn_stop"}); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("observer failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("observer did not exit after exact Stop completion")
	}
}
