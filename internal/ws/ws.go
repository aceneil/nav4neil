// Package ws embeds a tiny local-only WebSocket server inside the wznav
// binary. It is the v1 plumbing for the future "TUI ↔ daemon" channel
// (the daemon role is just wznav itself for now). Endpoints exposed on
// 127.0.0.1:<port>:
//
//	GET /health    → JSON: {"ok":true,"pid":N,"context":"<ctx>","port":N}
//	GET /context   → JSON: {"context":"<ctx>"}                  (read)
//	POST /context  → JSON body: {"context":"<ctx>"}            (write)
//
// The server is best-effort: if port WZNAV_WS is busy it walks upward
// (39771, 39772, ...) until one binds. On every failure it logs and
// returns without killing the TUI.
package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// DefaultPort is the initial port we try before walking upwards.
const DefaultPort = 39771

// Server holds the websocket + http plumbing plus the shared "context"
// value the TUI writes when the user picks a server. The zero value is
// not usable — call New.
type Server struct {
	mu      sync.RWMutex
	context string
	port    int
	ln      net.Listener
	srv     *http.Server
	url     string // ws://127.0.0.1:<port>
	stopped chan struct{}
}

// New constructs a Server but does not bind a port. Call Start to bind.
func New(initialContext string) *Server {
	return &Server{
		context: initialContext,
		stopped: make(chan struct{}),
	}
}

// Start binds 127.0.0.1:<port>, walking upward on EADDRINUSE, then serves
// HTTP in a goroutine until Stop is called or the listener is closed.
// base may be overridden via the WZNAV_WS env var (e.g. "39771" or ":0").
func (s *Server) Start(base int) error {
	if base <= 0 {
		base = DefaultPort
	}
	if v := strings.TrimSpace(os.Getenv("WZNAV_WS")); v != "" {
		if v == ":0" {
			base = 0
		} else if n, err := strconv.Atoi(v); err == nil && n >= 0 && n < 65536 {
			base = n
		} else {
			log.Printf("ws: WZNAV_WS=%q is not a valid port, ignoring", v)
		}
	}

	var ln net.Listener
	var err error
	try := base
	for i := 0; i < 32; i++ {
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", try))
		if err == nil {
			break
		}
		if !isAddrInUse(err) || base == 0 {
			return fmt.Errorf("ws: bind 127.0.0.1:%d: %w", try, err)
		}
		try++
	}
	if ln == nil {
		return errors.New("ws: exhausted port walk")
	}

	s.ln = ln
	s.port = ln.Addr().(*net.TCPAddr).Port
	s.url = fmt.Sprintf("ws://127.0.0.1:%d", s.port)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/context", s.handleContext)

	s.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		defer close(s.stopped)
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("ws: serve ended: %v", err)
		}
	}()
	log.Printf("ws: serving on %s", s.url)
	return nil
}

// Stop gracefully shuts the HTTP server down. Safe to call once.
func (s *Server) Stop() {
	if s.srv == nil {
		return
	}
	_ = s.srv.Close()
	select {
	case <-s.stopped:
	case <-time.After(2 * time.Second):
	}
}

// URL returns the bind URL (ws://127.0.0.1:<port>) or "" if not started.
func (s *Server) URL() string { return s.url }

// Port returns the bound port, or 0 if not started.
func (s *Server) Port() int { return s.port }

// SetContext updates the shared context. Thread-safe.
func (s *Server) SetContext(ctx string) {
	s.mu.Lock()
	s.context = ctx
	s.mu.Unlock()
}

// Context returns the current context value.
func (s *Server) Context() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.context
}

// ----- HTTP handlers -------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"pid":     os.Getpid(),
		"context": s.Context(),
		"port":    s.port,
		"version": "wznav-v1",
	})
}

func (s *Server) handleContext(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]string{"context": s.Context()})
	case http.MethodPost:
		var body struct {
			Context string `json:"context"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		body.Context = strings.TrimSpace(body.Context)
		if body.Context == "" {
			http.Error(w, "context required", http.StatusBadRequest)
			return
		}
		s.SetContext(body.Context)
		_ = json.NewEncoder(w).Encode(map[string]string{"context": s.Context()})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// Upgrade is a small helper used by future client code; kept exported for
// unit tests and for the v2 daemon-client package.
var Upgrade = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true }, // local-only
}

func isAddrInUse(err error) bool {
	if err == nil {
		return false
	}
	var sysErr *net.OpError
	if errors.As(err, &sysErr) {
		return true // best-effort; net.ErrClosed vs EADDRINUSE both surface as OpError
	}
	return false
}
