// Package modbus is the Modbus TCP/RTU transport: one shared connection with a FIFO request
// queue per address, raw register reads (FC 03/04 only), error classification and
// reconnect with exponential backoff. It never writes to devices (spec 4.4).
package modbus

import (
	"context"
	"errors"
	"time"
)

// Function selects the register table.
type Function int

// Supported read functions.
const (
	FuncInput   Function = 4 // read input registers
	FuncHolding Function = 3 // read holding registers
)

// Errors returned by Read. Callers branch on them with errors.Is.
var (
	// ErrIllegalAddress is the Modbus exception "illegal data address".
	ErrIllegalAddress = errors.New("modbus: illegal data address")
	// ErrException is any other Modbus exception: the device answered with an error.
	ErrException = errors.New("modbus: exception")
	// ErrUnavailable means the connection failed or is in backoff.
	ErrUnavailable = errors.New("modbus: connection unavailable")
	// ErrStopped is returned for requests after Stop.
	ErrStopped = errors.New("modbus: connection stopped")
)

// Backoff bounds (spec 8.2): a device that comes back is polled again within 30 s.
const (
	BackoffMin = time.Second
	BackoffMax = 30 * time.Second
)

// Settings describes one connection.
type Settings struct {
	Address string        // normalised: tcp://host:port or rtu:///dev/x
	Timeout time.Duration // per request
	Serial  Serial
}

// Serial holds RTU line parameters.
type Serial struct {
	Speed    int
	DataBits int
	Parity   string
	StopBits int
}

// Client is one opened connection to the bus. It is used by a single goroutine only.
type Client interface {
	ReadRegisters(ctx context.Context, unit uint8, fn Function, start, count uint16) ([]byte, error)
	Close() error
}

// Opener opens a Client for the settings; the real one uses grid-x/modbus.
type Opener interface {
	Open(ctx context.Context, s Settings) (Client, error)
}

// Reader is what consumers use: read count registers from start of a unit.
type Reader interface {
	Read(ctx context.Context, unit uint8, fn Function, start, count uint16) ([]uint16, error)
}
