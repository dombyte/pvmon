package collector_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/collector"
	"github.com/dombyte/pvmon/internal/collector/mocks"
	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/transport/modbus"
	"github.com/dombyte/pvmon/internal/util/clocktest"
)

const modbusYAML = `
schema: 1
profile: test/modbus
version: 1
vendor: Test
transport: modbus
modbus: { function: input, byte_order: big, word_order: big }
read: { mode: range, max_registers: 10, ranges: [[100, 109], [200, 206]] }
max_power: 10000
blocks:
  pv:
    role: pv
    channels:
      power: { addr: 100, type: u32, unit: W }
      energy_out: { addr: 102, type: u16, scale: 0.1, unit: kWh, counter: lifetime }
      voltage: { addr: 205, type: u16, unit: V }
`

const httpYAML = `
schema: 1
profile: test/http
version: 1
vendor: Test
transport: http
http:
  endpoints:
    - { id: live, url: "/live" }
    - { id: ids, url: "/ids", interval: 1h }
max_power: 20000
blocks:
  total:
    role: grid
    roles_allowed: [grid, consumer]
    channels:
      power: { endpoint: live, path: "$.p", type: number, unit: W }
      energy_in: { endpoint: ids, path: "$.ids", type: number, unit: Wh, counter: lifetime }
`

var start = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func device(t *testing.T, yaml, read string) *site.Device {
	t.Helper()
	p, err := profile.Parse([]byte(yaml))
	require.NoError(t, err)
	blocks := make([]string, 0, len(p.Blocks))
	for b := range p.Blocks {
		blocks = append(blocks, b)
	}
	sort.Strings(blocks) // every block in use, so the fetch reads all of it
	return &site.Device{
		ID: "dev", Unit: 3, Read: read, Profile: &profile.Loaded{Profile: p}, Blocks: blocks,
	}
}

func TestConstructorsRequireDependencies(t *testing.T) {
	t.Parallel()
	clock := clocktest.New(start)
	mb := device(t, modbusYAML, profile.ReadRange)
	ht := device(t, httpYAML, "")
	_, err := collector.NewModbusFetcher(nil, clock, mocks.NewMockModbusReader(t))
	require.Error(t, err)
	_, err = collector.NewModbusFetcher(mb, nil, mocks.NewMockModbusReader(t))
	require.Error(t, err)
	_, err = collector.NewModbusFetcher(mb, clock, nil)
	require.Error(t, err)
	_, err = collector.NewHTTPFetcher(nil, clock, mocks.NewMockHTTPReader(t))
	require.Error(t, err)
	_, err = collector.NewHTTPFetcher(ht, nil, mocks.NewMockHTTPReader(t))
	require.Error(t, err)
	_, err = collector.NewHTTPFetcher(ht, clock, nil)
	require.Error(t, err)
}

func words(count uint16, base uint16) []uint16 {
	w := make([]uint16, count)
	for i := range w {
		w[i] = base + uint16(i)
	}
	return w
}

func TestModbusRange(t *testing.T) {
	t.Parallel()
	clock := clocktest.New(start)
	r := mocks.NewMockModbusReader(t)
	// A poll reads only the needed registers inside each range.
	r.EXPECT().Read(mock.Anything, uint8(3), modbus.FuncInput, uint16(100), uint16(3)).
		Return(words(3, 1000), nil).Once()
	r.EXPECT().Read(mock.Anything, uint8(3), modbus.FuncInput, uint16(205), uint16(1)).
		Return(words(1, 2005), nil).Once()
	f, err := collector.NewModbusFetcher(device(t, modbusYAML, profile.ReadRange), clock, r)
	require.NoError(t, err)

	s, err := f.Fetch(context.Background())
	require.NoError(t, err)
	assert.Equal(t, start, s.At)
	assert.Len(t, s.Words, 4)
	assert.Equal(t, uint16(1000), s.Words[100])
	assert.Equal(t, uint16(1002), s.Words[102])
	assert.Equal(t, uint16(2005), s.Words[205])
	assert.Nil(t, s.JSON)

	// FetchAll (dumps) reads the whole ranges; Invalidate is harmless.
	r.EXPECT().Read(mock.Anything, uint8(3), modbus.FuncInput, uint16(100), uint16(10)).
		Return(words(10, 1), nil).Once()
	r.EXPECT().Read(mock.Anything, uint8(3), modbus.FuncInput, uint16(200), uint16(7)).
		Return(words(7, 1), nil).Once()
	s, err = f.FetchAll(context.Background())
	require.NoError(t, err)
	assert.Len(t, s.Words, 17)
	f.Invalidate()
}

func TestModbusDirectHolding(t *testing.T) {
	t.Parallel()
	clock := clocktest.New(start)
	r := mocks.NewMockModbusReader(t)
	r.EXPECT().Read(mock.Anything, uint8(3), modbus.FuncHolding, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ uint8, _ modbus.Function, s uint16,
			c uint16,
		) ([]uint16, error) {
			return words(c, s), nil
		})
	holding := strings.Replace(modbusYAML, "function: input", "function: holding", 1)
	dev := device(t, holding, profile.ReadDirect)
	f, err := collector.NewModbusFetcher(dev, clock, r)
	require.NoError(t, err)

	s, err := f.Fetch(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint16(100), s.Words[100])
	assert.Equal(t, uint16(101), s.Words[101], "u32 power merged from two words")
	assert.Equal(t, uint16(102), s.Words[102])
}

func TestModbusReadErrorFailsPoll(t *testing.T) {
	t.Parallel()
	clock := clocktest.New(start)
	boom := errors.New("boom")
	r := mocks.NewMockModbusReader(t)
	r.EXPECT().Read(mock.Anything, uint8(3), modbus.FuncInput, uint16(100), uint16(3)).
		Return(words(3, 1), nil).Once()
	r.EXPECT().Read(mock.Anything, uint8(3), modbus.FuncInput, uint16(205), uint16(1)).
		Return(nil, boom).Once()
	f, err := collector.NewModbusFetcher(device(t, modbusYAML, profile.ReadRange), clock, r)
	require.NoError(t, err)

	s, err := f.Fetch(context.Background())
	require.ErrorIs(t, err, boom)
	assert.Contains(t, err.Error(), "dev read 205+1")
	assert.Nil(t, s.Words)
	assert.Equal(t, start, s.At)
}

func TestHTTPSlowEndpointCaching(t *testing.T) {
	t.Parallel()
	clock := clocktest.New(start)
	r := mocks.NewMockHTTPReader(t)
	r.EXPECT().Get(mock.Anything, "/live").Return([]byte(`{"p":1}`), nil)
	r.EXPECT().Get(mock.Anything, "/ids").Return([]byte(`{"ids":1}`), nil).Twice()
	f, err := collector.NewHTTPFetcher(device(t, httpYAML, ""), clock, r)
	require.NoError(t, err)
	ctx := context.Background()

	s, err := f.Fetch(ctx)
	require.NoError(t, err)
	assert.Equal(t, start, s.At)
	assert.JSONEq(t, `{"p":1}`, string(s.JSON["live"]))
	assert.JSONEq(t, `{"ids":1}`, string(s.JSON["ids"]))

	clock.Advance(time.Minute) // ids not due: cached body, no second GET
	s, err = f.Fetch(ctx)
	require.NoError(t, err)
	assert.JSONEq(t, `{"ids":1}`, string(s.JSON["ids"]))
	assert.Equal(t, start.Add(time.Minute), s.At)

	clock.Advance(time.Hour) // due again: second GET /ids
	_, err = f.Fetch(ctx)
	require.NoError(t, err)

	// Invalidate drops the cache: the third /ids GET would exceed Twice, so expect more.
	r.EXPECT().Get(mock.Anything, "/ids").Return([]byte(`{"ids":2}`), nil).Once()
	f.Invalidate()
	s, err = f.Fetch(ctx)
	require.NoError(t, err)
	assert.JSONEq(t, `{"ids":2}`, string(s.JSON["ids"]))
}

func TestHTTPSkipsUnusedEndpoints(t *testing.T) {
	t.Parallel()
	clock := clocktest.New(start)
	r := mocks.NewMockHTTPReader(t)
	r.EXPECT().Get(mock.Anything, "/live").Return([]byte(`{"p":1}`), nil).Once()
	dev := device(t, httpYAML, "")
	dev.Profile.Blocks["total"].Channels["energy_in"] = profile.Channel{} // no /ids user
	f, err := collector.NewHTTPFetcher(dev, clock, r)
	require.NoError(t, err)
	s, err := f.Fetch(context.Background())
	require.NoError(t, err)
	assert.NotContains(t, s.JSON, "ids")
}

func TestHTTPFetchAllIgnoresIntervals(t *testing.T) {
	t.Parallel()
	clock := clocktest.New(start)
	r := mocks.NewMockHTTPReader(t)
	r.EXPECT().Get(mock.Anything, "/live").Return([]byte(`{}`), nil).Twice()
	r.EXPECT().Get(mock.Anything, "/ids").Return([]byte(`{}`), nil).Twice()
	f, err := collector.NewHTTPFetcher(device(t, httpYAML, ""), clock, r)
	require.NoError(t, err)

	_, err = f.Fetch(context.Background())
	require.NoError(t, err)
	s, err := f.FetchAll(context.Background())
	require.NoError(t, err)
	assert.Len(t, s.JSON, 2)
}

func TestHTTPErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	cases := []struct {
		name string
		body []byte
		err  error
		is   error
	}{
		{"get error", nil, boom, boom},
		{"not json", []byte("<html>"), nil, collector.ErrNotJSON},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := mocks.NewMockHTTPReader(t)
			r.EXPECT().Get(mock.Anything, "/live").Return(tc.body, tc.err).Once()
			f, err := collector.NewHTTPFetcher(device(t, httpYAML, ""),
				clocktest.New(start), r)
			require.NoError(t, err)
			s, err := f.Fetch(context.Background())
			require.ErrorIs(t, err, tc.is)
			assert.Nil(t, s.JSON)
		})
	}
}
