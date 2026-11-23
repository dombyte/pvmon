package modbus_test

import (
	"context"
	"errors"
	"testing"
	"time"

	gridx "github.com/grid-x/modbus"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/transport/modbus"
	"github.com/dombyte/pvmon/internal/transport/modbus/mocks"
	"github.com/dombyte/pvmon/internal/util/clocktest"
)

var t0 = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func newConn(t *testing.T, opener modbus.Opener) (*modbus.Conn, *clocktest.Clock) {
	t.Helper()
	clock := clocktest.New(t0)
	c, err := modbus.New(modbus.Settings{Address: "tcp://x:502", Timeout: time.Second},
		clock, opener, zerolog.Nop())
	require.NoError(t, err)
	c.Start()
	c.Start() // idempotent
	t.Cleanup(c.Stop)
	return c, clock
}

func read(c *modbus.Conn) ([]uint16, error) {
	return c.Read(context.Background(), 1, modbus.FuncInput, 100, 2)
}

func TestReadSuccessAndExceptions(t *testing.T) {
	t.Parallel()
	client := mocks.NewMockClient(t)
	opener := mocks.NewMockOpener(t)
	opener.EXPECT().Open(mock.Anything, mock.Anything).Return(client, nil).Once()
	client.EXPECT().Close().Return(nil).Once() // on Stop
	c, _ := newConn(t, opener)

	client.EXPECT().ReadRegisters(mock.Anything, uint8(1), modbus.FuncInput, uint16(100),
		uint16(2)).Return([]byte{0, 1, 0x12, 0x34}, nil).Once()
	words, err := read(c)
	require.NoError(t, err)
	assert.Equal(t, []uint16{1, 0x1234}, words)

	client.EXPECT().ReadRegisters(mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything).Return(nil, &gridx.Error{ExceptionCode: 2}).Once()
	_, err = read(c)
	require.ErrorIs(t, err, modbus.ErrIllegalAddress)

	client.EXPECT().ReadRegisters(mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything).Return(nil, &gridx.Error{ExceptionCode: 4}).Once()
	_, err = read(c)
	require.ErrorIs(t, err, modbus.ErrException)

	client.EXPECT().ReadRegisters(mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything).Return([]byte{0}, nil).Once()
	_, err = read(c)
	require.ErrorIs(t, err, modbus.ErrException, "short answer")
	// exceptions keep the connection: Open was called once only
}

func TestBackoff(t *testing.T) {
	t.Parallel()
	opener := mocks.NewMockOpener(t)
	c, clock := newConn(t, opener)
	opener.EXPECT().Open(mock.Anything, mock.Anything).Return(nil, errors.New("refused")).Times(3)

	_, err := read(c)
	require.ErrorIs(t, err, modbus.ErrUnavailable)
	_, err = read(c)
	require.ErrorContains(t, err, "backoff", "no dial during backoff")

	clock.Advance(modbus.BackoffMin)
	_, err = read(c)
	require.ErrorContains(t, err, "refused")
	clock.Advance(modbus.BackoffMin)
	_, err = read(c)
	require.ErrorContains(t, err, "backoff", "backoff doubled to 2s")
	clock.Advance(modbus.BackoffMin)
	_, err = read(c)
	require.ErrorContains(t, err, "refused")

	client := mocks.NewMockClient(t)
	opener.EXPECT().Open(mock.Anything, mock.Anything).Return(client, nil).Once()
	client.EXPECT().ReadRegisters(mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything).Return(nil, errors.New("i/o timeout")).Once()
	client.EXPECT().Close().Return(errors.New("already closed")).Once()
	clock.Advance(time.Hour)
	_, err = read(c)
	require.ErrorIs(t, err, modbus.ErrUnavailable, "transport error drops the client")
}

func TestBackoffCapped(t *testing.T) {
	t.Parallel()
	opener := mocks.NewMockOpener(t)
	opener.EXPECT().Open(mock.Anything, mock.Anything).Return(nil, errors.New("down"))
	c, clock := newConn(t, opener)
	for range 10 {
		_, err := read(c)
		require.ErrorContains(t, err, "down")
		clock.Advance(modbus.BackoffMax)
	}
}

func TestStopAndContext(t *testing.T) {
	t.Parallel()
	opener := mocks.NewMockOpener(t)
	c, _ := newConn(t, opener)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Read(ctx, 1, modbus.FuncHolding, 0, 1)
	require.ErrorIs(t, err, context.Canceled)

	c.Stop()
	c.Stop()
	_, err = read(c)
	require.ErrorIs(t, err, modbus.ErrStopped)

	_, err = modbus.New(modbus.Settings{}, nil, nil, zerolog.Nop())
	require.Error(t, err)
}

func TestStopWhileWaiting(t *testing.T) {
	t.Parallel()
	opener := mocks.NewMockOpener(t)
	block := make(chan struct{})
	opener.EXPECT().Open(mock.Anything, mock.Anything).RunAndReturn(
		func(context.Context, modbus.Settings) (modbus.Client, error) {
			<-block
			return nil, errors.New("late")
		}).Maybe()
	c, _ := newConn(t, opener)
	errs := make(chan error, 1)
	go func() {
		_, err := read(c)
		errs <- err
	}()
	// the worker is now blocked in Open; a second caller times out while queued
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := c.Read(ctx, 1, modbus.FuncInput, 0, 1)
	require.Error(t, err)
	close(block)
	require.Error(t, <-errs)
}

func TestTimeoutOnOpenConnectionRetriesAtOnce(t *testing.T) {
	t.Parallel()
	first, second := mocks.NewMockClient(t), mocks.NewMockClient(t)
	opener := mocks.NewMockOpener(t)
	opener.EXPECT().Open(mock.Anything, mock.Anything).Return(first, nil).Once()
	c, _ := newConn(t, opener)
	first.EXPECT().ReadRegisters(mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything).Return([]byte{0, 1, 0, 2}, nil).Once()
	_, err := read(c)
	require.NoError(t, err)

	// The open connection times out: dropped, reopened and asked again in the same request.
	first.EXPECT().ReadRegisters(mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything).Return(nil, errors.New("i/o timeout")).Once()
	first.EXPECT().Close().Return(nil).Once()
	opener.EXPECT().Open(mock.Anything, mock.Anything).Return(second, nil).Once()
	second.EXPECT().ReadRegisters(mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything).Return([]byte{0, 3, 0, 4}, nil).Once()
	words, err := read(c)
	require.NoError(t, err)
	assert.Equal(t, []uint16{3, 4}, words)

	// The fresh connection fails too: no third try, and the next request waits out a backoff.
	second.EXPECT().ReadRegisters(mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything).Return(nil, errors.New("i/o timeout")).Once()
	second.EXPECT().Close().Return(nil).Once()
	third := mocks.NewMockClient(t)
	opener.EXPECT().Open(mock.Anything, mock.Anything).Return(third, nil).Once()
	third.EXPECT().ReadRegisters(mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything).Return(nil, errors.New("i/o timeout")).Once()
	third.EXPECT().Close().Return(nil).Once()
	_, err = read(c)
	require.ErrorIs(t, err, modbus.ErrUnavailable)
	_, err = read(c)
	require.ErrorContains(t, err, "backoff")
}
