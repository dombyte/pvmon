//go:build integration

package app_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/app"
	"github.com/dombyte/pvmon/internal/config"
	"github.com/dombyte/pvmon/internal/transport/modbus"
	"github.com/dombyte/pvmon/internal/util"
	"github.com/dombyte/pvmon/profiles"
)

// TestRealDevices reads every device of the config in PVMON_IT_CONFIG once and checks that
// all its meters decode a power value (go test -tags integration ./internal/app).
func TestRealDevices(t *testing.T) {
	path := os.Getenv("PVMON_IT_CONFIG")
	if path == "" {
		t.Skip("PVMON_IT_CONFIG not set")
	}
	l, err := app.LoadConfig(config.Source{File: path, Environ: os.Environ()}, profiles.FS())
	require.NoError(t, err)
	clock := util.NewRealClock()
	for id, dev := range l.Site.Devices {
		t.Run(id, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			var mb *modbus.Conn
			if conn := l.Site.Connections[dev.Connection]; app.IsModbus(conn) {
				mb, err = app.CreateModbusConn(conn, clock, zerolog.Nop())
				require.NoError(t, err)
				mb.Start()
				defer mb.Stop()
			}
			f, err := app.CreateFetcher(l.Site, dev, mb, clock, zerolog.Nop())
			require.NoError(t, err)
			s, err := f.FetchAll(ctx)
			require.NoError(t, err)
			raw, err := s.Raw()
			require.NoError(t, err)
			for _, mid := range dev.Meters {
				m := l.Site.Meters[mid]
				v := dev.Profile.Channels(m.Source, raw)["power"]
				assert.True(t, v.Valid, "meter %s power", mid)
			}
		})
	}
}
