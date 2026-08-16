//go:build !windows

package ipc

import (
	"context"
	"fmt"
	"net"
)

func listen(string) (net.Listener, error) {
	return nil, fmt.Errorf("local IPC is only implemented on Windows in v1.1.0")
}

func dial(context.Context, string) (net.Conn, error) {
	return nil, fmt.Errorf("local IPC is only implemented on Windows in v1.1.0")
}
