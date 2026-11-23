package app_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // driver for reading config_object directly

	"github.com/dombyte/pvmon/internal/app"
)

const adminPassword = "correct horse battery"

// apiClient talks to a running harness as the first admin.
type apiClient struct {
	t    *testing.T
	base string
	http *http.Client
}

func newAPIClient(t *testing.T, ln net.Listener) *apiClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	return &apiClient{
		t: t, base: "http://" + ln.Addr().String(), http: &http.Client{Jar: jar},
	}
}

// do sends a request with the same-origin header the CSRF guard wants.
func (c *apiClient) do(method, path, body string) (int, string) {
	c.t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, c.base+path,
		strings.NewReader(body))
	require.NoError(c.t, err)
	req.Header.Set("Origin", c.base)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	require.NoError(c.t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(c.t, err)
	return resp.StatusCode, string(b)
}

// setup creates the first admin with the setup code from the startup log.
func (c *apiClient) setup(out *lockedBuffer) {
	c.t.Helper()
	var code string
	require.Eventually(c.t, func() bool {
		m := regexp.MustCompile(`"setup_code":"([^"]+)"`).FindStringSubmatch(out.String())
		if m == nil {
			return false
		}
		code = m[1]
		return true
	}, 5*time.Second, 10*time.Millisecond)
	status, body := c.do(http.MethodPost, "/api/v1/setup",
		`{"code":"`+code+`","name":"admin","password":"`+adminPassword+`","lang":"en"}`)
	require.Equal(c.t, http.StatusCreated, status, body)
}

func (c *apiClient) siteField(field string) any {
	c.t.Helper()
	status, body := c.do(http.MethodGet, "/api/v1/site", "")
	require.Equal(c.t, http.StatusOK, status, body)
	var m map[string]any
	require.NoError(c.t, json.Unmarshal([]byte(body), &m))
	return m[field]
}

// runServe starts Serve on a fresh listener; the returned stop ends it with exit 0.
func runServe(t *testing.T, h *harness) (stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	h.ln = ln
	h.opts.Listen = func(string) (net.Listener, error) { return ln, nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, h.opts) }()
	require.Eventually(t, func() bool { return strings.Contains(h.out.String(), "listening") },
		5*time.Second, 10*time.Millisecond)
	return func() {
		cancel()
		require.NoError(t, <-done)
	}
}

func (c *apiClient) healthHas(sub string) func() bool {
	return func() bool {
		_, body := c.do(http.MethodGet, "/health", "")
		return strings.Contains(body, sub)
	}
}

const newDevice = `{"address":"http://10.0.0.32","profile":"shelly/pro-3em",` +
	`"auth":{"user":"u","password":"s3cret-pw"}}`

func TestConfigAPI(t *testing.T) {
	t.Parallel()
	h := newServe(t, fakeSources(t))
	stop := runServe(t, h)
	c := newAPIClient(t, h.ln)
	c.setup(h.out)

	status, body := c.do(http.MethodPut, "/api/v1/config/settings", `{"site.name":"X"}`)
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, "X", c.siteField("name"))
	assert.EqualValues(t, 1, c.siteField("config_rev"))

	status, body = c.do(http.MethodPut, "/api/v1/config/meter/bad",
		`{"device":"ghost","source":"total"}`)
	assert.Equal(t, http.StatusUnprocessableEntity, status, body)
	assert.Contains(t, body, "validation_failed")
	assert.Contains(t, body, "meters.bad")

	status, body = c.do(http.MethodPut, "/api/v1/config/device/meter", newDevice)
	assert.Equal(t, http.StatusConflict, status, body)
	assert.Contains(t, body, "locked_by_file")
	status, _ = c.do(http.MethodPut, "/api/v1/config/settings", `{"unknown.key":1}`)
	assert.Equal(t, http.StatusUnprocessableEntity, status)

	status, body = c.do(http.MethodPut, "/api/v1/config/device/ev", newDevice)
	require.Equal(t, http.StatusOK, status, body)
	status, body = c.do(http.MethodPut, "/api/v1/config/meter/wallbox",
		`{"device":"ev","source":"total","role":"consumer"}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Eventually(t, c.healthHas("collector:http://10.0.0.32"), 5*time.Second,
		10*time.Millisecond)

	status, body = c.do(http.MethodGet, "/api/v1/config", "")
	require.Equal(t, http.StatusOK, status)
	assert.Regexp(t, `"id":"meter"[^}]*"locked":true|"locked":true[^}]*"id":"meter"`, body)
	assert.Contains(t, body, `"id":"ev"`)
	assert.NotContains(t, body, "s3cret-pw")
	status, body = c.do(http.MethodGet, "/api/v1/config/device/ev", "")
	require.Equal(t, http.StatusOK, status, body)
	assert.NotContains(t, body, "s3cret-pw")
	status, _ = c.do(http.MethodGet, "/api/v1/config/device/ghost", "")
	assert.Equal(t, http.StatusNotFound, status)

	status, body = c.do(http.MethodGet, "/api/v1/config/export", "")
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, body, "'***'")
	assert.NotContains(t, body, "s3cret-pw")

	storedBody := configObjectBody(t, h, "ev")
	assert.Contains(t, storedBody, "enc:v1:")
	assert.NotContains(t, storedBody, "s3cret-pw")

	status, body = c.do(http.MethodDelete, "/api/v1/config/device/ev", "")
	assert.Equal(t, http.StatusUnprocessableEntity, status, "the meter still uses it: "+body)
	status, _ = c.do(http.MethodDelete, "/api/v1/config/meter/wallbox", "")
	require.Equal(t, http.StatusNoContent, status)
	status, _ = c.do(http.MethodDelete, "/api/v1/config/device/ev", "")
	require.Equal(t, http.StatusNoContent, status)
	require.Eventually(t, func() bool {
		return !c.healthHas("collector:http://10.0.0.32")()
	}, 5*time.Second, 10*time.Millisecond)
	status, _ = c.do(http.MethodDelete, "/api/v1/config/device/meter", "")
	assert.Equal(t, http.StatusConflict, status)

	stop()
}

// configObjectBody reads the stored body of a device straight from the database.
func configObjectBody(t *testing.T, h *harness, id string) string {
	t.Helper()
	path := h.opts.Loaded.Result.Config.Storage.Path
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	require.NoError(t, err)
	defer db.Close()
	var b string
	require.NoError(t, db.QueryRow(
		`SELECT body FROM config_object WHERE id = ?`, id).Scan(&b))
	return b
}

func TestConfigPersistsAcrossRestart(t *testing.T) {
	t.Parallel()
	h := newServe(t, fakeSources(t))
	stop := runServe(t, h)
	c := newAPIClient(t, h.ln)
	c.setup(h.out)
	status, body := c.do(http.MethodPut, "/api/v1/config/settings", `{"site.name":"X"}`)
	require.Equal(t, http.StatusOK, status, body)
	stop()

	stop = runServe(t, h)
	defer stop()
	c = newAPIClient(t, h.ln)
	assert.Equal(t, "X", c.siteField("name"))
	assert.EqualValues(t, 1, c.siteField("config_rev"))
}

func TestConfigWebSocketSiteChanged(t *testing.T) {
	t.Parallel()
	h := newServe(t, fakeSources(t))
	stop := runServe(t, h)
	defer stop()
	c := newAPIClient(t, h.ln)
	c.setup(h.out)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://"+h.ln.Addr().String()+"/ws", nil)
	require.NoError(t, err)
	defer conn.CloseNow()
	require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`)))
	_, msg, err := conn.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, string(msg), "pong")

	status, body := c.do(http.MethodPut, "/api/v1/config/settings", `{"site.name":"Y"}`)
	require.Equal(t, http.StatusOK, status, body)
	for {
		_, msg, err = conn.Read(ctx)
		require.NoError(t, err)
		if strings.Contains(string(msg), `"type":"site_changed"`) {
			return
		}
	}
}

func TestServeInvalidStoredConfig(t *testing.T) {
	t.Parallel()
	h := newServe(t, fakeSources(t))
	stop := runServe(t, h)
	c := newAPIClient(t, h.ln)
	c.setup(h.out)
	stop()

	path := h.opts.Loaded.Result.Config.Storage.Path
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO config_object (kind, id, body, updated_at, updated_by)
		VALUES ('meter', 'bad', '{"device":"ghost","source":"total"}', 0, 'test')`)
	require.NoError(t, db.Close())
	if err != nil {
		t.Skipf("config_object layout differs: %v", err)
	}

	opts := h.opts
	opts.Listen = func(string) (net.Listener, error) { return h.ln, nil }
	err = app.Serve(context.Background(), opts)
	require.ErrorContains(t, err, "UI configuration")
}

func TestLoadStored(t *testing.T) {
	t.Parallel()
	l := loadServeConfig(t)
	clock := newServe(t, nil).clock

	got, err := app.LoadStored(context.Background(), l, clock)
	require.NoError(t, err)
	assert.Same(t, l, got, "no database file: unchanged")

	// Run a server once to create the database, key and a stored setting.
	h := &harness{opts: app.ServeOptions{}, out: &lockedBuffer{}, clock: clock}
	h.opts = newServe(t, fakeSources(t)).opts
	h.opts.Loaded = l
	h.opts.Stdout = h.out
	stop := runServe(t, h)
	c := newAPIClient(t, h.ln)
	c.setup(h.out)
	status, body := c.do(http.MethodPut, "/api/v1/config/settings", `{"site.name":"Z"}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = c.do(http.MethodPut, "/api/v1/config/device/ev", newDevice)
	require.Equal(t, http.StatusOK, status, body)
	stop()

	got, err = app.LoadStored(context.Background(), l, clock)
	require.NoError(t, err)
	assert.Equal(t, "Z", got.Result.Config.Site.Name)
	assert.EqualValues(t, 2, got.Site.Rev)
	assert.Contains(t, got.Result.Config.Devices, "ev")

	// Without secret.key the read-only job still loads; the secret is unreadable.
	key := filepath.Join(filepath.Dir(l.Result.Config.Storage.Path), "secret.key")
	require.NoError(t, os.Remove(key))
	got, err = app.LoadStored(context.Background(), l, clock)
	require.NoError(t, err, "a missing key is not an error")
	assert.EqualValues(t, 2, got.Site.Rev)
	assert.NotEqual(t, "s3cret-pw", got.Result.Config.Devices["ev"].Auth.Password.Value())
}

// TestConfigEmptyConnection: a connection no device uses has no collector, also once its
// last device is deleted (a collector without devices would panic in its loop).
func TestConfigEmptyConnection(t *testing.T) {
	t.Parallel()
	h := newServe(t, fakeSources(t))
	stop := runServe(t, h)
	c := newAPIClient(t, h.ln)
	c.setup(h.out)

	status, body := c.do(http.MethodPut, "/api/v1/config/connection/spare",
		`{"address":"tcp://10.0.0.77:502"}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Eventually(t, func() bool {
		return strings.Contains(h.out.String(), "configuration applied")
	}, 5*time.Second, 10*time.Millisecond)
	assert.False(t, c.healthHas("10.0.0.77")())

	status, body = c.do(http.MethodPut, "/api/v1/config/device/spare1",
		`{"connection":"spare","unit":1,"profile":"solis/s6-hybrid"}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Eventually(t, c.healthHas("collector:tcp://10.0.0.77"), 5*time.Second,
		10*time.Millisecond)
	status, body = c.do(http.MethodDelete, "/api/v1/config/device/spare1", "")
	require.Equal(t, http.StatusNoContent, status, body)
	require.Eventually(t, func() bool { return !c.healthHas("10.0.0.77")() },
		5*time.Second, 10*time.Millisecond, "the emptied connection keeps no collector")

	stop()
}
