package ipc

import (
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
	viewers   map[net.Conn]struct{}
	closed    chan struct{}
	closeOnce sync.Once
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

	switch event.Kind {
	case provider.SessionStarted:
		if strings.TrimSpace(event.SessionID) == "" {
			s.mu.Unlock()
			return false
		}
		switch event.Source {
		case provider.SessionSourceStartup, provider.SessionSourceResume, provider.SessionSourceClear, provider.SessionSourceCompact:
		default:
			s.mu.Unlock()
			return false
		}
		sessionChanged := s.sessionID != "" && s.sessionID != event.SessionID
		if sessionChanged {
			s.stale[s.sessionID] = struct{}{}
		}
		delete(s.stale, event.SessionID)
		switch event.Source {
		case provider.SessionSourceResume:
			s.prompts = nil
			s.metrics = nil
			s.seen = make(map[string]struct{})
			s.notice = "Session resumed. Showing new prompts only."
		case provider.SessionSourceClear:
			s.prompts = nil
			s.metrics = nil
			s.seen = make(map[string]struct{})
			s.notice = "Session cleared. Showing new prompts only."
		case provider.SessionSourceStartup:
			if sessionChanged {
				s.prompts = nil
				s.metrics = nil
				s.seen = make(map[string]struct{})
				s.notice = "New session started. Showing new prompts only."
			} else {
				s.notice = ""
			}
		}
		s.sessionID = event.SessionID
		s.state = "live"
	case provider.PromptSubmitted:
		if event.Prompt == nil || s.sessionID == "" || event.SessionID != s.sessionID {
			_, stale := s.stale[event.SessionID]
			s.mu.Unlock()
			return event.Prompt != nil && stale
		}
		if s.state == "ended" {
			s.mu.Unlock()
			return true
		}
		if _, exists := s.seen[event.Prompt.ID]; exists {
			s.mu.Unlock()
			return true
		}
		s.seen[event.Prompt.ID] = struct{}{}
		s.prompts = append(s.prompts, *event.Prompt)
		s.state = "live"
		s.notice = ""
	case provider.MetricsUpdated:
		if event.Metrics == nil || s.sessionID == "" || event.SessionID != s.sessionID {
			_, stale := s.stale[event.SessionID]
			s.mu.Unlock()
			return event.Metrics != nil && stale
		}
		if s.state == "ended" {
			s.mu.Unlock()
			return true
		}
		copy := *event.Metrics
		s.metrics = &copy
	case provider.SessionEnded:
		if event.SessionID != s.sessionID {
			_, stale := s.stale[event.SessionID]
			s.mu.Unlock()
			return stale
		}
		s.state = "ended"
		s.notice = ""
	default:
		s.mu.Unlock()
		return false
	}
	s.broadcastLocked()
	s.mu.Unlock()
	return true
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
	var metrics *provider.SessionMetrics
	if s.metrics != nil {
		copy := *s.metrics
		metrics = &copy
	}
	return Snapshot{State: s.state, Prompts: prompts, Notice: s.notice, Metrics: metrics}
}

func (s *Server) broadcastLocked() {
	snapshot := s.snapshotLocked()
	for viewer := range s.viewers {
		if err := writeSnapshot(viewer, snapshot); err != nil {
			delete(s.viewers, viewer)
			_ = viewer.Close()
		}
	}
}

func writeSnapshot(conn net.Conn, snapshot Snapshot) error {
	_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
	err := json.NewEncoder(conn).Encode(snapshot)
	_ = conn.SetWriteDeadline(time.Time{})
	return err
}
