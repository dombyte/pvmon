package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/livecache"
	"github.com/dombyte/pvmon/internal/util"
)

func newServer(t *testing.T, slot *util.Slot[*Hub]) *httptest.Server {
	t.Helper()
	h, err := NewHandler(slot, zerolog.Nop())
	require.NoError(t, err)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func wsURL(srv *httptest.Server) string { return "ws" + strings.TrimPrefix(srv.URL, "http") }

func dial(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, wsURL(srv), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() }) // the test is over; the peer may be gone already
	return conn
}

func exchange(t *testing.T, conn *websocket.Conn, msg string) frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte(msg)))
	return read(t, conn)
}

func read(t *testing.T, conn *websocket.Conn) frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	_, data, err := conn.Read(ctx)
	require.NoError(t, err)
	var f frame
	require.NoError(t, json.Unmarshal(data, &f))
	return f
}

func TestNewHandlerRejectsNilSlot(t *testing.T) {
	t.Parallel()
	h, err := NewHandler(nil, zerolog.Nop())
	require.Error(t, err)
	assert.Nil(t, h)
}

func TestNoHubIsUnavailable(t *testing.T) {
	t.Parallel()
	srv := newServer(t, &util.Slot[*Hub]{})
	resp, err := http.Get(srv.URL) //nolint:noctx // test against a local server
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestCrossOriginIsRejected(t *testing.T) {
	t.Parallel()
	e := newHubEnv(t)
	e.start(t)
	srv := newServer(t, e.slot)
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, wsURL(srv), &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {"http://evil.example"}},
	})
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	e := newHubEnv(t)
	e.start(t)
	e.cache.Set("pv/power", livecache.Value{Num: 700, Unit: "W", At: e.clock.Now()}, interval)
	srv := newServer(t, e.slot)
	conn := dial(t, srv)

	f := exchange(t, conn, `{"type":"subscribe","keys":["pv/power"]}`)
	assert.Equal(t, "snapshot", f["type"])
	assert.Contains(t, f["values"], "pv/power")

	assert.Equal(t, frame{"type": "pong"}, exchange(t, conn, `{"type":"ping"}`))
	assert.Equal(t, "invalid_message", exchange(t, conn, `{nope`)["code"])
	assert.Equal(t, "invalid_message", exchange(t, conn, `{"type":"bogus"}`)["code"])

	keys, err := json.Marshal(make([]string, MaxKeys+1))
	require.NoError(t, err)
	f = exchange(t, conn, `{"type":"subscribe","keys":`+string(keys)+`}`)
	assert.Equal(t, "too_many_keys", f["code"])

	// a push through the hub reaches the socket once the clock passes the tick
	e.cache.Set("pv/power", livecache.Value{Num: 800, Unit: "W", At: e.clock.Now()}, interval)
	got := make(chan frame, 1)
	go func() { got <- read(t, conn) }()
	var u frame
	require.Eventually(t, func() bool {
		e.clock.Advance(coalesceTick)
		select {
		case u = <-got:
			return true
		default:
			return false
		}
	}, wait, tick)
	assert.Equal(t, "update", u["type"])

	require.NoError(t, conn.Write(context.Background(), websocket.MessageText,
		[]byte(`{"type":"unsubscribe","keys":["pv/power"]}`)))
	assert.Equal(t, frame{"type": "pong"}, exchange(t, conn, `{"type":"ping"}`))
	require.NoError(t, conn.Close(websocket.StatusNormalClosure, ""))
	require.Eventually(t, func() bool { return len(e.hub.clientsSnapshot()) == 0 }, wait, tick)
}

func TestFullHubClosesWithTryAgainLater(t *testing.T) {
	t.Parallel()
	e := newHubEnv(t)
	e.start(t)
	for range MaxClients {
		c, _ := newTestClient()
		require.NoError(t, e.hub.register(c))
	}
	srv := newServer(t, e.slot)
	conn := dial(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	_, _, err := conn.Read(ctx)
	assert.Equal(t, websocket.StatusTryAgainLater, websocket.CloseStatus(err))
}

func TestSlowConsumerIsDisconnected(t *testing.T) {
	t.Parallel()
	e := newHubEnv(t)
	e.start(t)
	srv := newServer(t, e.slot)
	conn := dial(t, srv)
	assert.Equal(t, frame{"type": "pong"}, exchange(t, conn, `{"type":"ping"}`))
	require.Eventually(t, func() bool { return len(e.hub.clientsSnapshot()) == 1 }, wait, tick)
	for _, c := range e.hub.clientsSnapshot() {
		c.kill()
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	_, _, err := conn.Read(ctx)
	require.Error(t, err)
	require.Eventually(t, func() bool { return len(e.hub.clientsSnapshot()) == 0 }, wait, tick)
}

func (h *Hub) clientsSnapshot() []*client {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		out = append(out, c)
	}
	return out
}
