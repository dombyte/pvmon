package app

import (
	"fmt"

	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/collector"
	"github.com/dombyte/pvmon/internal/site"
	httpt "github.com/dombyte/pvmon/internal/transport/http"
	"github.com/dombyte/pvmon/internal/transport/modbus"
	"github.com/dombyte/pvmon/internal/util"
)

// CreateModbusConn builds the shared connection of a tcp:// or rtu:// address.
func CreateModbusConn(c *site.Connection, clock util.Clock, log zerolog.Logger,
) (*modbus.Conn, error) {
	s := modbus.Settings{
		Address: c.Address, Timeout: c.Timeout,
		Serial: modbus.Serial{
			Speed: c.Serial.Speed, DataBits: c.Serial.DataBits,
			Parity: c.Serial.Parity, StopBits: c.Serial.StopBits,
		},
	}
	conn, err := modbus.New(s, clock, modbus.GridX{}, log)
	if err != nil {
		return nil, fmt.Errorf("app: modbus %s: %w", c.Address, err)
	}
	return conn, nil
}

// CreateHTTPClient builds the HTTP transport of one device.
func CreateHTTPClient(c *site.Connection, d *site.Device, clock util.Clock, log zerolog.Logger,
) (*httpt.Client, error) {
	auth := ""
	if d.Profile.HTTP != nil {
		auth = d.Profile.HTTP.Auth
	}
	cl, err := httpt.New(httpt.Settings{
		BaseURL: c.Address, Timeout: d.Timeout, Auth: auth, User: d.User,
		Password: d.Password, Insecure: d.Insecure,
	}, clock, log.With().Str("device", d.ID).Logger())
	if err != nil {
		return nil, fmt.Errorf("app: http %s: %w", d.ID, err)
	}
	return cl, nil
}

// IsModbus reports whether a connection uses Modbus.
func IsModbus(c *site.Connection) bool { return c.Scheme == "tcp" || c.Scheme == "rtu" }

// CreateFetcher builds the raw fetcher of a device on a started Modbus connection (nil for
// HTTP devices) or with a new HTTP client.
func CreateFetcher(s *site.Site, d *site.Device, mb *modbus.Conn, clock util.Clock,
	log zerolog.Logger,
) (*collector.Fetcher, error) {
	conn := s.Connections[d.Connection]
	if IsModbus(conn) {
		f, err := collector.NewModbusFetcher(d, clock, mb)
		if err != nil {
			return nil, fmt.Errorf("app: fetcher %s: %w", d.ID, err)
		}
		return f, nil
	}
	cl, err := CreateHTTPClient(conn, d, clock, log)
	if err != nil {
		return nil, err
	}
	f, err := collector.NewHTTPFetcher(d, clock, cl)
	if err != nil {
		return nil, fmt.Errorf("app: fetcher %s: %w", d.ID, err)
	}
	return f, nil
}
