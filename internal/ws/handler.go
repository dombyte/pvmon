package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/util"
)

// writeTimeout bounds every frame write, so a stuck peer never blocks its writer forever.
const writeTimeout = 10 * time.Second

// Handler upgrades GET /ws and runs one connection on the current hub. The upgrader keeps
// the library's same-origin check (Origin host == Host).
type Handler struct {
	slot *util.Slot[*Hub]
	log  zerolog.Logger
}

// NewHandler builds the handler for the hub published in slot.
func NewHandler(slot *util.Slot[*Hub], log zerolog.Logger) (*Handler, error) {
	if err := util.RequireAll("ws handler", util.D("slot", slot)); err != nil {
		return nil, err
	}
	return &Handler{slot: slot, log: log}, nil
}

// ServeHTTP upgrades the connection and serves it until either side closes.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hub, ok := h.slot.Load()
	if !ok {
		http.Error(w, "live data not available", http.StatusServiceUnavailable)
		return
	}
	// http.Server read/write timeouts stay on the hijacked connection; clear them, the
	// connection is long-lived and bounded by writeTimeout and the hub instead.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})  //nolint:errcheck // unsupported writer: no deadline set
	_ = rc.SetWriteDeadline(time.Time{}) //nolint:errcheck // unsupported writer: no deadline set
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		h.log.Debug().Err(err).Msg("websocket upgrade")
		return
	}
	conn.SetReadLimit(MaxMessage)
	// The connection outlives the request handler's deadlines; it ends with the hub.
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()
	c := newClient(cancel)
	if err := hub.register(c); err != nil {
		// The client is turned away; a failed close changes nothing.
		_ = conn.Close(websocket.StatusTryAgainLater, "too many clients")
		return
	}
	defer hub.unregister(c)
	go h.write(ctx, conn, c)
	h.read(ctx, conn, c, hub)
	_ = conn.Close(websocket.StatusNormalClosure, "") // the peer may already be gone
}

// read handles client messages until the connection or its context ends.
func (h *Handler) read(ctx context.Context, conn *websocket.Conn, c *client, hub *Hub) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var m inMsg
		if err := json.Unmarshal(data, &m); err != nil {
			c.send(errorMsg{Type: typeError, Code: codeBadMessage})
			continue
		}
		handle(m, c, hub)
	}
}

func handle(m inMsg, c *client, hub *Hub) {
	switch {
	case len(m.Keys) > MaxKeys:
		c.send(errorMsg{Type: typeError, Code: codeTooManyKeys})
	case m.Type == typeSubscribe:
		c.subscribe(m.Keys, hub.isKnown, hub.cache)
	case m.Type == typeUnsubscribe:
		c.unsubscribe(m.Keys)
	case m.Type == typePing:
		c.send(simpleMsg{Type: typePong})
	default:
		c.send(errorMsg{Type: typeError, Code: codeBadMessage})
	}
}

// write sends queued frames; a failed write ends the connection.
func (h *Handler) write(ctx context.Context, conn *websocket.Conn, c *client) {
	defer c.kill()
	for {
		select {
		case <-ctx.Done():
			return
		case b := <-c.out:
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Write(wctx, websocket.MessageText, b)
			cancel()
			if err != nil {
				h.log.Debug().Err(err).Msg("websocket write")
				return
			}
		}
	}
}
