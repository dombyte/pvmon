package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeAndShutdown(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := New(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}), time.Second)
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()
	resp, err := http.Get("http://" + ln.Addr().String()) //nolint:noctx // test
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, "ok", string(body))
	require.NoError(t, s.Shutdown(context.Background()))
	require.NoError(t, <-done)
	require.NoError(t, s.Serve(ln), "a shut down server returns nil")
	require.Error(t, New(http.NotFoundHandler(), time.Second).Serve(ln), "closed listener")
}

func TestShutdownDeadline(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	started := make(chan struct{})
	release := make(chan struct{})
	s := New(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	}), time.Minute)
	go func() { _ = s.Serve(ln) }()
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String()) //nolint:noctx // test
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, s.Shutdown(ctx), "a running request outlives the deadline")
	close(release)
}
