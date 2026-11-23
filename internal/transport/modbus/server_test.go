package modbus_test

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/transport/modbus"
	"github.com/dombyte/pvmon/internal/util"
)

// registers answers reads: register value = address, except 9999 (illegal address).
func respond(fc byte, start, count uint16) []byte {
	if start == 9999 {
		return []byte{fc | 0x80, 2}
	}
	pdu := []byte{fc, byte(2 * count)}
	for i := range count {
		pdu = binary.BigEndian.AppendUint16(pdu, start+i)
	}
	return pdu
}

// serveTCP answers Modbus TCP frames on conn until it is closed.
func serveTCP(conn net.Conn) {
	defer conn.Close()
	for {
		head := make([]byte, 12)
		if _, err := io.ReadFull(conn, head); err != nil {
			return
		}
		pdu := respond(head[7], binary.BigEndian.Uint16(head[8:]),
			binary.BigEndian.Uint16(head[10:]))
		out := append([]byte{}, head[:4]...)
		out = binary.BigEndian.AppendUint16(out, uint16(len(pdu)+1))
		out = append(out, head[6])
		if _, err := conn.Write(append(out, pdu...)); err != nil {
			return
		}
	}
}

func pipeDialer(t *testing.T) func(context.Context, string, string) (net.Conn, error) {
	t.Helper()
	return func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		go serveTCP(server)
		return client, nil
	}
}

func TestGridXTCP(t *testing.T) {
	t.Parallel()
	g := modbus.GridX{Dial: pipeDialer(t)}
	cl, err := g.Open(context.Background(), modbus.Settings{
		Address: "tcp://dev:502",
		Timeout: time.Second,
	})
	require.NoError(t, err)
	defer cl.Close()

	b, err := cl.ReadRegisters(context.Background(), 1, modbus.FuncInput, 33000, 3)
	require.NoError(t, err)
	assert.Equal(t, []byte{0x80, 0xE8, 0x80, 0xE9, 0x80, 0xEA}, b)
	b, err = cl.ReadRegisters(context.Background(), 2, modbus.FuncHolding, 5, 1)
	require.NoError(t, err)
	assert.Equal(t, []byte{0, 5}, b)
	_, err = cl.ReadRegisters(context.Background(), 1, modbus.FuncInput, 9999, 1)
	require.Error(t, err)
}

func TestGridXErrors(t *testing.T) {
	t.Parallel()
	_, err := modbus.GridX{}.Open(context.Background(), modbus.Settings{Address: "udp://x"})
	require.ErrorContains(t, err, "unsupported")
	failing := modbus.GridX{Dial: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("no route")
	}}
	_, err = failing.Open(context.Background(), modbus.Settings{
		Address: "tcp://x:502",
		Timeout: time.Second,
	})
	require.ErrorContains(t, err, "connect")
	_, err = modbus.GridX{}.Open(context.Background(), modbus.Settings{
		Address: "rtu:///dev/does-not-exist", Timeout: time.Second,
		Serial: modbus.Serial{Speed: 9600, DataBits: 8, Parity: "N", StopBits: 1},
	})
	require.ErrorContains(t, err, "connect")
}

// TestConnOverTCP runs the full path: Conn → GridX → TCP framing → test server.
func TestConnOverTCP(t *testing.T) {
	t.Parallel()
	c, err := modbus.New(modbus.Settings{Address: "tcp://dev:502", Timeout: time.Second},
		util.NewRealClock(), modbus.GridX{Dial: pipeDialer(t)}, nopLogger())
	require.NoError(t, err)
	c.Start()
	defer c.Stop()
	words, err := c.Read(context.Background(), 1, modbus.FuncInput, 33130, 2)
	require.NoError(t, err)
	assert.Equal(t, []uint16{33130, 33131}, words)
	_, err = c.Read(context.Background(), 1, modbus.FuncInput, 9999, 1)
	require.ErrorIs(t, err, modbus.ErrIllegalAddress)
}
