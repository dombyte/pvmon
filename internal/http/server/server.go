// Package server runs the HTTP server lifecycle.
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Server wraps http.Server with bounded timeouts.
type Server struct {
	srv *http.Server
}

// Timeouts of the server.
const (
	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 2 * time.Minute
)

// New builds a server; requestTimeout is app.request_timeout.
func New(h http.Handler, requestTimeout time.Duration) *Server {
	return &Server{srv: &http.Server{
		Handler: h, ReadHeaderTimeout: readHeaderTimeout, ReadTimeout: requestTimeout,
		WriteTimeout: requestTimeout, IdleTimeout: idleTimeout,
	}}
}

// Serve accepts connections on ln until Shutdown; it returns nil after a shutdown.
func (s *Server) Serve(ln net.Listener) error {
	if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}

// Shutdown stops accepting and waits for running requests until ctx ends.
func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("server: shutdown: %w", err)
	}
	return nil
}
