package derive

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func day(t *testing.T, loc *time.Location, y int, m time.Month, d int) time.Time {
	t.Helper()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

func TestPeriodsDayDST(t *testing.T) {
	t.Parallel()
	loc := berlin(t)
	cases := []struct {
		name  string
		from  time.Time
		slots int64
		key   string
	}{
		{"normal", day(t, loc, 2026, 3, 28), 96, "2026-03-28"},
		{"spring", day(t, loc, 2026, 3, 29), 92, "2026-03-29"},
		{"autumn", day(t, loc, 2026, 10, 25), 100, "2026-10-25"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// from/to inside the day: only the local date counts
			in := tc.from.Add(13*time.Hour + 5*time.Minute)
			ps, err := Periods(LevelDay, in, in, loc)
			require.NoError(t, err)
			require.Len(t, ps, 1)
			assert.Equal(t, tc.key, ps[0].Key)
			assert.Equal(t, tc.slots, ps[0].To-ps[0].From)
			assert.Equal(t, Slot(tc.from), ps[0].From)
		})
	}
	ps, err := Periods(LevelDay, day(t, loc, 2026, 3, 28), day(t, loc, 2026, 3, 30), loc)
	require.NoError(t, err)
	require.Len(t, ps, 3)
	assert.Equal(t, ps[0].To, ps[1].From)
	assert.Equal(t, ps[1].To, ps[2].From)
}

func TestPeriodsMonthYearWhole(t *testing.T) {
	t.Parallel()
	loc := berlin(t)
	ps, err := Periods(LevelMonth, day(t, loc, 2026, 3, 15), day(t, loc, 2026, 3, 16), loc)
	require.NoError(t, err)
	require.Len(t, ps, 1)
	assert.Equal(t, "2026-03", ps[0].Key)
	assert.Equal(t, Slot(day(t, loc, 2026, 3, 1)), ps[0].From)
	assert.Equal(t, int64(31*96-4), ps[0].To-ps[0].From, "March has the short day")

	ps, err = Periods(LevelMonth, day(t, loc, 2026, 11, 30), day(t, loc, 2027, 1, 1), loc)
	require.NoError(t, err)
	require.Len(t, ps, 3)
	assert.Equal(t, "2026-11", ps[0].Key)
	assert.Equal(t, "2027-01", ps[2].Key)

	ps, err = Periods(LevelYear, day(t, loc, 2025, 12, 31), day(t, loc, 2026, 6, 1), loc)
	require.NoError(t, err)
	require.Len(t, ps, 2)
	assert.Equal(t, "2025", ps[0].Key)
	assert.Equal(t, ps[0].To, ps[1].From)
	assert.Equal(t, Slot(day(t, loc, 2027, 1, 1)), ps[1].To)
}

func TestPeriodsTotalAndBucket(t *testing.T) {
	t.Parallel()
	loc := berlin(t)
	ps, err := Periods(LevelTotal, day(t, loc, 2026, 1, 1), day(t, loc, 2026, 1, 2), loc)
	require.NoError(t, err)
	require.Len(t, ps, 1)
	assert.Equal(t, "total", ps[0].Key)
	assert.Equal(t, int64(192), ps[0].To-ps[0].From)

	for _, tc := range []struct {
		d     time.Time
		slots int
	}{
		{day(t, loc, 2026, 3, 29), 92},
		{day(t, loc, 2026, 10, 25), 100},
		{day(t, loc, 2026, 5, 1), 96},
	} {
		ps, err = Periods(LevelBucket, tc.d, tc.d, loc)
		require.NoError(t, err)
		assert.Len(t, ps, tc.slots)
		assert.Equal(t, ps[0].From+1, ps[0].To)
	}
	assert.Equal(t, "2026-05-01T00:15:00+02:00", ps[1].Key)
}

func TestPeriodsErrors(t *testing.T) {
	t.Parallel()
	loc := berlin(t)
	d1, d2 := day(t, loc, 2026, 5, 2), day(t, loc, 2026, 5, 1)
	for _, l := range []Level{LevelDay, LevelMonth, LevelYear, LevelTotal, LevelBucket} {
		_, err := Periods(l, d1, d2, loc)
		assert.ErrorIs(t, err, ErrInvalidRange, string(l))
	}
	_, err := Periods(Level("week"), d2, d2, loc)
	assert.ErrorIs(t, err, ErrInvalidLevel)

	_, err = Periods(LevelDay, day(t, loc, 2000, 1, 1), day(t, loc, 2012, 1, 1), loc)
	assert.ErrorIs(t, err, ErrRangeTooLong)
	ps, err := Periods(LevelDay, day(t, loc, 2020, 1, 1), day(t, loc, 2020, 1, 31), loc)
	require.NoError(t, err)
	assert.Len(t, ps, 31)

	_, err = Periods(LevelBucket, day(t, loc, 2026, 1, 1), day(t, loc, 2026, 4, 15), loc)
	assert.ErrorIs(t, err, ErrRangeTooLong, "105 days of buckets")
	ps, err = Periods(LevelBucket, day(t, loc, 2026, 1, 1), day(t, loc, 2026, 4, 14), loc)
	require.NoError(t, err)
	assert.Len(t, ps, 104*96-4, "March 29 is short")
}

func TestDaysAndMonths(t *testing.T) {
	t.Parallel()
	loc := berlin(t)
	month, err := Periods(LevelMonth, day(t, loc, 2026, 10, 1), day(t, loc, 2026, 10, 1), loc)
	require.NoError(t, err)
	ds := Days(month[0], loc)
	require.Len(t, ds, 31)
	assert.Equal(t, "2026-10-25", ds[24].Key)
	assert.Equal(t, int64(100), ds[24].To-ds[24].From)
	assert.Equal(t, month[0].To, ds[30].To)

	year, err := Periods(LevelYear, day(t, loc, 2026, 1, 1), day(t, loc, 2026, 1, 1), loc)
	require.NoError(t, err)
	ms := Months(year[0], loc)
	require.Len(t, ms, 12)
	assert.Equal(t, "2026-12", ms[11].Key)
	assert.Len(t, Days(year[0], loc), 365)
}
