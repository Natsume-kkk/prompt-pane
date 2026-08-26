package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTurnObserverErrorsDoNotExposeTranscriptPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private-user-transcript.jsonl")
	observation := TurnObservation{SessionID: "session", TurnID: "turn", TranscriptPath: path}

	if _, err := PrepareTurnObservation(observation); err == nil {
		t.Fatal("missing transcript was accepted")
	} else if strings.Contains(err.Error(), path) || strings.Contains(err.Error(), filepath.Base(path)) {
		t.Fatalf("prepare error exposed its path: %v", err)
	}
	observation.Offset = 0
	if _, err := WaitForTurnEnd(context.Background(), observation); err == nil {
		t.Fatal("missing transcript was observed")
	} else if strings.Contains(err.Error(), path) || strings.Contains(err.Error(), filepath.Base(path)) {
		t.Fatalf("observer error exposed its path: %v", err)
	}
}

func TestWaitForTurnEndDetectsExactInterruptedTurnFromOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "current.jsonl")
	writeSyntheticTranscript(t, path,
		`{"type":"session_meta","payload":{"id":"thr_exact"}}`,
		`{"type":"event_msg","payload":{"type":"turn_aborted","turn_id":"turn_old","reason":"interrupted"}}`,
	)
	observation, err := PrepareTurnObservation(TurnObservation{
		SessionID: "thr_exact", TurnID: "turn_exact", TranscriptPath: path,
	})
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		end TurnEnd
		err error
	}
	results := make(chan result, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		end, waitErr := WaitForTurnEnd(ctx, observation)
		results <- result{end: end, err: waitErr}
	}()

	appendSyntheticTranscript(t, path,
		`{"type":"event_msg","payload":{"type":"user_message","message":"never-inspected"}}`,
		`{"type":"event_msg","payload":{"type":"turn_aborted","turn_id":"turn_other","reason":"interrupted"}}`,
	)
	appendSyntheticBytes(t, path, []byte(`{"type":"event_msg","payload":{"type":"turn_aborted",`))
	time.Sleep(2 * turnObservationPollInterval)
	select {
	case got := <-results:
		t.Fatalf("partial lifecycle record ended observation early: %#v", got)
	default:
	}
	appendSyntheticBytes(t, path, []byte(`"turn_id":"turn_exact","reason":"interrupted"}}`+"\n"))

	got := <-results
	if got.err != nil || got.end != TurnEndAborted {
		t.Fatalf("turn end = %v, err = %v", got.end, got.err)
	}
}

func TestWaitForTurnEndDetectsNormalCompletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "current.jsonl")
	writeSyntheticTranscript(t, path, `{"type":"session_meta","payload":{"id":"thr_exact"}}`)
	observation, err := PrepareTurnObservation(TurnObservation{
		SessionID: "thr_exact", TurnID: "turn_exact", TranscriptPath: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	appendSyntheticTranscript(t, path, `{"type":"event_msg","payload":{"type":"turn_complete","turn_id":"turn_exact"}}`)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	end, err := WaitForTurnEnd(ctx, observation)
	if err != nil || end != TurnEndComplete {
		t.Fatalf("turn end = %v, err = %v", end, err)
	}
}

func TestWaitForTurnEndRejectsMismatchedSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "current.jsonl")
	writeSyntheticTranscript(t, path, `{"type":"session_meta","payload":{"id":"thr_other"}}`)
	observation, err := PrepareTurnObservation(TurnObservation{
		SessionID: "thr_exact", TurnID: "turn_exact", TranscriptPath: path,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := WaitForTurnEnd(ctx, observation); err == nil {
		t.Fatal("mismatched transcript session was accepted")
	}
}

func TestParseTurnObservationRejectsInvalidOffset(t *testing.T) {
	if _, err := ParseTurnObservation([]string{"codex", "thr", "turn", "-1", `C:\synthetic\current.jsonl`}); err == nil {
		t.Fatal("negative transcript offset was accepted")
	}
}

func writeSyntheticTranscript(t *testing.T, path string, lines ...string) {
	t.Helper()
	data := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendSyntheticTranscript(t *testing.T, path string, lines ...string) {
	t.Helper()
	appendSyntheticBytes(t, path, []byte(strings.Join(lines, "\n")+"\n"))
}

func appendSyntheticBytes(t *testing.T, path string, data []byte) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		t.Fatal(fmt.Errorf("sync transcript: %w", err))
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
