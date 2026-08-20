package ipc

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/Natsume-kkk/prompt-pane/internal/provider"
	runcontext "github.com/Natsume-kkk/prompt-pane/internal/run"
)

type Server struct {
	run runcontext.Context

	mu        sync.Mutex
	listener  net.Listener
	state     string
	sessionID string
	stale     map[string]struct{}
	prompts   []provider.UserPrompt
	seen      map[string]struct{}
	notice    string
	metrics   *provider.SessionMetrics
	parent    *sessionView
	viewers   map[net.Conn]struct{}
	closed    chan struct{}
	closeOnce sync.Once
}

var snapshotBufferPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

type sessionView struct {
	state     string
	sessionID string
	prompts   []provider.UserPrompt
	seen      map[string]struct{}
	notice    string
	metrics   *provider.SessionMetrics
}

func NewServer(run runcontext.Context) *Server {
	return &Server{
		run:     run,
		state:   "ready",
		stale:   make(map[string]struct{}),
		seen:    make(map[string]struct{}),
		viewers: make(map[net.Conn]struct{}),
		closed:  make(chan struct{}),
	}
}

func (s *Server) Start() error {
	listener, err := listen(s.run.Endpoint)
	if err != nil {
		return fmt.Errorf("start local IPC: %w", err)
	}
	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()
	go s.acceptLoop(listener)
	return nil
}

func (s *Server) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		close(s.closed)
		s.mu.Lock()
		if s.listener != nil {
			closeErr = s.listener.Close()
		}
		for viewer := range s.viewers {
			_ = viewer.Close()
		}
		s.viewers = nil
		s.mu.Unlock()
	})
	return closeErr
}

func (s *Server) acceptLoop(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
				continue
			}
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var request Request
	decoder := json.NewDecoder(io.LimitReader(conn, MaxMessageBytes+1))
	if err := decoder.Decode(&request); err != nil || !s.authenticated(request) {
		_ = conn.Close()
		return
	}

	switch request.Type {
	case "event":
		accepted := s.apply(request.Event)
		_ = json.NewEncoder(conn).Encode(Response{OK: accepted})
		_ = conn.Close()
	case "subscribe":
		_ = conn.SetReadDeadline(time.Time{})
		s.addViewer(conn)
	default:
		_ = conn.Close()
	}
}

func (s *Server) authenticated(request Request) bool {
	return request.Version == ProtocolVersion &&
		request.RunID == s.run.ID &&
		len(request.Token) == len(s.run.Token) &&
		subtle.ConstantTimeCompare([]byte(request.Token), []byte(s.run.Token)) == 1
}

func (s *Server) apply(event provider.Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch event.Kind {
	case provider.SessionStarted:
		if strings.TrimSpace(event.SessionID) == "" {
			return false
		}
		switch event.Source {
		case provider.SessionSourceStartup, provider.SessionSourceResume, provider.SessionSourceClear, provider.SessionSourceCompact:
		default:
			return false
		}
		sessionChanged := s.sessionID != "" && s.sessionID != event.SessionID
		// Codex has no side-chat source or exit hook. A different startup while the
		// parent is still live is the only reliable distinction from a new main chat.
		if event.Source == provider.SessionSourceStartup && sessionChanged && s.state == "live" && s.parent == nil {
			parent := s.captureSessionLocked()
			s.parent = &parent
			s.stale[s.sessionID] = struct{}{}
			delete(s.stale, event.SessionID)
			s.state = "live"
			s.sessionID = event.SessionID
			s.prompts = nil
			s.seen = make(map[string]struct{})
			s.notice = ""
			s.metrics = cloneMetrics(parent.metrics)
			break
		}
		if s.parent != nil && !sessionChanged && (event.Source == provider.SessionSourceStartup || event.Source == provider.SessionSourceCompact) {
			s.state = "live"
			break
		}
		s.parent = nil
		if sessionChanged {
			s.stale[s.sessionID] = struct{}{}
		}
		delete(s.stale, event.SessionID)
		switch event.Source {
		case provider.SessionSourceResume:
			s.resetSessionLocked("Session resumed. Showing new prompts only.")
		case provider.SessionSourceClear:
			s.resetSessionLocked("Session cleared. Showing new prompts only.")
		case provider.SessionSourceStartup:
			if sessionChanged {
				s.resetSessionLocked("New session started. Showing new prompts only.")
			} else {
				s.notice = ""
			}
		}
		s.sessionID = event.SessionID
		s.state = "live"
	case provider.PromptSubmitted:
		if event.Prompt == nil || s.sessionID == "" {
			return false
		}
		if event.SessionID != s.sessionID {
			if s.parent != nil && event.SessionID == s.parent.sessionID {
				s.restoreParentLocked()
			} else {
				_, stale := s.stale[event.SessionID]
				return stale
			}
		}
		if s.state == "ended" {
			return true
		}
		if _, exists := s.seen[event.Prompt.ID]; exists {
			return true
		}
		s.seen[event.Prompt.ID] = struct{}{}
		s.prompts = append(s.prompts, *event.Prompt)
		s.state = "live"
		s.notice = ""
	case provider.MetricsUpdated:
		if event.Metrics == nil || s.sessionID == "" || event.SessionID != s.sessionID {
			_, stale := s.stale[event.SessionID]
			return event.Metrics != nil && stale
		}
		if s.state == "ended" {
			return true
		}
		if s.parent != nil {
			return true
		}
		s.metrics = cloneMetrics(event.Metrics)
	case provider.SessionEnded:
		if event.SessionID != s.sessionID {
			_, stale := s.stale[event.SessionID]
			return stale
		}
		if s.parent != nil {
			s.restoreParentLocked()
			break
		}
		s.state = "ended"
		s.notice = ""
	default:
		return false
	}
	s.broadcastLocked()
	return true
}

func (s *Server) resetSessionLocked(notice string) {
	s.prompts = nil
	s.metrics = nil
	s.seen = make(map[string]struct{})
	s.notice = notice
}

func (s *Server) restoreParentLocked() {
	overlayID := s.sessionID
	parentID := s.parent.sessionID
	s.restoreSessionLocked(*s.parent)
	s.parent = nil
	s.stale[overlayID] = struct{}{}
	delete(s.stale, parentID)
}

func (s *Server) captureSessionLocked() sessionView {
	return sessionView{
		state:     s.state,
		sessionID: s.sessionID,
		prompts:   append([]provider.UserPrompt(nil), s.prompts...),
		seen:      cloneSeen(s.seen),
		notice:    s.notice,
		metrics:   cloneMetrics(s.metrics),
	}
}

func (s *Server) restoreSessionLocked(view sessionView) {
	s.state = view.state
	s.sessionID = view.sessionID
	s.prompts = append([]provider.UserPrompt(nil), view.prompts...)
	s.seen = cloneSeen(view.seen)
	s.notice = view.notice
	s.metrics = cloneMetrics(view.metrics)
}

func cloneSeen(source map[string]struct{}) map[string]struct{} {
	copy := make(map[string]struct{}, len(source))
	for id := range source {
		copy[id] = struct{}{}
	}
	return copy
}

func cloneMetrics(metrics *provider.SessionMetrics) *provider.SessionMetrics {
	if metrics == nil {
		return nil
	}
	copy := *metrics
	return &copy
}

func (s *Server) addViewer(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.viewers[conn] = struct{}{}
	if err := writeSnapshot(conn, s.snapshotLocked()); err != nil {
		delete(s.viewers, conn)
		_ = conn.Close()
	}
}

func (s *Server) snapshotLocked() Snapshot {
	prompts := append([]provider.UserPrompt(nil), s.prompts...)
	return Snapshot{State: s.state, Prompts: prompts, Notice: s.notice, Metrics: cloneMetrics(s.metrics)}
}

func (s *Server) broadcastLocked() {
	snapshot := s.snapshotLocked()
	_ = withEncodedSnapshot(snapshot, func(encoded []byte) error {
		for viewer := range s.viewers {
			if err := writeEncodedSnapshot(viewer, encoded); err != nil {
				delete(s.viewers, viewer)
				_ = viewer.Close()
			}
		}
		return nil
	})
}

func writeSnapshot(conn net.Conn, snapshot Snapshot) error {
	return withEncodedSnapshot(snapshot, func(encoded []byte) error {
		return writeEncodedSnapshot(conn, encoded)
	})
}

func withEncodedSnapshot(snapshot Snapshot, consume func([]byte) error) error {
	buffer := snapshotBufferPool.Get().(*bytes.Buffer)
	buffer.Reset()
	defer func() {
		// Pooled capacity may be reused, but prompt bytes must not survive as cached content.
		clear(buffer.Bytes())
		buffer.Reset()
		if buffer.Cap() <= 1<<20 {
			snapshotBufferPool.Put(buffer)
		}
	}()
	if err := json.NewEncoder(buffer).Encode(snapshot); err != nil {
		return err
	}
	return consume(buffer.Bytes())
}

func writeEncodedSnapshot(conn net.Conn, encoded []byte) error {
	_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
	var err error
	for len(encoded) > 0 {
		var written int
		written, err = conn.Write(encoded)
		if err != nil {
			break
		}
		if written == 0 {
			err = io.ErrShortWrite
			break
		}
		encoded = encoded[written:]
	}
	_ = conn.SetWriteDeadline(time.Time{})
	return err
}
