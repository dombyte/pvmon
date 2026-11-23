package maintenance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/util/clocktest"
)

func TestLocks(t *testing.T) {
	t.Parallel()
	db := filepath.Join(t.TempDir(), "pvmon.db")
	server, err := AcquireShared(db)
	require.NoError(t, err)
	_, err = AcquireExclusive(db)
	require.ErrorIs(t, err, ErrLocked)
	require.NoError(t, server.Release())
	job, err := AcquireExclusive(db)
	require.NoError(t, err)
	_, err = AcquireShared(db)
	require.ErrorIs(t, err, ErrLocked)
	require.NoError(t, job.Release())
	require.Error(t, job.Release(), "double release")
	_, err = AcquireExclusive(filepath.Join(t.TempDir(), "missing", "x.db"))
	require.ErrorContains(t, err, "open lock")
}

type fakeSource struct {
	samples []profile.Sample
	errs    []error
	i       int
}

func (f *fakeSource) FetchAll(context.Context) (profile.Sample, error) {
	i := f.i
	f.i++
	return f.samples[i], f.errs[i]
}

const httpProfile = `
schema: 1
profile: test/http
version: 4
vendor: T
transport: http
http: { endpoints: [ { id: live, url: /live } ] }
blocks:
  total: { role: grid, channels: { power: { endpoint: live, path: "$.p", type: number, unit: W } } }
`

func TestDumpHTTP(t *testing.T) {
	t.Parallel()
	p, err := profile.Parse([]byte(httpProfile))
	require.NoError(t, err)
	t0 := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	clock := clocktest.New(t0)
	live := json.RawMessage(`{"p": 5, "id": "shellypro3em-a8032abc1234", "eth": "AA:BB:CC:DD:EE:FF"}`)
	src := &fakeSource{
		samples: []profile.Sample{
			{At: t0, JSON: map[string]json.RawMessage{"live": live}},
			{At: t0.Add(2 * time.Second)},
		},
		errs: []error{nil, errors.New("timeout")},
	}
	var out bytes.Buffer
	done := make(chan error)
	go func() {
		done <- Dump(context.Background(), job(src, p, clock),
			DumpOptions{Count: 2, Interval: 2 * time.Second}, &out)
	}()
	clock.BlockUntil(1)
	clock.Advance(2 * time.Second)
	require.NoError(t, <-done)

	var fx profile.Fixture
	require.NoError(t, json.Unmarshal(out.Bytes(), &fx))
	assert.Equal(t, "test/http", fx.Profile)
	assert.Equal(t, 4, fx.Version)
	assert.Equal(t, "dump", fx.Source)
	require.Len(t, fx.Samples, 2)
	body := string(fx.Samples[0].JSON["live"])
	assert.NotContains(t, body, "AA:BB")
	assert.Contains(t, body, "00:00:00:00:00:00")
	assert.Equal(t, "timeout", fx.Samples[1].Err)
}

const modbusProfile = `
schema: 1
profile: test/mb
version: 1
vendor: T
transport: modbus
modbus: { function: input }
read: { mode: range, ranges: [[0, 9]] }
blocks:
  pv: { role: pv, channels: { power: { addr: 5, type: u16, unit: W } } }
`

func TestDumpModbusAndCancel(t *testing.T) {
	t.Parallel()
	p, err := profile.Parse([]byte(modbusProfile))
	require.NoError(t, err)
	clock := clocktest.New(time.Unix(0, 0))
	src := &fakeSource{
		samples: []profile.Sample{{Words: map[int]uint16{0: 7, 5: 100}}},
		errs:    []error{nil},
	}
	var out bytes.Buffer
	require.NoError(t, Dump(context.Background(), job(src, p, clock), DumpOptions{Count: 1},
		&out))
	var fx profile.Fixture
	require.NoError(t, json.Unmarshal(out.Bytes(), &fx))
	assert.Equal(t, map[int]uint16{0: 7, 5: 100}, fx.Samples[0].Words, "words unchanged")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	src = &fakeSource{samples: make([]profile.Sample, 2), errs: make([]error, 2)}
	err = Dump(ctx, job(src, p, clock), DumpOptions{Count: 2, Interval: time.Second}, &out)
	require.ErrorIs(t, err, context.Canceled)
	one := &fakeSource{samples: make([]profile.Sample, 1), errs: make([]error, 1)}
	err = Dump(context.Background(), job(one, p, clock), DumpOptions{Count: 1}, failWriter{})
	require.ErrorContains(t, err, "write dump")
}

func job(src SampleSource, p *profile.Profile, clock *clocktest.Clock) DumpJob {
	return DumpJob{Source: src, Profile: p, Clock: clock, Log: zerolog.Nop()}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }
