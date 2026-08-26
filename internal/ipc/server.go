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

	mu             sync.Mutex
	listener       net.Listener
	state          string
	sessionID      string
	activeTurnID   string
	activePromptID string
	promptSequence uint64
	stale          map[string]struct{}
	prompts        []provider.UserPrompt
	notice         string
	metrics        *provider.SessionMetrics
	parent         *sessionView
	viewers        map[net.Conn]struct{}
	turnWatchers   map[turnKey]*turnWatcher
	closed         chan struct{}
	closeOnce      sync.Once
}

var snapshotBufferPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

const maxPooledSnapshotCapacity = 1 << 20

type sessionView struct {
	state          string
	sessionID      string
	activeTurnID   string
	activePromptID string
	prompts        []provider.UserPrompt
	notice         string
	metrics        *provider.SessionMetrics
}

type turnKey struct {
	sessionID string
	turnID    string
}

type turnWatcher struct {
	done chan struct{}
}

func NewServer(run runcontext.Context) *Server {
	return &Server{
		run:          run,
		state:        "ready",
		stale:        make(map[string]struct{}),
		viewers:      make(map[net.Conn]struct{}),
		turnWatchers: make(map[turnKey]*turnWatcher),
		closed:       make(chan struct{}),
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
		s.finishAllTurnWatchersLocked()
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
	case "watch_turn":
		_ = conn.SetReadDeadline(time.Time{})
		s.addTurnWatcher(conn, request.Watch)
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
		// Codex 0.149 marks /side and /btw as ephemeral forks without a transcript.
		// Persistent /new and /fork startups establish a new main view even while
		// the previous session remains live.
		if event.Source == provider.SessionSourceStartup && event.Ephemeral && sessionChanged && s.state == "live" && s.parent == nil {
			parent := s.captureSessionLocked()
			s.parent = &parent
			s.stale[s.sessionID] = struct{}{}
			delete(s.stale, event.SessionID)
			s.state = "live"
			s.sessionID = event.SessionID
			s.activeTurnID = ""
			s.activePromptID = ""
			s.prompts = nil
			s.notice = ""
			s.metrics = cloneMetrics(parent.metrics)
			break
		}
		if s.parent != nil && !sessionChanged && (event.Source == provider.SessionSourceStartup || event.Source == provider.SessionSourceCompact) {
			s.state = "live"
			break
		}
		if sessionChanged || s.parent != nil {
			s.finishAllTurnWatchersLocked()
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
		if event.Prompt == nil || event.Prompt.Text == "" || s.sessionID == "" {
			return false
		}
		turnID := strings.TrimSpace(event.TurnID)
		if turnID == "" {
			// Accept the pre-split internal event shape during an atomic upgrade.
			turnID = strings.TrimSpace(event.Prompt.ID)
		}
		if turnID == "" {
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
		if s.activeTurnID != "" && s.activeTurnID != turnID {
			s.finishTurnWatcherLocked(turnKey{sessionID: s.sessionID, turnID: s.activeTurnID})
		}
		prompt := *event.Prompt
		prompt.ID = s.nextPromptIDLocked()
		s.prompts = append(s.prompts, prompt)
		s.activeTurnID = turnID
		s.activePromptID = prompt.ID
		s.state = "live"
		s.notice = ""
	case provider.TurnCompleted:
		if strings.TrimSpace(event.TurnID) == "" || s.sessionID == "" {
			return false
		}
		if event.SessionID != s.sessionID {
			if s.parent != nil && event.SessionID == s.parent.sessionID {
				s.finishTurnWatcherLocked(turnKey{sessionID: event.SessionID, turnID: event.TurnID})
				if event.TurnID == s.parent.activeTurnID {
					s.parent.activeTurnID = ""
					s.parent.activePromptID = ""
					if event.Metrics != nil {
						s.parent.metrics = cloneMetrics(event.Metrics)
					}
				}
				return true
			}
			_, stale := s.stale[event.SessionID]
			if stale {
				s.finishTurnWatcherLocked(turnKey{sessionID: event.SessionID, turnID: event.TurnID})
			}
			return stale
		}
		s.finishTurnWatcherLocked(turnKey{sessionID: event.SessionID, turnID: event.TurnID})
		if s.state == "ended" || event.TurnID != s.activeTurnID {
			return true
		}
		s.activeTurnID = ""
		s.activePromptID = ""
		if s.parent == nil && event.Metrics != nil {
			s.metrics = cloneMetrics(event.Metrics)
		}
	case provider.SessionEnded:
		if event.SessionID != s.sessionID {
			_, stale := s.stale[event.SessionID]
			if stale {
				s.finishSessionWatchersLocked(event.SessionID)
			}
			return stale
		}
		s.finishSessionWatchersLocked(event.SessionID)
		if s.parent != nil {
			s.restoreParentLocked()
			break
		}
		s.state = "ended"
		s.activeTurnID = ""
		s.activePromptID = ""
		s.notice = ""
	default:
		return false
	}
	s.broadcastLocked()
	return true
}

func (s *Server) resetSessionLocked(notice string) {
	s.finishSessionWatchersLocked(s.sessionID)
	s.prompts = nil
	s.metrics = nil
	s.activeTurnID = ""
	s.activePromptID = ""
	s.notice = notice
}

func (s *Server) nextPromptIDLocked() string {
	s.promptSequence++
	return fmt.Sprintf("prompt-%d", s.promptSequence)
}

func (s *Server) restoreParentLocked() {
	overlayID := s.sessionID
	parentID := s.parent.sessionID
	s.finishSessionWatchersLocked(overlayID)
	s.restoreSessionLocked(*s.parent)
	s.parent = nil
	s.stale[overlayID] = struct{}{}
	delete(s.stale, parentID)
}

func (s *Server) captureSessionLocked() sessionView {
	return sessionView{
		state:          s.state,
		sessionID:      s.sessionID,
		activeTurnID:   s.activeTurnID,
		activePromptID: s.activePromptID,
		prompts:        append([]provider.UserPrompt(nil), s.prompts...),
		notice:         s.notice,
		metrics:        cloneMetrics(s.metrics),
	}
}

func (s *Server) restoreSessionLocked(view sessionView) {
	s.state = view.state
	s.sessionID = view.sessionID
	s.activeTurnID = view.activeTurnID
	s.activePromptID = view.activePromptID
	s.prompts = append([]provider.UserPrompt(nil), view.prompts...)
	s.notice = view.notice
	s.metrics = cloneMetrics(view.metrics)
}

func cloneMetrics(metrics *provider.SessionMetrics) *provider.SessionMetrics {
	if metrics == nil {
		return nil
	}
	copy := *metrics
	copy.Quotas = append([]provider.QuotaWindow(nil), metrics.Quotas...)
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

func (s *Server) addTurnWatcher(conn net.Conn, watch *TurnWatch) {
	if watch == nil || strings.TrimSpace(watch.SessionID) == "" || strings.TrimSpace(watch.TurnID) == "" {
		_ = conn.Close()
		return
	}
	key := turnKey{sessionID: watch.SessionID, turnID: watch.TurnID}
	watcher := &turnWatcher{done: make(chan struct{})}

	s.mu.Lock()
	_, duplicate := s.turnWatchers[key]
	active := s.turnActiveLocked(key)
	select {
	case <-s.closed:
		active = false
	default:
	}
	if active && !duplicate {
		s.turnWatchers[key] = watcher
	}
	s.mu.Unlock()

	if !active || duplicate {
		_ = writeResponse(conn, Response{OK: true, Release: true})
		_ = conn.Close()
		return
	}
	if err := writeResponse(conn, Response{OK: true, Watching: true}); err != nil {
		s.removeTurnWatcher(key, watcher)
		_ = conn.Close()
		return
	}

	disconnected := make(chan struct{}, 1)
	go func() {
		var buffer [1]byte
		_, _ = conn.Read(buffer[:])
		disconnected <- struct{}{}
	}()
	select {
	case <-watcher.done:
		_ = writeResponse(conn, Response{OK: true, Release: true})
	case <-disconnected:
	}
	s.removeTurnWatcher(key, watcher)
	_ = conn.Close()
}

func (s *Server) turnActiveLocked(key turnKey) bool {
	if s.state == "live" && s.sessionID == key.sessionID && s.activeTurnID == key.turnID {
		return true
	}
	return s.parent != nil && s.parent.state == "live" && s.parent.sessionID == key.sessionID && s.parent.activeTurnID == key.turnID
}

func (s *Server) removeTurnWatcher(key turnKey, watcher *turnWatcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.turnWatchers[key] == watcher {
		delete(s.turnWatchers, key)
	}
}

func (s *Server) finishTurnWatcherLocked(key turnKey) {
	watcher, ok := s.turnWatchers[key]
	if !ok {
		return
	}
	delete(s.turnWatchers, key)
	close(watcher.done)
}

func (s *Server) finishSessionWatchersLocked(sessionID string) {
	for key := range s.turnWatchers {
		if key.sessionID == sessionID {
			s.finishTurnWatcherLocked(key)
		}
	}
}

func (s *Server) finishAllTurnWatchersLocked() {
	for key := range s.turnWatchers {
		s.finishTurnWatcherLocked(key)
	}
}

func (s *Server) snapshotLocked() Snapshot {
	prompts := append([]provider.UserPrompt(nil), s.prompts...)
	return Snapshot{
		State:          s.state,
		Prompts:        prompts,
		Notice:         s.notice,
		ActiveTurnID:   s.activeTurnID,
		ActivePromptID: s.activePromptID,
		Metrics:        cloneMetrics(s.metrics),
	}
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

func writeResponse(conn net.Conn, response Response) error {
	_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
	err := json.NewEncoder(conn).Encode(response)
	_ = conn.SetWriteDeadline(time.Time{})
	return err
}

func withEncodedSnapshot(snapshot Snapshot, consume func([]byte) error) error {
	buffer := snapshotBufferPool.Get().(*bytes.Buffer)
	buffer.Reset()
	defer recycleSnapshotBuffer(buffer)
	if err := json.NewEncoder(buffer).Encode(snapshot); err != nil {
		return err
	}
	return consume(buffer.Bytes())
}

func recycleSnapshotBuffer(buffer *bytes.Buffer) {
	data := buffer.Bytes()
	if buffer.Cap() <= maxPooledSnapshotCapacity {
		// Clear the full reusable allocation, including bytes beyond the latest
		// snapshot length that may still contain an older prompt.
		clear(data[:buffer.Cap()])
		buffer.Reset()
		snapshotBufferPool.Put(buffer)
		return
	}
	clear(data)
	buffer.Reset()
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
