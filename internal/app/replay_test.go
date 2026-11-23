package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/app"
	"github.com/dombyte/pvmon/internal/collector"
	"github.com/dombyte/pvmon/internal/config"
	"github.com/dombyte/pvmon/internal/database"
	"github.com/dombyte/pvmon/internal/derive"
	"github.com/dombyte/pvmon/internal/eventbus"
	"github.com/dombyte/pvmon/internal/health"
	healthmocks "github.com/dombyte/pvmon/internal/health/mocks"
	"github.com/dombyte/pvmon/internal/http/handler"
	"github.com/dombyte/pvmon/internal/livecache"
	"github.com/dombyte/pvmon/internal/report"
	"github.com/dombyte/pvmon/internal/service"
	smocks "github.com/dombyte/pvmon/internal/service/mocks"
	"github.com/dombyte/pvmon/internal/simulate"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/internal/util/clocktest"
	"github.com/dombyte/pvmon/profiles"
)

// Reference setups (spec 1, 15, Gate 2): each built-in simulation setup runs through the
// whole pipeline (config → site → collectors → buckets → derive) for a local day across
// both midnights. The day's site quantities must match the household model, satisfy the
// balance identities of spec 2, and /api/v1/site must match its golden file.

// tolWh is the tolerance against the model: Solis daily registers have 0.1 kWh
// resolution and each poll edge may shift one interval of a load step.
const tolWh = 150.0

var replayLoc = mustLoad("Europe/Berlin")

func mustLoad(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

type replayRig struct {
	site  *site.Site
	store *storage.Storage
	clock *clocktest.Clock
	colls []*collector.Collector
}

func newReplay(t *testing.T, setup string, start time.Time) *replayRig {
	t.Helper()
	dir := t.TempDir()
	yml, err := simulate.Setup(setup)
	require.NoError(t, err)
	db := filepath.Join(dir, "pvmon.db")
	yml = bytes.ReplaceAll(yml, []byte("./data/simulate.db"), []byte(db))
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, yml, 0o600))
	l, err := app.LoadConfig(config.Source{File: path}, profiles.FS())
	require.NoError(t, err)
	ctx := context.Background()
	sqlDB, err := database.Open(ctx, db)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, database.Migrate(ctx, sqlDB, nil, start))
	st, err := storage.New(sqlDB, replayLoc)
	require.NoError(t, err)
	r := &replayRig{site: l.Site, store: st, clock: clocktest.New(start)}
	r.colls = r.collectors(t)
	return r
}

func (r *replayRig) collectors(t *testing.T) []*collector.Collector {
	t.Helper()
	bus := eventbus.New()
	cache, err := livecache.New(bus, r.clock)
	require.NoError(t, err)
	rep := healthmocks.NewMockReporter(t)
	rep.EXPECT().DevicePoll(mock.Anything, mock.Anything).Maybe()
	rep.EXPECT().DeviceState(mock.Anything, mock.Anything).Maybe()
	var out []*collector.Collector
	for addr, conn := range r.site.Connections {
		srcs, tr, err := simulate.Sources(replayLoc)(r.site, conn, r.clock, zerolog.Nop())
		require.NoError(t, err)
		c, err := collector.New(collector.Settings{
			Site: r.site, Connection: addr, Sources: srcs, Location: replayLoc,
		}, collector.Deps{
			Store: r.store.Collector(), Cache: cache, Bus: bus, Reporter: rep,
			Transport: tr, Clock: r.clock, Log: zerolog.Nop(),
		})
		require.NoError(t, err)
		out = append(out, c)
	}
	return out
}

// run polls every collector until end; each collector waits on one timer between polls.
func (r *replayRig) run(t *testing.T, end time.Time) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, c := range r.colls {
		require.NoError(t, c.Start(ctx))
	}
	// Step by the shortest device interval, so every collector polls at most once per step.
	step := time.Hour
	for _, d := range r.site.Devices {
		step = min(step, d.Interval)
	}
	for r.clock.Now().Before(end) {
		r.clock.BlockUntil(len(r.colls))
		r.clock.Advance(step)
	}
	r.clock.BlockUntil(len(r.colls))
	for _, c := range r.colls {
		require.NoError(t, c.Stop())
	}
}

// day returns the derived day value of every series of names, in Wh or percent, and
// whether it is estimated (≈).
func (r *replayRig) day(t *testing.T, day time.Time, names []string,
) (vals map[string]float64, est map[string]bool) {
	t.Helper()
	dv, err := derive.New(r.site, r.store.Read(), r.store.Derive(), r.clock, replayLoc)
	require.NoError(t, err)
	res, err := dv.Energy(context.Background(), names, derive.LevelDay, day, day)
	require.NoError(t, err)
	vals, est = map[string]float64{}, map[string]bool{}
	for _, s := range res {
		require.Len(t, s.Values, 1, s.Name)
		v := s.Values[0]
		require.NotNil(t, v.Value.Value, "%s has data", s.Name)
		assert.True(t, v.Complete, "%s complete", s.Name)
		vals[s.Name], est[s.Name] = *v.Value.Value, v.Estimated
	}
	return vals, est
}

// siteJSON renders GET /api/v1/site through the real service and handler.
func (r *replayRig) siteJSON(t *testing.T) []byte {
	t.Helper()
	snap := smocks.NewMockSnapshotSource(t)
	snap.EXPECT().Snapshot().Return(health.Snapshot{}).Maybe()
	svc, err := service.NewSite(r.site, replayLoc, r.store.Read(), snap)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	handler.NewSite(svc, zerolog.Nop()).ServeHTTP(rec,
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/site", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var pretty bytes.Buffer
	require.NoError(t, json.Indent(&pretty, rec.Body.Bytes(), "", "  "))
	return append(pretty.Bytes(), '\n')
}

// modelDay integrates the household model over a local day in Wh per site quantity.
func modelDay(day time.Time) map[string]float64 {
	out := map[string]float64{}
	const step = time.Second
	for t := day; t.Before(day.AddDate(0, 0, 1)); t = t.Add(step) {
		// The heat pump is part of the household in every setup; without its own meter
		// (R1, R3) it ends up in the house rest.
		p := simulate.PowersAt(t.In(replayLoc))
		h := step.Hours()
		out["site/pv"] += p.PV * h
		out["site/grid_in"] += max(p.Grid, 0) * h
		out["site/grid_out"] += max(-p.Grid, 0) * h
		out["site/battery_out"] += max(p.Battery, 0) * h
		out["site/battery_in"] += max(-p.Battery, 0) * h
		out["site/consumption"] += (p.House + p.Wallbox + p.HeatPump) * h
		out["site/loadpoints/wallbox"] += p.Wallbox * h
		out["site/loadpoints/heatpump"] += p.HeatPump * h
		out["backup/energy_in"] += p.Backup * h
	}
	return out
}

func TestReferenceSetups(t *testing.T) {
	t.Parallel()
	for _, setup := range []string{"R1", "R2", "R3", "R4", "R5", "R6", "R7"} {
		t.Run(setup, func(t *testing.T) {
			t.Parallel()
			day := time.Date(2026, 11, 19, 0, 0, 0, 0, replayLoc)
			r := newReplay(t, setup, day.Add(-10*time.Minute))
			r.run(t, day.AddDate(0, 0, 1).Add(15*time.Minute))
			checkSiteGolden(t, setup, r.siteJSON(t))
			checkDay(t, r, day)
			checkReport(t, r, day)
		})
	}
}

func checkSiteGolden(t *testing.T, setup string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "site-"+strings.ToLower(setup)+".json")
	if os.Getenv("PVMON_UPDATE_GOLDEN") != "" {
		require.NoError(t, os.WriteFile(path, got, 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(got))
}

func checkDay(t *testing.T, r *replayRig, day time.Time) {
	t.Helper()
	model := modelDay(day)
	names := siteEnergyNames(r.site)
	extra := []string{"site/house_rest", "site/self_sufficiency", "site/self_consumption"}
	if _, ok := r.site.Meters["backup"]; ok {
		extra = append(extra, "backup/energy_in")
	}
	got, est := r.day(t, day, append(names, extra...))
	assert.Equal(t, integratesPV(r.site), est["site/pv"], "site/pv is ≈ iff a PV meter integrates")
	if _, ok := got["backup/energy_in"]; ok {
		assert.InDelta(t, model["backup/energy_in"], got["backup/energy_in"], tolWh, "backup")
	}
	for _, n := range names {
		assert.InDelta(t, model[n], got[n], tolWh, n)
	}
	// Balance identities of spec 2 hold exactly on the stored energies.
	cons := got["site/pv"] + got["site/grid_in"] - got["site/grid_out"] +
		got["site/battery_out"] - got["site/battery_in"]
	assert.InDelta(t, cons, got["site/consumption"], 0.01, "consumption")
	rest := got["site/consumption"] // the backup output is part of the house rest
	for _, n := range names {
		if strings.HasPrefix(n, "site/loadpoints/") {
			rest -= got[n]
		}
	}
	assert.InDelta(t, rest, got["site/house_rest"], 0.01, "house rest")
	assert.InDelta(t, 100*(cons-got["site/grid_in"])/cons, got["site/self_sufficiency"], 0.01)
	pv := got["site/pv"]
	assert.InDelta(t, 100*(pv-got["site/grid_out"])/pv, got["site/self_consumption"], 0.01)
	assert.Greater(t, pv, 20000.0, "the model produced a real day")
}

// checkReport builds the day report through the real services: one complete day from
// complete buckets, consistent with the derived site quantities (spec 12).
func checkReport(t *testing.T, r *replayRig, day time.Time) {
	t.Helper()
	dv, err := derive.New(r.site, r.store.Read(), r.store.Derive(), r.clock, replayLoc)
	require.NoError(t, err)
	svc, err := service.NewReports(r.site, dv, r.clock, replayLoc)
	require.NoError(t, err)
	d := day.Format(time.DateOnly)
	rep, err := svc.Build(context.Background(),
		service.ReportQuery{From: d, To: d, Compare: "none"})
	require.NoError(t, err)
	s := rep.Summary
	require.Equal(t, 1, s.CompleteDays)
	assert.Zero(t, s.FallbackDays, "every bucket of the day is complete")
	model := modelDay(day)
	assert.InDelta(t, model["site/consumption"], s.Total.Consumption, tolWh)
	assert.InDelta(t, s.Total.Consumption, s.Sources.Solar+s.Sources.Battery+s.Sources.Grid,
		0.01, "the sources add up to the consumption")
	assert.LessOrEqual(t, s.GridCharge, s.Total.Charge+0.01)
	assert.Equal(t, report.SlotsPerDay, countNotNil(rep.Profile.Mean[:]))
	if lp := rep.Loadpoints; lp != nil {
		for _, m := range lp.Meters {
			assert.LessOrEqual(t, m.Solar, m.In+0.01, m.ID)
		}
	}
	assert.Equal(t, len(rep.Layout.PV) >= 2, rep.PVSources != nil, "pv sources")
	assert.Equal(t, len(rep.Layout.Batteries) >= 2, rep.Batteries != nil, "batteries")
}

func countNotNil(vs []*float64) int {
	n := 0
	for _, v := range vs {
		if v != nil {
			n++
		}
	}
	return n
}

// integratesPV reports whether a top-level PV meter's energy is integrated from power.
func integratesPV(s *site.Site) bool {
	for _, si := range s.Series() {
		m := s.Meters[si.Meter]
		if si.Origin == site.OriginIntegrated && m.Role == "pv" && !m.Nested() {
			return true
		}
	}
	return false
}

// siteEnergyNames are the site energy quantities the setup has.
func siteEnergyNames(s *site.Site) []string {
	names := []string{
		"site/pv", "site/grid_in", "site/grid_out", "site/battery_in",
		"site/battery_out", "site/consumption",
	}
	cats := map[string]bool{}
	for _, m := range s.Meters {
		if m.Category != "" && m.Parent == "site" {
			cats[m.Category] = true
		}
	}
	for c := range cats {
		names = append(names, "site/loadpoints/"+c)
	}
	return names
}
