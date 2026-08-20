package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/Natsume-kkk/prompt-pane/internal/provider"
	runcontext "github.com/Natsume-kkk/prompt-pane/internal/run"
)

func BenchmarkBroadcastSnapshot(b *testing.B) {
	server := NewServer(runcontext.Context{})
	server.state = "live"
	server.prompts = make([]provider.UserPrompt, 200)
	for index := range server.prompts {
		server.prompts[index] = provider.UserPrompt{ID: fmt.Sprintf("prompt-%d", index), Text: "中文 prompt with emoji 🚀"}
	}
	for index := range 4 {
		server.viewers[&discardConn{id: index}] = struct{}{}
	}
	b.Run("encode-once", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			server.broadcastLocked()
		}
	})
	b.Run("encode-per-viewer", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			snapshot := server.snapshotLocked()
			for viewer := range server.viewers {
				_ = json.NewEncoder(viewer).Encode(snapshot)
			}
		}
	})
}

type discardConn struct{ id int }

func (*discardConn) Read([]byte) (int, error)         { return 0, nil }
func (*discardConn) Write(data []byte) (int, error)   { return len(data), nil }
func (*discardConn) Close() error                     { return nil }
func (*discardConn) LocalAddr() net.Addr              { return discardAddr("local") }
func (*discardConn) RemoteAddr() net.Addr             { return discardAddr("remote") }
func (*discardConn) SetDeadline(time.Time) error      { return nil }
func (*discardConn) SetReadDeadline(time.Time) error  { return nil }
func (*discardConn) SetWriteDeadline(time.Time) error { return nil }

type discardAddr string

func (address discardAddr) Network() string { return string(address) }
func (address discardAddr) String() string  { return string(address) }
