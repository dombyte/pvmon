package app

import (
	"github.com/rs/zerolog"

	"github.com/dombyte/pvmon/internal/collector"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/util"
)

// noTransport is the lifecycle of HTTP connections, whose clients need none.
type noTransport struct{}

func (noTransport) Start() {}
func (noTransport) Stop()  {}

// RealSources builds fetchers on the real transports of a connection.
func RealSources(s *site.Site, c *site.Connection, clock util.Clock, log zerolog.Logger,
) (map[string]collector.Source, collector.Transport, error) {
	srcs := map[string]collector.Source{}
	var tr collector.Transport = noTransport{}
	if IsModbus(c) {
		mb, err := CreateModbusConn(c, clock, log)
		if err != nil {
			return nil, nil, err
		}
		tr = mb
		for _, id := range c.Devices {
			f, err := CreateFetcher(s, s.Devices[id], mb, clock, log)
			if err != nil {
				return nil, nil, err
			}
			srcs[id] = f
		}
		return srcs, tr, nil
	}
	for _, id := range c.Devices {
		f, err := CreateFetcher(s, s.Devices[id], nil, clock, log)
		if err != nil {
			return nil, nil, err
		}
		srcs[id] = f
	}
	return srcs, tr, nil
}
