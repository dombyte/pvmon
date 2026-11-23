package derive

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/database"
	"github.com/dombyte/pvmon/internal/derive/mocks"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/internal/util"
	"github.com/dombyte/pvmon/internal/util/clocktest"
)

type env struct {
	st    *storage.Storage
	db    *sql.DB
	loc   *time.Location
	clock *clocktest.Clock
	svc   *Service
	pv    int64
	site  *site.Site
}

// newEnv opens a real SQLite storage; the clock stands at now (Berlin).
func newEnv(t *testing.T, now time.Time) *env {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pvmon.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, database.Migrate(ctx, db, nil, now))
	loc := berlin(t)
	st, err := storage.New(db, loc)
	require.NoError(t, err)
	ids, err := st.Collector().EnsureSeries(ctx, 0, []storage.SeriesDef{
		{SeriesKey: storage.SeriesKey{Meter: "pv", Channel: "energy_out"}, Kind: site.KindEnergy},
	})
	require.NoError(t, err)
	e := &env{
		st: st, db: db, loc: loc, clock: clocktest.New(now), site: buildSite(t, false, false),
		pv: ids[storage.SeriesKey{Meter: "pv", Channel: "energy_out"}],
	}
	e.svc, err = New(e.site, st.Read(), st.Derive(), e.clock, loc)
	require.NoError(t, err)
	return e
}

// put books wh into every slot of [from, to) of series id.
func (e *env) put(t *testing.T, from, to int64, wh float64, flags map[int64]int) {
	t.Helper()
	var bs []storage.EnergyBucket
	for s := from; s < to; s++ {
		bs = append(bs, storage.EnergyBucket{SeriesID: e.pv, Slot: s, Wh: wh, Flags: flags[s]})
	}
	require.NoError(t, e.st.Collector().Flush(context.Background(), storage.Batch{
		Energy: bs, TodayStart: Slot(util.Midnight(e.clock.Now(), e.loc)),
	}))
}

func (e *env) midnight(y int, m time.Month, d int) int64 {
	return Slot(time.Date(y, m, d, 0, 0, 0, 0, e.loc))
}

// fill: 16th complete (96 Wh), 17th with one gap bucket (96 Wh), 18th empty, 19th until noon
// (48 Wh); the clock stands at 2026-11-19 12:00.
func fill(t *testing.T) *env {
	t.Helper()
	loc := berlin(t)
	e := newEnv(t, time.Date(2026, 11, 19, 12, 0, 0, 0, loc))
	d16, d17, d19 := e.midnight(2026, 11, 16), e.midnight(2026, 11, 17), e.midnight(2026, 11, 19)
	e.put(t, d16, d16+96, 1, nil)
	e.put(t, d17, d17+96, 1, map[int64]int{d17 + 10: storage.FlagGap})
	e.put(t, d19, d19+48, 1, nil)
	return e
}

func (e *env) day(t *testing.T, name string, level Level, from, to int) []PeriodValue {
	t.Helper()
	f := time.Date(2026, 11, from, 0, 0, 0, 0, e.loc)
	res, err := e.svc.Energy(context.Background(), []string{name}, level, f,
		time.Date(2026, 11, to, 0, 0, 0, 0, e.loc))
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, name, res[0].Name)
	return res[0].Values
}

func TestEnergyDays(t *testing.T) {
	t.Parallel()
	e := fill(t)
	vs := e.day(t, "pv/energy_out", LevelDay, 16, 19)
	require.Len(t, vs, 4)
	want := []struct {
		period   string
		wh       *float64
		complete bool
		est      bool
		missing  int
	}{
		{"2026-11-16", fp(96), true, false, 0},
		{"2026-11-17", fp(96), false, true, 1},
		{"2026-11-18", nil, false, false, 1},
		{"2026-11-19", fp(48), true, false, 0},
	}
	for i, w := range want {
		assert.Equal(t, w.period, vs[i].Period)
		if w.wh == nil {
			assert.Nil(t, vs[i].Value.Value, w.period)
		} else {
			require.NotNil(t, vs[i].Value.Value, w.period)
			assert.InDelta(t, *w.wh, *vs[i].Value.Value, 1e-9, w.period)
		}
		assert.Equal(t, w.complete, vs[i].Complete, w.period)
		assert.Equal(t, w.est, vs[i].Estimated, w.period)
		assert.Len(t, vs[i].Missing, w.missing, w.period)
	}
}

func TestEnergyMonthAndYear(t *testing.T) {
	t.Parallel()
	e := fill(t)
	vs := e.day(t, "pv/energy_out", LevelMonth, 5, 5)
	require.Len(t, vs, 1)
	assert.Equal(t, "2026-11", vs[0].Period)
	assert.InDelta(t, 240, *vs[0].Value.Value, 1e-9)
	assert.False(t, vs[0].Complete)
	assert.Equal(t, []string{"2026-11-17", "2026-11-18"}, vs[0].Missing,
		"days before the first slot do not count")
	assert.True(t, vs[0].Estimated)

	vs = e.day(t, "pv/energy_out", LevelYear, 5, 5)
	require.Len(t, vs, 1)
	assert.Equal(t, "2026", vs[0].Period)
	assert.InDelta(t, 240, *vs[0].Value.Value, 1e-9)
	assert.Len(t, vs[0].Missing, 2)
}

func TestEnergyTotalWithCarry(t *testing.T) {
	t.Parallel()
	e := fill(t)
	vs := e.day(t, "pv/energy_out", LevelTotal, 19, 19)
	require.Len(t, vs, 1)
	assert.Equal(t, "total", vs[0].Period)
	assert.InDelta(t, 240, *vs[0].Value.Value, 1e-9)

	_, err := e.db.ExecContext(context.Background(),
		`INSERT INTO carry(key, wh, before_slot) VALUES(?, 1000, 0)`, strconv.FormatInt(e.pv, 10))
	require.NoError(t, err)
	vs = e.day(t, "pv/energy_out", LevelTotal, 19, 19)
	assert.InDelta(t, 1240, *vs[0].Value.Value, 1e-9)
	assert.Len(t, vs[0].Missing, 2)
}

func TestEnergyBucketLevel(t *testing.T) {
	t.Parallel()
	e := fill(t)
	vs := e.day(t, "pv/energy_out", LevelBucket, 17, 17)
	require.Len(t, vs, 96)
	assert.InDelta(t, 1, *vs[0].Value.Value, 1e-9)
	assert.True(t, vs[0].Complete)
	gap := vs[10]
	assert.True(t, gap.Estimated)
	assert.False(t, gap.Complete)
	assert.Len(t, gap.Missing, 1)

	vs = e.day(t, "pv/energy_out", LevelBucket, 19, 19)
	require.Len(t, vs, 96)
	assert.NotNil(t, vs[47].Value.Value)
	assert.Nil(t, vs[48].Value.Value, "no bucket yet")

	vs = e.day(t, "pv/energy_out", LevelBucket, 18, 18)
	assert.Nil(t, vs[0].Value.Value)
}

func TestEnergyNoDataAndNeverStored(t *testing.T) {
	t.Parallel()
	e := newEnv(t, time.Date(2026, 11, 19, 12, 0, 0, 0, berlin(t)))
	for _, level := range []Level{LevelDay, LevelMonth, LevelYear, LevelTotal, LevelBucket} {
		vs := e.day(t, "pv/energy_out", level, 18, 19)
		require.NotEmpty(t, vs)
		for _, v := range vs {
			assert.Nil(t, v.Value.Value, string(level))
			assert.Empty(t, v.Missing, string(level)+" without any data nothing is missing")
		}
	}
	// grid series were never stored: a formula over them has no data, never 0
	vs := e.day(t, site.QuantityConsumption, LevelDay, 18, 18)
	assert.Nil(t, vs[0].Value.Value)

	res, err := e.svc.Energy(context.Background(), []string{QuantitySelfSufficiency},
		LevelDay, time.Date(2026, 11, 18, 0, 0, 0, 0, e.loc), time.Date(2026, 11, 18, 0, 0, 0, 0, e.loc))
	require.NoError(t, err)
	assert.True(t, res[0].Ratio)
}

func TestEnergyCaching(t *testing.T) {
	t.Parallel()
	e := fill(t)
	ctx := context.Background()
	first := e.day(t, "pv/energy_out", LevelDay, 16, 19)

	keys := []string{
		"pv/energy_out|day|2026-11-16", "pv/energy_out|day|2026-11-17",
		"pv/energy_out|day|2026-11-18", "pv/energy_out|day|2026-11-19",
	}
	got, err := e.st.Derive().CacheGet(ctx, keys, 0)
	require.NoError(t, err)
	assert.Contains(t, got, keys[0])
	assert.Contains(t, got, keys[1])
	assert.Contains(t, got, keys[2])
	assert.NotContains(t, got, keys[3], "today is never cached")

	// a second service answers the closed days from the cache: the reader mock has no
	// SumEnergyMany expectation, so a call would fail the test
	rd := mocks.NewMockReader(t)
	rd.EXPECT().SeriesID(mock.Anything, storage.SeriesKey{Meter: "pv", Channel: "energy_out"}).
		Return(e.pv, nil)
	rd.EXPECT().FirstSlot(mock.Anything).Return(e.midnight(2026, 11, 16), true, nil).Maybe()
	svc, err := New(e.site, rd, e.st.Derive(), e.clock, e.loc)
	require.NoError(t, err)
	res, err := svc.Energy(ctx, []string{"pv/energy_out"}, LevelDay,
		time.Date(2026, 11, 16, 0, 0, 0, 0, e.loc), time.Date(2026, 11, 18, 0, 0, 0, 0, e.loc))
	require.NoError(t, err)
	assert.Equal(t, first[:3], res[0].Values)
}

// Regression: browsing back before the first stored slot cached one "no data" row per
// series and day, which made derived_cache the largest table by far.
func TestEnergyBeforeFirstSlotNotCached(t *testing.T) {
	t.Parallel()
	e := fill(t)
	ctx := context.Background()
	for _, level := range []Level{LevelDay, LevelMonth, LevelYear} {
		res, err := e.svc.Energy(ctx, []string{"pv/energy_out"}, level,
			time.Date(2024, 1, 1, 0, 0, 0, 0, e.loc), time.Date(2026, 11, 15, 0, 0, 0, 0, e.loc))
		require.NoError(t, err)
		for _, v := range res[0].Values {
			if v.Period == "2026" || v.Period == "2026-11" {
				continue // the month and year of the first slot have data
			}
			assert.Nil(t, v.Value.Value, v.Period)
			assert.Empty(t, v.Missing, v.Period)
		}
	}
	var n int
	require.NoError(t, e.db.QueryRowContext(ctx, `SELECT count(*) FROM derived_cache
		WHERE key NOT LIKE '%|2026-11%' AND key NOT LIKE '%|2026'`).Scan(&n))
	assert.Zero(t, n, "periods before the first slot are not cached")
}

func TestEnergyClosedYearCached(t *testing.T) {
	t.Parallel()
	e := fill(t)
	e.clock.Set(time.Date(2027, 1, 5, 12, 0, 0, 0, e.loc))
	res, err := e.svc.Energy(context.Background(), []string{"pv/energy_out"}, LevelYear,
		time.Date(2026, 6, 1, 0, 0, 0, 0, e.loc), time.Date(2026, 6, 1, 0, 0, 0, 0, e.loc))
	require.NoError(t, err)
	assert.InDelta(t, 240, *res[0].Values[0].Value.Value, 1e-9)
	assert.NotEmpty(t, res[0].Values[0].Missing)
	got, err := e.st.Derive().CacheGet(context.Background(),
		[]string{"pv/energy_out|year|2026", "pv/energy_out|month|2026-11"}, 0)
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestEnergySiteQuantities(t *testing.T) {
	t.Parallel()
	e := fill(t)
	ctx := context.Background()
	ids, err := e.st.Collector().EnsureSeries(ctx, 0, []storage.SeriesDef{
		{SeriesKey: storage.SeriesKey{Meter: "grid", Channel: "energy_in"}, Kind: site.KindEnergy},
		{SeriesKey: storage.SeriesKey{Meter: "grid", Channel: "energy_out"}, Kind: site.KindEnergy},
	})
	require.NoError(t, err)
	gin := ids[storage.SeriesKey{Meter: "grid", Channel: "energy_in"}]
	gout := ids[storage.SeriesKey{Meter: "grid", Channel: "energy_out"}]
	d16 := e.midnight(2026, 11, 16)
	var bs []storage.EnergyBucket
	for s := d16; s < d16+96; s++ {
		bs = append(bs, storage.EnergyBucket{SeriesID: gin, Slot: s, Wh: 1},
			storage.EnergyBucket{SeriesID: gout, Slot: s, Wh: 0.5})
	}
	require.NoError(t, e.st.Collector().Flush(ctx, storage.Batch{
		Energy:     bs,
		TodayStart: Slot(util.Midnight(e.clock.Now(), e.loc)),
	}))

	cons := e.day(t, site.QuantityConsumption, LevelDay, 16, 16)[0]
	assert.InDelta(t, 96+96-48, *cons.Value.Value, 1e-9)
	assert.True(t, cons.Complete)
	ss := e.day(t, QuantitySelfSufficiency, LevelDay, 16, 16)[0]
	assert.InDelta(t, 100*48.0/144, *ss.Value.Value, 1e-9)
}

func TestUnknownSeries(t *testing.T) {
	t.Parallel()
	e := fill(t)
	_, err := e.svc.Energy(context.Background(), []string{"pv/energy_out", "x", "y/z"},
		LevelDay, time.Now(), time.Now())
	var u *UnknownSeriesError
	require.ErrorAs(t, err, &u)
	assert.Equal(t, []string{"x", "y/z"}, u.Names)
	assert.Equal(t, "derive: unknown series x, y/z", err.Error())

	_, err = e.svc.Energy(context.Background(), []string{"pv/energy_out"}, LevelDay,
		time.Date(2026, 11, 2, 0, 0, 0, 0, e.loc), time.Date(2026, 11, 1, 0, 0, 0, 0, e.loc))
	assert.ErrorIs(t, err, ErrInvalidRange)
	_, err = e.svc.Energy(context.Background(), []string{"pv/energy_out"}, "week",
		time.Now(), time.Now())
	assert.ErrorIs(t, err, ErrInvalidLevel)
}

func TestNewAndNames(t *testing.T) {
	t.Parallel()
	e := fill(t)
	rd, cache := mocks.NewMockReader(t), mocks.NewMockCache(t)
	loc := berlin(t)
	for name, fn := range map[string]func() (*Service, error){
		"site":   func() (*Service, error) { return New(nil, rd, cache, e.clock, loc) },
		"reader": func() (*Service, error) { return New(e.site, nil, cache, e.clock, loc) },
		"cache":  func() (*Service, error) { return New(e.site, rd, nil, e.clock, loc) },
		"clock":  func() (*Service, error) { return New(e.site, rd, cache, nil, loc) },
		"location": func() (*Service, error) {
			return New(e.site, rd, cache, e.clock, nil)
		},
	} {
		_, err := fn()
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), name)
	}
	names := e.svc.Names()
	assert.IsIncreasing(t, names)
	assert.Contains(t, names, site.QuantityConsumption)
	assert.Len(t, names, len(Catalogue(e.site)))
}

var errBoom = errors.New("boom")

func TestEnergyReaderErrors(t *testing.T) {
	t.Parallel()
	key := storage.SeriesKey{Meter: "pv", Channel: "energy_out"}
	cases := []struct {
		name  string
		level Level
		setup func(rd *mocks.MockReader, c *mocks.MockCache)
	}{
		{"series id", LevelDay, func(rd *mocks.MockReader, _ *mocks.MockCache) {
			rd.EXPECT().SeriesID(mock.Anything, key).Return(0, errBoom)
		}},
		{"cache get", LevelDay, func(rd *mocks.MockReader, c *mocks.MockCache) {
			rd.EXPECT().SeriesID(mock.Anything, key).Return(1, nil)
			rd.EXPECT().FirstSlot(mock.Anything).Return(0, true, nil)
			c.EXPECT().CacheGet(mock.Anything, mock.Anything, int64(0)).
				Return(nil, errBoom)
		}},
		{"first slot", LevelDay, func(rd *mocks.MockReader, _ *mocks.MockCache) {
			rd.EXPECT().SeriesID(mock.Anything, key).Return(1, nil)
			rd.EXPECT().FirstSlot(mock.Anything).Return(0, false, errBoom)
		}},
		{"sum", LevelDay, func(rd *mocks.MockReader, c *mocks.MockCache) {
			rd.EXPECT().SeriesID(mock.Anything, key).Return(1, nil)
			c.EXPECT().CacheGet(mock.Anything, mock.Anything, int64(0)).
				Return(nil, nil)
			rd.EXPECT().FirstSlot(mock.Anything).Return(0, true, nil)
			rd.EXPECT().SumEnergyMany(mock.Anything, mock.Anything, mock.Anything,
				mock.Anything, mock.Anything).Return(nil, errBoom)
		}},
		{"cache put", LevelDay, func(rd *mocks.MockReader, c *mocks.MockCache) {
			rd.EXPECT().SeriesID(mock.Anything, key).Return(1, nil)
			c.EXPECT().CacheGet(mock.Anything, mock.Anything, int64(0)).
				Return(nil, nil)
			rd.EXPECT().FirstSlot(mock.Anything).Return(0, true, nil)
			rd.EXPECT().SumEnergyMany(mock.Anything, mock.Anything, mock.Anything,
				mock.Anything, mock.Anything).Return(nil, nil)
			c.EXPECT().CachePut(mock.Anything, int64(0), mock.Anything, mock.Anything).
				Return(errBoom)
		}},
		{"carry", LevelTotal, func(rd *mocks.MockReader, c *mocks.MockCache) {
			rd.EXPECT().SeriesID(mock.Anything, key).Return(1, nil)
			c.EXPECT().CacheGet(mock.Anything, mock.Anything, int64(0)).
				Return(nil, nil).Maybe()
			c.EXPECT().CachePut(mock.Anything, int64(0), mock.Anything, mock.Anything).
				Return(nil).Maybe()
			rd.EXPECT().FirstSlot(mock.Anything).Return(0, true, nil)
			rd.EXPECT().SumEnergyMany(mock.Anything, mock.Anything, mock.Anything,
				mock.Anything, mock.Anything).Return(nil, nil).Maybe()
			rd.EXPECT().Carry(mock.Anything, mock.Anything).Return(nil, errBoom)
		}},
		{"first slot total", LevelTotal, func(rd *mocks.MockReader, _ *mocks.MockCache) {
			rd.EXPECT().SeriesID(mock.Anything, key).Return(1, nil)
			rd.EXPECT().FirstSlot(mock.Anything).Return(0, false, errBoom)
		}},
		{"buckets", LevelBucket, func(rd *mocks.MockReader, _ *mocks.MockCache) {
			rd.EXPECT().SeriesID(mock.Anything, key).Return(1, nil)
			rd.EXPECT().EnergyBuckets(mock.Anything, int64(1), mock.Anything, mock.Anything).
				Return(nil, errBoom)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			loc := berlin(t)
			rd, c := mocks.NewMockReader(t), mocks.NewMockCache(t)
			c.EXPECT().CacheGen().Return(0).Maybe()
			tc.setup(rd, c)
			svc, err := New(buildSite(t, false, false), rd, c,
				clocktest.New(time.Date(2026, 11, 19, 12, 0, 0, 0, loc)), loc)
			require.NoError(t, err)
			d := time.Date(2026, 11, 10, 0, 0, 0, 0, loc)
			_, err = svc.Energy(context.Background(), []string{"pv/energy_out"}, tc.level, d, d)
			require.ErrorIs(t, err, errBoom)
		})
	}
}

func TestSeriesIDNotFoundAndCorruptCache(t *testing.T) {
	t.Parallel()
	loc := berlin(t)
	rd, c := mocks.NewMockReader(t), mocks.NewMockCache(t)
	rd.EXPECT().SeriesID(mock.Anything, storage.SeriesKey{Meter: "pv", Channel: "energy_out"}).
		Return(7, nil)
	rd.EXPECT().FirstSlot(mock.Anything).Return(0, true, nil)
	c.EXPECT().CacheGet(mock.Anything, mock.Anything, int64(0)).
		RunAndReturn(func(_ context.Context, keys []string, _ int64) (map[string]string, error) {
			return map[string]string{keys[0]: "{not json"}, nil
		})
	rd.EXPECT().SumEnergyMany(mock.Anything, []int64{7}, mock.Anything, mock.Anything,
		mock.Anything).Return(map[int64]storage.EnergySum{7: {Wh: 3, Buckets: 96, Closed: 96}}, nil)
	c.EXPECT().CacheGen().Return(0)
	c.EXPECT().CachePut(mock.Anything, int64(0), mock.Anything, mock.Anything).Return(nil)
	svc, err := New(buildSite(t, false, false), rd, c,
		clocktest.New(time.Date(2026, 11, 19, 12, 0, 0, 0, loc)), loc)
	require.NoError(t, err)
	d := time.Date(2026, 11, 18, 0, 0, 0, 0, loc)
	res, err := svc.Energy(context.Background(), []string{"pv/energy_out"}, LevelDay, d, d)
	require.NoError(t, err)
	assert.InDelta(t, 3, *res[0].Values[0].Value.Value, 1e-9, "corrupt entry is recomputed")

	// ErrNotFound counts as "never stored"
	rd2 := mocks.NewMockReader(t)
	rd2.EXPECT().SeriesID(mock.Anything, mock.Anything).Return(0, storage.ErrNotFound)
	rd2.EXPECT().FirstSlot(mock.Anything).Return(0, false, nil).Maybe()
	rd2.EXPECT().SumEnergyMany(mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything).Return(nil, nil).Maybe()
	c2 := mocks.NewMockCache(t)
	c2.EXPECT().CacheGet(mock.Anything, mock.Anything, mock.Anything).Return(nil, nil).Maybe()
	c2.EXPECT().CacheGen().Return(0).Maybe()
	c2.EXPECT().CachePut(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Maybe()
	svc, err = New(buildSite(t, false, false), rd2, c2,
		clocktest.New(time.Date(2026, 11, 19, 12, 0, 0, 0, loc)), loc)
	require.NoError(t, err)
	res, err = svc.Energy(context.Background(), []string{"pv/energy_out"}, LevelDay, d, d)
	require.NoError(t, err)
	assert.Nil(t, res[0].Values[0].Value.Value)
}
