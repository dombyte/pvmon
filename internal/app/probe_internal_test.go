package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/transport/modbus"
	"github.com/dombyte/pvmon/internal/util/clocktest"
	"github.com/dombyte/pvmon/profiles"
)

func probeSite(t *testing.T, addr, prof string) (*site.Site, *site.Device) {
	t.Helper()
	l, err := profile.NewLoader(profiles.FS(), os.ReadFile)
	require.NoError(t, err)
	p, err := l.Load(prof, ".")
	require.NoError(t, err)
	s, err := site.Build(site.Spec{
		Connections: map[string]site.ConnectionSpec{},
		Devices: map[string]site.DeviceSpec{"d": {
			Address: addr, Unit: 1, Profile: p, Interval: time.Second, Timeout: time.Second,
		}},
	})
	require.NoError(t, err)
	d := s.Devices["d"]
	for name := range p.Blocks {
		d.Blocks = append(d.Blocks, name) // as Probe does
	}
	return s, d
}

func TestRealProbeHTTP(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"total_act_power": 512}`))
	}))
	t.Cleanup(srv.Close)
	s, d := probeSite(t, srv.URL, "shelly/pro-3em")
	clock := clocktest.New(time.Now())
	src, release, err := RealProbe(s, d, nil, clock, zerolog.Nop())
	require.NoError(t, err)
	defer release()
	sample, err := src.Fetch(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, sample.JSON)
}

func TestRealProbeModbus(t *testing.T) {
	t.Parallel()
	s, d := probeSite(t, "tcp://127.0.0.1:1", "eastron/sdm630")
	clock := clocktest.New(time.Now())
	src, release, err := RealProbe(s, d, nil, clock, zerolog.Nop())
	require.NoError(t, err, "an address without collector gets its own connection")
	release()
	_, err = src.Fetch(context.Background())
	require.ErrorIs(t, err, modbus.ErrStopped, "released after the probe")

	shared, err := CreateModbusConn(s.Connections[d.Connection], clock, zerolog.Nop())
	require.NoError(t, err)
	src, release, err = RealProbe(s, d, shared, clock, zerolog.Nop())
	require.NoError(t, err)
	release()
	shared.Start()
	shared.Stop()
	_, err = src.Fetch(context.Background())
	require.ErrorIs(t, err, modbus.ErrStopped, "reads through the collector's connection")
}
