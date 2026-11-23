package service

import (
	"context"
	"fmt"
	"time"

	"github.com/dombyte/pvmon/internal/derive"
	"github.com/dombyte/pvmon/internal/util"
)

// GaugeDeriver is the part of derive.Service the gauge service needs.
type GaugeDeriver interface {
	Gauge(ctx context.Context, names []string, from, to time.Time) ([]derive.GaugeResult, error)
}

// GaugeModel is the body of GET /api/v1/gauge (spec 9.1).
type GaugeModel struct {
	Series []GaugeSeries `json:"series"`
}

// GaugeSeries is one series; slots without data are left out.
type GaugeSeries struct {
	Name   string       `json:"name"`
	Unit   string       `json:"unit"`
	Points []GaugePoint `json:"points"`
}

// GaugePoint is one 15-minute slot: its start, avg/min/max and covered seconds.
type GaugePoint struct {
	At    time.Time `json:"at"`
	Avg   float64   `json:"avg"`
	Min   float64   `json:"min"`
	Max   float64   `json:"max"`
	Cover int       `json:"cover"`
}

const slotSeconds = 900

// Gauge answers gauge requests.
type Gauge struct {
	d   GaugeDeriver
	loc *time.Location
}

// NewGauge builds the service; loc is the site's location for the slot times.
func NewGauge(d GaugeDeriver, loc *time.Location) (*Gauge, error) {
	err := util.RequireAll("service", util.D("deriver", d), util.D("location", loc))
	if err != nil {
		return nil, err
	}
	return &Gauge{d: d, loc: loc}, nil
}

// Series returns the 15-minute gauges of names for the local days from..to.
func (s *Gauge) Series(ctx context.Context, names []string, from, to time.Time,
) (GaugeModel, error) {
	res, err := s.d.Gauge(ctx, names, from, to)
	if err != nil {
		return GaugeModel{}, fmt.Errorf("service: gauge: %w", err)
	}
	out := GaugeModel{Series: make([]GaugeSeries, len(res))}
	for i, r := range res {
		gs := GaugeSeries{Name: r.Name, Unit: r.Unit, Points: make([]GaugePoint, len(r.Points))}
		for j, p := range r.Points {
			gs.Points[j] = GaugePoint{
				At:  time.Unix(p.Slot*slotSeconds, 0).In(s.loc),
				Avg: util.Round(p.Avg, decimals), Min: util.Round(p.Min, decimals),
				Max: util.Round(p.Max, decimals), Cover: p.Cover,
			}
		}
		out.Series[i] = gs
	}
	return out, nil
}
