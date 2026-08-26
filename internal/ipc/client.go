package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/Natsume-kkk/prompt-pane/internal/provider"
	runcontext "github.com/Natsume-kkk/prompt-pane/internal/run"
)

func SendEvent(ctx context.Context, run runcontext.Context, event provider.Event) error {
	conn, err := dial(ctx, run.Endpoint)
	if err != nil {
		return fmt.Errorf("connect to Prompt Pane: %w", err)
	}
	defer conn.Close()
	request := Request{Version: ProtocolVersion, RunID: run.ID, Token: run.Token, Type: "event", Event: event}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return fmt.Errorf("send Prompt Pane event: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetReadDeadline(deadline)
	}
	var response Response
	if err := json.NewDecoder(conn).Decode(&response); err != nil || !response.OK {
		return fmt.Errorf("Prompt Pane rejected the event")
	}
	return nil
}

func Subscribe(ctx context.Context, run runcontext.Context) (net.Conn, *json.Decoder, error) {
	conn, err := dial(ctx, run.Endpoint)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to Prompt Pane: %w", err)
	}
	request := Request{Version: ProtocolVersion, RunID: run.ID, Token: run.Token, Type: "subscribe"}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("subscribe to Prompt Pane: %w", err)
	}
	return conn, json.NewDecoder(conn), nil
}

func WaitForTurnRelease(ctx context.Context, run runcontext.Context, sessionID string, turnID string) error {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(turnID) == "" {
		return fmt.Errorf("turn watch is missing required fields")
	}
	conn, err := dial(ctx, run.Endpoint)
	if err != nil {
		return fmt.Errorf("connect to Prompt Pane: %w", err)
	}
	defer conn.Close()
	stopClosing := context.AfterFunc(ctx, func() {
		_ = conn.Close()
	})
	defer stopClosing()

	handshakeDeadline := time.Now().Add(1500 * time.Millisecond)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(handshakeDeadline) {
		handshakeDeadline = deadline
	}
	_ = conn.SetDeadline(handshakeDeadline)
	request := Request{
		Version: ProtocolVersion,
		RunID:   run.ID,
		Token:   run.Token,
		Type:    "watch_turn",
		Watch:   &TurnWatch{SessionID: sessionID, TurnID: turnID},
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return fmt.Errorf("register Prompt Pane turn watch: %w", err)
	}
	decoder := json.NewDecoder(conn)
	var response Response
	if err := decoder.Decode(&response); err != nil || !response.OK {
		return fmt.Errorf("Prompt Pane rejected the turn watch")
	}
	if response.Release {
		return nil
	}
	if !response.Watching {
		return fmt.Errorf("Prompt Pane returned an invalid turn watch response")
	}
	_ = conn.SetDeadline(time.Time{})

	response = Response{}
	if err := decoder.Decode(&response); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("Prompt Pane turn watch ended unexpectedly")
	}
	if !response.OK || !response.Release {
		return fmt.Errorf("Prompt Pane returned an invalid turn release")
	}
	return nil
}

func HookContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 1500*time.Millisecond)
}
