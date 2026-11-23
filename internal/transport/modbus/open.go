package modbus

import (
	"context"
	"fmt"
	"net"
	"strings"

	gridx "github.com/grid-x/modbus"
)

// GridX opens real connections with grid-x/modbus. Dial overrides the TCP dialer (tests).
type GridX struct {
	Dial gridx.DialFunc
}

// handler is the part of the grid-x handlers this package needs.
type handler interface {
	gridx.ClientHandler
	SetSlave(slaveID byte)
}

// Open connects to a tcp:// or rtu:// address.
func (g GridX) Open(ctx context.Context, s Settings) (Client, error) {
	var h handler
	switch {
	case strings.HasPrefix(s.Address, "tcp://"):
		h = g.tcp(s)
	case strings.HasPrefix(s.Address, "rtu://"):
		h = rtu(s)
	default:
		return nil, fmt.Errorf("modbus: unsupported address %q", s.Address)
	}
	if err := h.Connect(ctx); err != nil {
		return nil, fmt.Errorf("modbus: connect %s: %w", s.Address, err)
	}
	return &gridClient{h: h, c: gridx.NewClient(h)}, nil
}

func (g GridX) tcp(s Settings) handler {
	var opts []gridx.TCPClientHandlerOption
	if g.Dial != nil {
		opts = append(opts, gridx.WithDialer(g.Dial))
	}
	h := gridx.NewTCPClientHandler(strings.TrimPrefix(s.Address, "tcp://"), opts...)
	h.Timeout = s.Timeout
	h.IdleTimeout = -1        // keep the connection; reconnects are handled by Conn
	h.LinkRecoveryTimeout = 0 // no hidden retries inside the library
	h.ProtocolRecoveryTimeout = 0
	return h
}

func rtu(s Settings) handler {
	h := gridx.NewRTUClientHandler(strings.TrimPrefix(s.Address, "rtu://"))
	h.BaudRate = s.Serial.Speed
	h.DataBits = s.Serial.DataBits
	h.Parity = s.Serial.Parity
	h.StopBits = s.Serial.StopBits
	h.Timeout = s.Timeout
	h.IdleTimeout = -1
	h.LinkRecoveryTimeout = 0
	return h
}

type gridClient struct {
	h handler
	c gridx.Client
}

func (g *gridClient) ReadRegisters(ctx context.Context, unit uint8, fn Function, start,
	count uint16,
) ([]byte, error) {
	g.h.SetSlave(unit)
	if fn == FuncHolding {
		return g.c.ReadHoldingRegisters(ctx, start, count) //nolint:wrapcheck // classified by Conn
	}
	return g.c.ReadInputRegisters(ctx, start, count) //nolint:wrapcheck // classified by Conn
}

func (g *gridClient) Close() error {
	if err := g.h.Close(); err != nil {
		return fmt.Errorf("modbus: close: %w", err)
	}
	return nil
}

// compile-time check that the dial signature matches net.Dialer.
var _ gridx.DialFunc = (&net.Dialer{}).DialContext
