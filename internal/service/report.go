package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dombyte/pvmon/internal/derive"
	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/report"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/util"
)

// ErrInvalidReport wraps every bad report parameter (400, code invalid_range).
var ErrInvalidReport = report.ErrInvalidRange

// slotChunkDays keeps one bucket request below derive.MaxBuckets (100 slots on a DST day).
const slotChunkDays = 100

// ReportQuery are the parameters of GET /api/v1/report (spec 9.1, 12).
type ReportQuery struct {
	From, To, Granularity, Compare string
}

// Reports builds energy reports from derived site quantities.
type Reports struct {
	energy EnergyDeriver
	clock  util.Clock
	loc    *time.Location
	plan   reportPlan
}

// reportPlan maps derive names to report keys, decided once from the site.
type reportPlan struct {
	layout report.Layout
	day    map[string]string // derive name → report key (daily values)
	slot   map[string]string // derive name → report key (bucket values)
	meters map[string][]string
	labels map[string]string
}

// NewReports builds the service for site s.
func NewReports(s *site.Site, energy EnergyDeriver, clock util.Clock, loc *time.Location,
) (*Reports, error) {
	if err := util.RequireAll("service", util.D("site", s), util.D("deriver", energy),
		util.D("clock", clock), util.D("location", loc)); err != nil {
		return nil, err
	}
	return &Reports{energy: energy, clock: clock, loc: loc, plan: newPlan(s, energy.Names())},
		nil
}

func newPlan(s *site.Site, names []string) reportPlan {
	p := reportPlan{
		day: map[string]string{
			site.QuantityConsumption: report.KeyConsumption,
			site.QuantityGridIn:      report.KeyImport, site.QuantityGridOut: report.KeyExport,
		},
		slot: map[string]string{
			site.QuantityConsumption: report.KeyConsumption, site.QuantityGridIn: report.KeyImport,
		},
		meters: map[string][]string{}, labels: map[string]string{},
	}
	known := map[string]bool{}
	for _, n := range names {
		known[n] = true
		if meter, _, ok := strings.Cut(n, "/"); ok && meter != "site" {
			p.meters[meter] = append(p.meters[meter], n)
			p.day[n] = n
		}
	}
	for _, id := range sortedIDs(s) {
		p.labels[id] = s.Meters[id].Label
		p.addMeter(s.Meters[id], known)
	}
	return p
}

func sortedIDs(s *site.Site) []string {
	ids := make([]string, 0, len(s.Meters))
	for id := range s.Meters {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// addMeter adds a top-level PV meter, battery or loadpoint with energy counters to the
// layout and the plan.
func (p *reportPlan) addMeter(m *site.Meter, known map[string]bool) {
	if m.Nested() {
		return
	}
	rm := report.Meter{ID: m.ID, Label: m.Label, Category: m.Category, KWp: m.KWp}
	switch m.Role {
	case profile.RolePV:
		p.addPV(rm, known)
	case profile.RoleBattery:
		p.addBattery(rm, known)
	case profile.RoleConsumer:
		p.addLoadpoint(rm, known)
	}
}

func (p *reportPlan) addPV(m report.Meter, known map[string]bool) {
	out := m.ID + "/energy_out"
	if !known[out] {
		return
	}
	p.layout.PV = append(p.layout.PV, m)
	p.day[site.QuantityPV] = report.KeyPV
	p.day[out] = report.MeterKey(report.KeyPV, m.ID)
}

func (p *reportPlan) addBattery(m report.Meter, known map[string]bool) {
	in, out := m.ID+"/energy_in", m.ID+"/energy_out"
	if !known[in] || !known[out] {
		return
	}
	p.layout.Batteries = append(p.layout.Batteries, m)
	p.day[site.QuantityBatteryIn] = report.KeyCharge
	p.day[site.QuantityBatteryOut] = report.KeyDischarge
	p.slot[site.QuantityBatteryIn] = report.KeyCharge
	p.day[in] = report.MeterKey(report.KeyCharge, m.ID)
	p.day[out] = report.MeterKey(report.KeyDischarge, m.ID)
}

func (p *reportPlan) addLoadpoint(m report.Meter, known map[string]bool) {
	in := m.ID + "/energy_in"
	if !known[in] {
		return
	}
	p.layout.Loadpoints = append(p.layout.Loadpoints, m)
	p.day[in] = report.MeterKey(report.KeyLoadpoint, m.ID)
	p.slot[in] = report.MeterKey(report.KeyLoadpoint, m.ID)
}

// Build computes the report of q.
func (r *Reports) Build(ctx context.Context, q ReportQuery) (report.Report, error) {
	in, err := r.input(q)
	if err != nil {
		return report.Report{}, err
	}
	days, quality, err := r.days(ctx, in.Range)
	if err != nil {
		return report.Report{}, err
	}
	in.Days, in.Quality = days, quality
	if in.Compare != nil {
		if in.Compare.Days, _, err = r.days(ctx, in.Compare.Range); err != nil {
			return report.Report{}, err
		}
	}
	return report.Build(in), nil
}

// input parses q into the report input without days.
func (r *Reports) input(q ReportQuery) (report.Input, error) {
	today := r.clock.Now().In(r.loc).Format(time.DateOnly)
	rng, err := report.ParseRange(q.From, q.To, today)
	if err != nil {
		return report.Input{}, err
	}
	todayDay, _ := report.ParseDay(today) //nolint:errcheck // formatted above
	rng, excluded, err := rng.Finished(todayDay)
	if err != nil {
		return report.Input{}, err
	}
	gran, err := report.ParseGranularity(q.Granularity, rng)
	if err != nil {
		return report.Input{}, err
	}
	mode, err := report.ParseCompare(q.Compare)
	if err != nil {
		return report.Input{}, err
	}
	in := report.Input{
		Range: rng, Granularity: gran, Layout: r.plan.layout,
		TodayExcluded: excluded,
	}
	if cmp, ok := mode.Of(rng); ok {
		in.Compare = &report.CompareInput{Mode: mode, Range: cmp}
	}
	return in, nil
}

// local turns a report day (midnight UTC) into the local midnight of that date.
func (r *Reports) local(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, r.loc)
}

// days loads the daily values, the slot estimates and the meters' data quality.
func (r *Reports) days(ctx context.Context, rng report.Range,
) ([]report.Day, []report.MeterQuality, error) {
	names := keysOf(r.plan.day)
	res, err := r.energy.Energy(ctx, names, derive.LevelDay, r.local(rng.From), r.local(rng.To))
	if err != nil {
		return nil, nil, fmt.Errorf("service: report days: %w", err)
	}
	days := make([]report.Day, 0, rng.Days())
	for i := range rng.Days() {
		d := report.Day{Date: rng.From.AddDate(0, 0, i), Values: map[string]float64{}}
		for _, s := range res {
			if v := s.Values[i]; v.Complete && v.Value.Value != nil {
				d.Values[r.plan.day[s.Name]] = *v.Value.Value
			}
		}
		days = append(days, d)
	}
	if err := r.addSlots(ctx, rng, days); err != nil {
		return nil, nil, err
	}
	return days, r.quality(res), nil
}

// quality counts each meter's days with an incomplete or estimated energy series.
func (r *Reports) quality(res []derive.Result) []report.MeterQuality {
	byName := map[string]derive.Result{}
	for _, s := range res {
		byName[s.Name] = s
	}
	out := make([]report.MeterQuality, 0, len(r.plan.meters))
	for _, id := range keysOf(r.plan.meters) {
		missing, estimated := map[int]bool{}, map[int]bool{}
		for _, n := range r.plan.meters[id] {
			markDays(byName[n].Values, missing, estimated)
		}
		out = append(out, report.MeterQuality{
			ID: id, Label: r.plan.labels[id],
			MissingDays: countTrue(missing), EstimatedDays: countTrue(estimated),
		})
	}
	return out
}

// markDays marks the days of one series without a complete value or with estimates.
func markDays(vs []derive.PeriodValue, missing, estimated map[int]bool) {
	for i, v := range vs {
		missing[i] = missing[i] || !v.Complete || v.Value.Value == nil
		estimated[i] = estimated[i] || v.Estimated
	}
}

func countTrue(m map[int]bool) int {
	n := 0
	for _, v := range m {
		if v {
			n++
		}
	}
	return n
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
