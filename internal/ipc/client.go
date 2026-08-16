package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
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

func HookContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 1500*time.Millisecond)
}
