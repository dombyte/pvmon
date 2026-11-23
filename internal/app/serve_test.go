package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/app"
	"github.com/dombyte/pvmon/internal/buildinfo"
	"github.com/dombyte/pvmon/internal/collector"
	cmocks "github.com/dombyte/pvmon/internal/collector/mocks"
	"github.com/dombyte/pvmon/internal/config"
	"github.com/dombyte/pvmon/internal/health"
	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/util"
	"github.com/dombyte/pvmon/internal/util/clocktest"
	"github.com/dombyte/pvmon/profiles"
)

const serveYAML = `
app: { port: 18080, log_level: debug }
storage: { path: DATA/pvmon.db }
devices:
  meter: { address: "http://10.0.0.31", profile: shelly/pro-3em, interval: 2s }
meters:
  grid: { device: meter, source: total, critical: true }
`

func loadServeConfig(t *testing.T) *app.Loaded {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yml := strings.ReplaceAll(serveYAML, "DATA", filepath.Join(dir, "data"))
	require.NoError(t, os.WriteFile(path, []byte(yml), 0o600))
	l, err := app.LoadConfig(config.Source{File: path}, profiles.FS())
	require.NoError(t, err)
	return l
}

func fakeSources(t *testing.T) app.SourceFactory {
	t.Helper()
	return func(s *site.Site, c *site.Connection, clock util.Clock, _ zerolog.Logger,
	) (map[string]collector.Source, collector.Transport, error) {
		src := cmocks.NewMockSource(t)
		src.EXPECT().Fetch(mock.Anything).RunAndReturn(
			func(context.Context) (profile.Sample, error) {
				return profile.Sample{At: clock.Now()}, errors.New("device unreachable")
			}).Maybe()
		tr := cmocks.NewMockTransport(t)
		tr.EXPECT().Start().Maybe()
		tr.EXPECT().Stop().Maybe()
		return map[string]collector.Source{c.Devices[0]: src}, tr, nil
	}
}

// lockedBuffer is a log sink that is safe for concurrent writers.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type harness struct {
	ln    net.Listener
	opts  app.ServeOptions
	clock *clocktest.Clock
	out   *lockedBuffer
}

func newServe(t *testing.T, sources app.SourceFactory) *harness {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	clock := clocktest.New(time.Now())
	out := &lockedBuffer{}
	return &harness{ln: ln, clock: clock, out: out, opts: app.ServeOptions{
		Loaded: loadServeConfig(t), Build: buildinfo.New("1.0.0-abc", "abc", ""),
		Stdout: out, Listen: func(string) (net.Listener, error) { return ln, nil },
		Clock: clock, Sources: sources, Location: time.UTC,
	}}
}

func get(t *testing.T, ln net.Listener, path string) (int, string) {
	t.Helper()
	resp, err := http.Get("http://" + ln.Addr().String() + path) //nolint:noctx // test
	require.NoError(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(b)
}

// TestServeStartup is the startup test of cmd/app (AGENTS.md section 9).
func TestServeStartup(t *testing.T) {
	t.Parallel()
	h := newServe(t, fakeSources(t))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, h.opts) }()
	h.clock.BlockUntil(3) // supervisor ticker, collector loop, backup ticker
	code, body := get(t, h.ln, "/api/v1/version")
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, `"version":"1.0.0-abc"`)
	h.clock.Advance(5 * time.Second)
	require.Eventually(t, func() bool {
		_, body := get(t, h.ln, "/health")
		return strings.Contains(body, `"meter":"offline"`)
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done, "a signal ends with exit 0")
	assert.Contains(t, h.out.String(), "shutting down")
}

func TestServeFatal(t *testing.T) {
	t.Parallel()
	broken := func(*site.Site, *site.Connection, util.Clock, zerolog.Logger,
	) (map[string]collector.Source, collector.Transport, error) {
		return nil, nil, errors.New("no transport")
	}
	h := newServe(t, broken)
	err := app.Serve(context.Background(), h.opts)
	require.ErrorIs(t, err, health.ErrHealthFatal)
}

func TestServeErrors(t *testing.T) {
	t.Parallel()
	h := newServe(t, fakeSources(t))
	h.opts.Listen = func(string) (net.Listener, error) { return nil, errors.New("in use") }
	require.ErrorContains(t, app.Serve(context.Background(), h.opts), "listen")

	h = newServe(t, fakeSources(t))
	h.opts.Loaded.Result.Config.App.LogLevel = "loud"
	require.Error(t, app.Serve(context.Background(), h.opts))

	h = newServe(t, fakeSources(t))
	h.opts.Loaded.Result.Config.Storage.Path = "/proc/pvmon/x.db"
	require.ErrorContains(t, app.Serve(context.Background(), h.opts), "data directory")
}

func TestServeDefaultsAndRealSources(t *testing.T) {
	t.Parallel()
	h := newServe(t, nil)
	h.opts.Loaded.Result.Config.App.Port = 0
	opts := app.ServeOptions{Loaded: h.opts.Loaded, Build: h.opts.Build, Stdout: h.out}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, opts) }()
	require.Eventually(t, func() bool { return strings.Contains(h.out.String(), "listening") },
		5*time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)

	l, err := load(t, devYAML)
	require.NoError(t, err)
	for _, c := range l.Site.Connections {
		srcs, tr, err := app.RealSources(l.Site, c, h.clock, zerolog.Nop())
		require.NoError(t, err)
		assert.Len(t, srcs, len(c.Devices))
		tr.Start()
		tr.Stop()
	}
}

func TestServeDatabaseErrors(t *testing.T) {
	t.Parallel()
	h := newServe(t, fakeSources(t))
	path := h.opts.Loaded.Result.Config.Storage.Path
	require.NoError(t, os.MkdirAll(path, 0o750), "a directory is not a database")
	require.Error(t, app.Serve(context.Background(), h.opts))
}
