//go:build windows

package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Natsume-kkk/prompt-pane/internal/provider"
	runcontext "github.com/Natsume-kkk/prompt-pane/internal/run"
)

func TestSnapshotJSONOmitsTimestampAndAcceptsLegacyField(t *testing.T) {
	legacy := []byte(`{"state":"live","prompts":[{"id":"turn_1","text":"hello","timestamp":"2026-08-12T01:02:03Z"}],"notice":""}`)
	var snapshot Snapshot
	if err := json.Unmarshal(legacy, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Prompts) != 1 || snapshot.Prompts[0].ID != "turn_1" || snapshot.Prompts[0].Text != "hello" {
		t.Fatalf("legacy snapshot = %#v", snapshot)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`"timestamp"`)) {
		t.Fatalf("new snapshot still contains timestamp: %s", encoded)
	}
}

func TestEncodeSnapshotPreservesNewlineDelimitedWireFormat(t *testing.T) {
	snapshot := Snapshot{State: "live", Prompts: []provider.UserPrompt{{ID: "turn_1", Text: "中文 🚀"}}, Notice: "ready"}
	var encoded []byte
	err := withEncodedSnapshot(snapshot, func(data []byte) error {
		encoded = append(encoded, data...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var expected bytes.Buffer
	if err := json.NewEncoder(&expected).Encode(snapshot); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, expected.Bytes()) {
		t.Fatalf("encoded snapshot = %q, want %q", encoded, expected.Bytes())
	}
}

func TestServerPublishesBoundPrompt(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(run)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, decoder, err := Subscribe(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var initial Snapshot
	if err := decoder.Decode(&initial); err != nil || initial.State != "ready" {
		t.Fatalf("initial snapshot: %#v, %v", initial, err)
	}
	if err := SendEvent(ctx, run, provider.Event{Kind: provider.SessionStarted, SessionID: "thr_1", Source: provider.SessionSourceStartup}); err != nil {
		t.Fatal(err)
	}
	if err := SendEvent(ctx, run, provider.Event{Kind: provider.PromptSubmitted, SessionID: "thr_1", Prompt: &provider.UserPrompt{ID: "turn_1", Text: "hello"}}); err != nil {
		t.Fatal(err)
	}

	var snapshot Snapshot
	for len(snapshot.Prompts) == 0 {
		if err := decoder.Decode(&snapshot); err != nil {
			t.Fatal(err)
		}
	}
	if snapshot.Prompts[0].Text != "hello" {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}

func TestServerRejectsWrongToken(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(run)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	bad := run
	bad.Token = "wrong"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, decoder, err := Subscribe(ctx, bad)
	if err != nil {
		return
	}
	defer conn.Close()
	var snapshot Snapshot
	if err := decoder.Decode(&snapshot); err == nil {
		data, _ := json.Marshal(snapshot)
		t.Fatalf("unauthorized subscriber received data: %s", data)
	}
}

func TestConcurrentRunsDoNotCrossStreams(t *testing.T) {
	type fixture struct {
		run     runcontext.Context
		decoder *json.Decoder
		conn    net.Conn
	}
	fixtures := make([]fixture, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for index := range fixtures {
		run, err := runcontext.New()
		if err != nil {
			t.Fatal(err)
		}
		server := NewServer(run)
		if err := server.Start(); err != nil {
			t.Fatal(err)
		}
		defer server.Close()
		conn, decoder, err := Subscribe(ctx, run)
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		defer conn.Close()
		fixtures[index] = fixture{run: run, decoder: decoder, conn: conn}
		var initial Snapshot
		if err := decoder.Decode(&initial); err != nil {
			t.Fatal(err)
		}
	}

	errors := make(chan error, len(fixtures))
	var workers sync.WaitGroup
	for index := range fixtures {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			sessionID := fmt.Sprintf("session-%d", index)
			if err := SendEvent(ctx, fixtures[index].run, provider.Event{Kind: provider.SessionStarted, SessionID: sessionID, Source: provider.SessionSourceStartup}); err != nil {
				errors <- err
				return
			}
			errors <- SendEvent(ctx, fixtures[index].run, provider.Event{
				Kind: provider.PromptSubmitted, SessionID: sessionID,
				Prompt: &provider.UserPrompt{ID: "turn", Text: fmt.Sprintf("run-%d", index)},
			})
		}(index)
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}

	for index := range fixtures {
		var snapshot Snapshot
		for len(snapshot.Prompts) == 0 {
			if err := fixtures[index].decoder.Decode(&snapshot); err != nil {
				t.Fatal(err)
			}
		}
		want := fmt.Sprintf("run-%d", index)
		if len(snapshot.Prompts) != 1 || snapshot.Prompts[0].Text != want {
			t.Fatalf("run %d received %#v", index, snapshot.Prompts)
		}
	}
}

func TestResumeRebindClearsPromptsAndIgnoresStaleEvents(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(run)
	if !server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "old", Source: "startup"}) {
		t.Fatal("startup session was rejected")
	}
	if !server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "old", Prompt: &provider.UserPrompt{ID: "old-turn", Text: "old"}}) {
		t.Fatal("old session prompt was rejected")
	}
	if !server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "resumed", Source: "resume"}) {
		t.Fatal("resume session was rejected")
	}
	if snapshot := server.snapshotLocked(); snapshot.State != "live" || len(snapshot.Prompts) != 0 {
		t.Fatalf("resume snapshot = %#v", snapshot)
	}
	if !server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "old", Prompt: &provider.UserPrompt{ID: "late-turn", Text: "late"}}) {
		t.Fatal("stale prompt was not ignored successfully")
	}
	if !server.apply(provider.Event{Kind: provider.SessionEnded, SessionID: "old"}) {
		t.Fatal("stale session end was not ignored successfully")
	}
	if snapshot := server.snapshotLocked(); snapshot.State != "live" || len(snapshot.Prompts) != 0 {
		t.Fatalf("stale events changed snapshot = %#v", snapshot)
	}
	if !server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "resumed", Prompt: &provider.UserPrompt{ID: "new-turn", Text: "new"}}) {
		t.Fatal("resumed session prompt was rejected")
	}
	if snapshot := server.snapshotLocked(); snapshot.State != "live" || len(snapshot.Prompts) != 1 || snapshot.Prompts[0].Text != "new" {
		t.Fatalf("resumed snapshot = %#v", snapshot)
	}
}

func TestMetricsFollowSessionResetRules(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(run)
	metrics := &provider.SessionMetrics{Model: "gpt-5.4", TotalTokens: 100}
	server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "one", Source: provider.SessionSourceStartup})
	if !server.apply(provider.Event{Kind: provider.MetricsUpdated, SessionID: "one", Metrics: metrics}) || server.snapshotLocked().Metrics == nil {
		t.Fatal("current-session metrics were not accepted")
	}
	server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "one", Source: provider.SessionSourceCompact})
	if server.snapshotLocked().Metrics == nil {
		t.Fatal("compact discarded current metrics")
	}
	server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "two", Source: provider.SessionSourceResume})
	if server.snapshotLocked().Metrics != nil {
		t.Fatal("resume retained old metrics")
	}
	if !server.apply(provider.Event{Kind: provider.MetricsUpdated, SessionID: "one", Metrics: metrics}) || server.snapshotLocked().Metrics != nil {
		t.Fatal("stale metrics changed the active snapshot")
	}
}

func TestSideChatRestoresParentOnNextPromptAndKeepsMetrics(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(run)
	parentMetrics := &provider.SessionMetrics{Model: "gpt-5.6-sol", TotalTokens: 120}
	sideMetrics := &provider.SessionMetrics{Model: "gpt-5.6-luna", TotalTokens: 12}
	if !server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "parent", Source: provider.SessionSourceStartup}) ||
		!server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "parent", Prompt: &provider.UserPrompt{ID: "parent-1", Text: "before side"}}) ||
		!server.apply(provider.Event{Kind: provider.MetricsUpdated, SessionID: "parent", Metrics: parentMetrics}) ||
		!server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "side", Source: provider.SessionSourceStartup}) {
		t.Fatal("side chat setup was rejected")
	}
	if snapshot := server.snapshotLocked(); len(snapshot.Prompts) != 0 || snapshot.Metrics == nil || snapshot.Metrics.TotalTokens != 120 {
		t.Fatalf("side chat did not inherit parent metrics: %#v", snapshot)
	}
	if !server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "side", Prompt: &provider.UserPrompt{ID: "side-1", Text: "temporary"}}) ||
		!server.apply(provider.Event{Kind: provider.MetricsUpdated, SessionID: "side", Metrics: sideMetrics}) {
		t.Fatal("side chat events were rejected")
	}
	if snapshot := server.snapshotLocked(); len(snapshot.Prompts) != 1 || snapshot.Prompts[0].Text != "temporary" || snapshot.Metrics == nil || snapshot.Metrics.TotalTokens != 120 {
		t.Fatalf("side chat changed frozen parent metrics: %#v", snapshot)
	}
	if !server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "parent", Prompt: &provider.UserPrompt{ID: "parent-2", Text: "after side"}}) {
		t.Fatal("returning parent prompt was rejected")
	}
	if snapshot := server.snapshotLocked(); len(snapshot.Prompts) != 2 || snapshot.Prompts[0].Text != "before side" || snapshot.Prompts[1].Text != "after side" || snapshot.Metrics == nil || snapshot.Metrics.TotalTokens != 120 {
		t.Fatalf("parent snapshot was not restored: %#v", snapshot)
	}
	if !server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "side", Prompt: &provider.UserPrompt{ID: "side-late", Text: "late"}}) {
		t.Fatal("late side prompt was not ignored")
	}
	if snapshot := server.snapshotLocked(); len(snapshot.Prompts) != 2 {
		t.Fatalf("late side prompt changed restored parent: %#v", snapshot)
	}
}

func TestSideChatSessionEndRestoresParentImmediately(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(run)
	if !server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "parent", Source: provider.SessionSourceStartup}) ||
		!server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "parent", Prompt: &provider.UserPrompt{ID: "parent-1", Text: "parent"}}) ||
		!server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "side", Source: provider.SessionSourceStartup}) ||
		!server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "side", Prompt: &provider.UserPrompt{ID: "side-1", Text: "side"}}) ||
		!server.apply(provider.Event{Kind: provider.SessionEnded, SessionID: "side"}) {
		t.Fatal("side chat end sequence was rejected")
	}
	if snapshot := server.snapshotLocked(); snapshot.State != "live" || len(snapshot.Prompts) != 1 || snapshot.Prompts[0].Text != "parent" {
		t.Fatalf("side chat end did not restore parent: %#v", snapshot)
	}
}

func TestServerBroadcastsSameSessionResumeReset(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(run)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, decoder, err := Subscribe(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))

	var snapshot Snapshot
	if err := decoder.Decode(&snapshot); err != nil || snapshot.State != "ready" {
		t.Fatalf("initial snapshot: %#v, %v", snapshot, err)
	}
	if err := SendEvent(ctx, run, provider.Event{Kind: provider.SessionStarted, SessionID: "same", Source: provider.SessionSourceStartup}); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&snapshot); err != nil || snapshot.State != "live" {
		t.Fatalf("startup snapshot: %#v, %v", snapshot, err)
	}
	if err := SendEvent(ctx, run, provider.Event{Kind: provider.PromptSubmitted, SessionID: "same", Prompt: &provider.UserPrompt{ID: "old", Text: "old"}}); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&snapshot); err != nil || len(snapshot.Prompts) != 1 {
		t.Fatalf("prompt snapshot: %#v, %v", snapshot, err)
	}
	if err := SendEvent(ctx, run, provider.Event{Kind: provider.SessionStarted, SessionID: "same", Source: provider.SessionSourceResume}); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&snapshot); err != nil || len(snapshot.Prompts) != 0 || snapshot.Notice != "Session resumed. Showing new prompts only." {
		t.Fatalf("resume snapshot: %#v, %v", snapshot, err)
	}
	if err := SendEvent(ctx, run, provider.Event{Kind: provider.PromptSubmitted, SessionID: "same", Prompt: &provider.UserPrompt{ID: "new", Text: "new"}}); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&snapshot); err != nil || len(snapshot.Prompts) != 1 || snapshot.Prompts[0].Text != "new" || snapshot.Notice != "" {
		t.Fatalf("new prompt snapshot: %#v, %v", snapshot, err)
	}
}

func TestClearResetsPromptsAndAllowsRebind(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(run)
	if !server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "old", Source: provider.SessionSourceStartup}) ||
		!server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "old", Prompt: &provider.UserPrompt{ID: "old-turn", Text: "old"}}) {
		t.Fatal("initial session was rejected")
	}
	if !server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "cleared", Source: provider.SessionSourceClear}) {
		t.Fatal("clear session was rejected")
	}
	if snapshot := server.snapshotLocked(); len(snapshot.Prompts) != 0 || snapshot.Notice != "Session cleared. Showing new prompts only." {
		t.Fatalf("clear snapshot = %#v", snapshot)
	}
	if !server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "old", Prompt: &provider.UserPrompt{ID: "late", Text: "late"}}) {
		t.Fatal("stale pre-clear prompt was not ignored")
	}
}

func TestClearWithSameSessionIDResetsPrompts(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(run)
	if !server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "same", Source: provider.SessionSourceStartup}) ||
		!server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "same", Prompt: &provider.UserPrompt{ID: "old-turn", Text: "old"}}) ||
		!server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "same", Source: provider.SessionSourceClear}) {
		t.Fatal("same-session clear sequence was rejected")
	}
	if snapshot := server.snapshotLocked(); len(snapshot.Prompts) != 0 || snapshot.Notice != "Session cleared. Showing new prompts only." {
		t.Fatalf("same-session clear snapshot = %#v", snapshot)
	}
}

func TestCompactPreservesPromptsAcrossRebind(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(run)
	if !server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "before", Source: provider.SessionSourceStartup}) ||
		!server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "before", Prompt: &provider.UserPrompt{ID: "turn-1", Text: "before compact"}}) {
		t.Fatal("initial session was rejected")
	}
	if !server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "after", Source: provider.SessionSourceCompact}) {
		t.Fatal("compact session was rejected")
	}
	if snapshot := server.snapshotLocked(); len(snapshot.Prompts) != 1 || snapshot.Prompts[0].Text != "before compact" {
		t.Fatalf("compact snapshot = %#v", snapshot)
	}
	if !server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "before", Prompt: &provider.UserPrompt{ID: "late", Text: "late"}}) {
		t.Fatal("stale pre-compact prompt was not ignored")
	}
	if !server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "after", Prompt: &provider.UserPrompt{ID: "turn-2", Text: "after compact"}}) {
		t.Fatal("post-compact prompt was rejected")
	}
	if snapshot := server.snapshotLocked(); len(snapshot.Prompts) != 2 || snapshot.Prompts[1].Text != "after compact" {
		t.Fatalf("post-compact snapshot = %#v", snapshot)
	}
}

func TestRepeatedResumeCanReturnToKnownSession(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(run)
	if !server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "a", Source: provider.SessionSourceStartup}) ||
		!server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "a", Prompt: &provider.UserPrompt{ID: "a-old", Text: "a old"}}) ||
		!server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "b", Source: provider.SessionSourceResume}) ||
		!server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "b", Prompt: &provider.UserPrompt{ID: "b-old", Text: "b old"}}) ||
		!server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "a", Source: provider.SessionSourceResume}) {
		t.Fatal("repeated resume sequence was rejected")
	}
	if !server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "b", Prompt: &provider.UserPrompt{ID: "b-late", Text: "b late"}}) {
		t.Fatal("stale session event was not ignored")
	}
	if !server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "a", Prompt: &provider.UserPrompt{ID: "a-new", Text: "a new"}}) {
		t.Fatal("returned session prompt was rejected")
	}
	if snapshot := server.snapshotLocked(); len(snapshot.Prompts) != 1 || snapshot.Prompts[0].Text != "a new" {
		t.Fatalf("returned session snapshot = %#v", snapshot)
	}
}

func TestSessionStartRejectsInvalidFields(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []provider.Event{
		{Kind: provider.SessionStarted, SessionID: "", Source: provider.SessionSourceStartup},
		{Kind: provider.SessionStarted, SessionID: "current", Source: "fork"},
	} {
		server := NewServer(run)
		if server.apply(event) {
			t.Fatalf("invalid session event was accepted: %#v", event)
		}
	}
}

func TestUnknownSessionEventsAreRejected(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(run)
	if !server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "current", Source: "startup"}) {
		t.Fatal("current session was rejected")
	}
	if server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "unknown", Prompt: &provider.UserPrompt{ID: "turn", Text: "unknown"}}) {
		t.Fatal("unknown prompt was accepted")
	}
	if server.apply(provider.Event{Kind: provider.SessionEnded, SessionID: "unknown"}) {
		t.Fatal("unknown session end was accepted")
	}
}

func TestDifferentStartupAfterSessionEndClearsAndRebinds(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(run)
	if !server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "first", Source: provider.SessionSourceStartup}) ||
		!server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "first", Prompt: &provider.UserPrompt{ID: "old", Text: "old"}}) ||
		!server.apply(provider.Event{Kind: provider.SessionEnded, SessionID: "first"}) {
		t.Fatal("first session was rejected")
	}
	if !server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "second", Source: provider.SessionSourceStartup}) {
		t.Fatal("different startup session was rejected")
	}
	if snapshot := server.snapshotLocked(); snapshot.State != "live" || len(snapshot.Prompts) != 0 || snapshot.Notice != "New session started. Showing new prompts only." {
		t.Fatalf("new session snapshot = %#v", snapshot)
	}
	if !server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "first", Prompt: &provider.UserPrompt{ID: "late", Text: "late"}}) ||
		!server.apply(provider.Event{Kind: provider.SessionEnded, SessionID: "first"}) {
		t.Fatal("stale first session event was not ignored")
	}
	if !server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "second", Prompt: &provider.UserPrompt{ID: "old", Text: "new"}}) {
		t.Fatal("second session prompt was rejected")
	}
	if snapshot := server.snapshotLocked(); snapshot.State != "live" || len(snapshot.Prompts) != 1 || snapshot.Prompts[0].Text != "new" || snapshot.Notice != "" {
		t.Fatalf("second session snapshot = %#v", snapshot)
	}
}

func TestRepeatedStartupForSameSessionPreservesPrompts(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(run)
	if !server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "same", Source: provider.SessionSourceStartup}) ||
		!server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "same", Prompt: &provider.UserPrompt{ID: "turn", Text: "keep"}}) ||
		!server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "same", Source: provider.SessionSourceStartup}) ||
		!server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "same", Prompt: &provider.UserPrompt{ID: "turn", Text: "duplicate"}}) {
		t.Fatal("same-session startup sequence was rejected")
	}
	if snapshot := server.snapshotLocked(); snapshot.State != "live" || len(snapshot.Prompts) != 1 || snapshot.Prompts[0].Text != "keep" || snapshot.Notice != "" {
		t.Fatalf("same-session startup snapshot = %#v", snapshot)
	}
}

func TestPromptAfterSessionEndIsIgnoredUntilSessionStart(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(run)
	if !server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "same", Source: provider.SessionSourceStartup}) ||
		!server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "same", Prompt: &provider.UserPrompt{ID: "before", Text: "before"}}) ||
		!server.apply(provider.Event{Kind: provider.SessionEnded, SessionID: "same"}) {
		t.Fatal("session end sequence was rejected")
	}
	if !server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "same", Prompt: &provider.UserPrompt{ID: "late", Text: "late"}}) {
		t.Fatal("late prompt was not ignored successfully")
	}
	if snapshot := server.snapshotLocked(); snapshot.State != "ended" || len(snapshot.Prompts) != 1 || snapshot.Prompts[0].Text != "before" {
		t.Fatalf("late prompt changed ended snapshot = %#v", snapshot)
	}
	if !server.apply(provider.Event{Kind: provider.SessionStarted, SessionID: "same", Source: provider.SessionSourceResume}) ||
		!server.apply(provider.Event{Kind: provider.PromptSubmitted, SessionID: "same", Prompt: &provider.UserPrompt{ID: "after", Text: "after"}}) {
		t.Fatal("resumed session sequence was rejected")
	}
	if snapshot := server.snapshotLocked(); snapshot.State != "live" || len(snapshot.Prompts) != 1 || snapshot.Prompts[0].Text != "after" {
		t.Fatalf("resumed session snapshot = %#v", snapshot)
	}
}
