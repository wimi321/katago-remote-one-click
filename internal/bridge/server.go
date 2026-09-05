package bridge

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type Server struct {
	engine          *Engine
	token           string
	maxMessageBytes int64
	logger          *log.Logger
	httpServer      *http.Server

	activeMu      sync.Mutex
	activeSession string
}

func NewServer(engine *Engine, token string, maxMessageBytes int64, logger *log.Logger) (*Server, error) {
	if engine == nil {
		return nil, errors.New("engine is required")
	}
	if len(token) < 32 {
		return nil, errors.New("access token must contain at least 32 characters")
	}
	if maxMessageBytes <= 0 {
		maxMessageBytes = 4 << 20
	}
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	server := &Server{
		engine:          engine,
		token:           token,
		maxMessageBytes: maxMessageBytes,
		logger:          logger,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", server.health)
	mux.HandleFunc("/katago/", server.websocket)
	server.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	return server, nil
}

func (s *Server) Serve(listener net.Listener) error {
	s.logger.Printf("WebSocket bridge listening on %s", listener.Addr())
	err := s.httpServer.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
}

func (s *Server) websocket(w http.ResponseWriter, r *http.Request) {
	provided := strings.TrimPrefix(r.URL.Path, "/katago/")
	if provided == "" || strings.Contains(provided, "/") || !constantTimeEqual(provided, s.token) {
		http.NotFound(w, r)
		return
	}

	connection, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		s.logger.Printf("WebSocket handshake failed: %v", err)
		return
	}
	connection.SetReadLimit(s.maxMessageBytes)
	session := &websocketSender{
		id:         newSessionID(),
		connection: connection,
	}
	if !s.claimSession(session.id) {
		_ = connection.Close(websocket.StatusTryAgainLater, "another client is already connected")
		return
	}
	s.logger.Printf("client connected (session=%s)", session.id)
	defer func() {
		s.engine.CancelSession(session.id)
		s.releaseSession(session.id)
		_ = connection.Close(websocket.StatusNormalClosure, "session ended")
		s.logger.Printf("client disconnected (session=%s)", session.id)
	}()

	for {
		messageType, payload, err := connection.Read(r.Context())
		if err != nil {
			status := websocket.CloseStatus(err)
			if status != websocket.StatusNormalClosure && status != websocket.StatusGoingAway && !errors.Is(err, context.Canceled) {
				s.logger.Printf("client read ended (session=%s, status=%d): %v", session.id, status, err)
			}
			return
		}
		if messageType != websocket.MessageText && messageType != websocket.MessageBinary {
			continue
		}
		for _, line := range strings.Split(string(payload), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if err := s.engine.Submit(r.Context(), session, []byte(line)); err != nil {
				s.logger.Printf("client request failed (session=%s): %v", session.id, err)
				return
			}
		}
	}
}

func (s *Server) claimSession(id string) bool {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	if s.activeSession != "" {
		return false
	}
	s.activeSession = id
	return true
}

func (s *Server) releaseSession(id string) {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	if s.activeSession == id {
		s.activeSession = ""
	}
}

func constantTimeEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func newSessionID() string {
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value)
}

type websocketSender struct {
	id         string
	connection *websocket.Conn
	writeMu    sync.Mutex
}

func (s *websocketSender) SessionID() string {
	return s.id
}

func (s *websocketSender) Send(ctx context.Context, payload []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.connection.Write(ctx, websocket.MessageText, payload)
}

func ProtocolError(id, message string) []byte {
	payload, _ := json.Marshal(map[string]string{"id": id, "error": message})
	return payload
}
