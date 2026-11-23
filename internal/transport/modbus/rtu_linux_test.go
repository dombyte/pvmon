//go:build linux

package modbus_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/transport/modbus"
	"github.com/dombyte/pvmon/internal/util"
)

// openPTY returns the master side and the slave device path of a new pseudo terminal: a
// virtual serial pair for RTU tests.
func openPTY(t *testing.T) (*os.File, string) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pty support: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	unlock := int32(0)
	ioctl(t, master, syscall.TIOCSPTLCK, unsafe.Pointer(&unlock))
	var n uint32
	ioctl(t, master, syscall.TIOCGPTN, unsafe.Pointer(&n))
	return master, fmt.Sprintf("/dev/pts/%d", n)
}

func ioctl(t *testing.T, f *os.File, req uintptr, arg unsafe.Pointer) {
	t.Helper()
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(arg))
	require.Zero(t, errno)
}

func crc16(b []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, x := range b {
		crc ^= uint16(x)
		for range 8 {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

// serveRTU answers RTU read requests (8-byte frames) on the master side.
func serveRTU(master io.ReadWriter) {
	for {
		req := make([]byte, 8)
		if _, err := io.ReadFull(master, req); err != nil {
			return
		}
		pdu := respond(req[1], binary.BigEndian.Uint16(req[2:]), binary.BigEndian.Uint16(req[4:]))
		out := append([]byte{req[0]}, pdu...)
		out = binary.LittleEndian.AppendUint16(out, crc16(out))
		if _, err := master.Write(out); err != nil {
			return
		}
	}
}

func TestConnOverRTU(t *testing.T) {
	t.Parallel()
	master, slave := openPTY(t)
	go serveRTU(master)
	c, err := modbus.New(modbus.Settings{
		Address: "rtu://" + slave, Timeout: time.Second,
		Serial: modbus.Serial{Speed: 9600, DataBits: 8, Parity: "N", StopBits: 1},
	}, util.NewRealClock(), modbus.GridX{}, nopLogger())
	require.NoError(t, err)
	c.Start()
	defer c.Stop()
	// two unit IDs on one shared bus, served in FIFO order
	for _, unit := range []uint8{1, 2} {
		words, err := c.Read(context.Background(), unit, modbus.FuncInput, 40, 2)
		require.NoError(t, err)
		assert.Equal(t, []uint16{40, 41}, words)
	}
	_, err = c.Read(context.Background(), 1, modbus.FuncHolding, 9999, 1)
	require.ErrorIs(t, err, modbus.ErrIllegalAddress)
}
