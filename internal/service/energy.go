package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dombyte/pvmon/internal/derive"
	"github.com/dombyte/pvmon/internal/util"
)

// Request errors of the energy endpoint; the handler maps them to error codes.
var (
	ErrInvalidLevel = derive.ErrInvalidLevel
	ErrInvalidRange = derive.ErrInvalidRange
	ErrRangeTooLong = derive.ErrRangeTooLong
)

// UnknownSeriesError lists requested series names that do not exist.
type UnknownSeriesError = derive.UnknownSeriesError

const (
	unitWh      = "Wh"
	unitPercent = "%"
	decimals    = 2
)

// EnergyDeriver is the part of derive.Service the energy service needs.
type EnergyDeriver interface {
	Energy(ctx context.Context, names []string, level derive.Level, from, to time.Time,
	) ([]derive.Result, error)
	Names() []string
}

// EnergyModel is the body of GET /api/v1/energy (spec 9.1).
type EnergyModel struct {
	Series []EnergySeries `json:"series"`
}

// EnergySeries is one series over the requested periods.
type EnergySeries struct {
	Name   string        `json:"name"`
	Unit   string        `json:"unit"`
	Values []EnergyValue `json:"values"`
}

// EnergyValue is one period. Value is Wh for energy series and percent for ratios; it
// is marshalled as "wh" or "pct" and is null when there is no data, never 0. Missing
// lists the incomplete local days of the period (bucket level: the slot itself).
type EnergyValue struct {
	Period    string
	Value     *float64
	Ratio     bool
	Complete  bool
	Estimated bool
	Missing   []string
}

// energyQuality is the shared part of both value shapes.
type energyQuality struct {
	Complete    bool     `json:"complete"`
	Estimated   bool     `json:"estimated"`
	MissingDays int      `json:"missing_days"`
	Missing     []string `json:"missing"`
}

type energyWh struct {
	Period string   `json:"period"`
	Wh     *float64 `json:"wh"`
	energyQuality
}

type energyPct struct {
	Period string   `json:"period"`
	Pct    *float64 `json:"pct"`
	energyQuality
}

// MarshalJSON writes the number as "wh" or, for ratios, as "pct".
func (v EnergyValue) MarshalJSON() ([]byte, error) {
	q := energyQuality{v.Complete, v.Estimated, len(v.Missing), v.Missing}
	if q.Missing == nil {
		q.Missing = []string{} // an empty list, never null
	}
	var out any = energyWh{v.Period, v.Value, q}
	if v.Ratio {
		out = energyPct{v.Period, v.Value, q}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("service: marshal energy value: %w", err)
	}
	return b, nil
}

// Energy answers energy requests.
type Energy struct{ d EnergyDeriver }

// NewEnergy builds the service.
func NewEnergy(d EnergyDeriver) (*Energy, error) {
	if err := util.RequireAll("service", util.D("deriver", d)); err != nil {
		return nil, err
	}
	return &Energy{d: d}, nil
}

// Series returns the periods of level for the local days from..to.
func (s *Energy) Series(ctx context.Context, names []string, level string, from, to time.Time,
) (EnergyModel, error) {
	res, err := s.d.Energy(ctx, names, derive.Level(level), from, to)
	if err != nil {
		return EnergyModel{}, fmt.Errorf("service: energy: %w", err)
	}
	out := EnergyModel{Series: make([]EnergySeries, len(res))}
	for i, r := range res {
		es := EnergySeries{Name: r.Name, Unit: unitWh, Values: make([]EnergyValue, len(r.Values))}
		if r.Ratio {
			es.Unit = unitPercent
		}
		for j, v := range r.Values {
			es.Values[j] = energyValue(v, r.Ratio)
		}
		out.Series[i] = es
	}
	return out, nil
}

func energyValue(v derive.PeriodValue, ratio bool) EnergyValue {
	ev := EnergyValue{
		Period: v.Period, Ratio: ratio, Complete: v.Complete, Estimated: v.Estimated,
		Missing: v.Missing,
	}
	if v.Value.Value != nil {
		r := util.Round(*v.Value.Value, decimals)
		ev.Value = &r
	}
	return ev
}
