package collector_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/util/clocktest"
)

// gate1Expected is written by scripts/gate1-expected.sh from the solis database.
type gate1Expected struct {
	Date      string             `json:"date"`      // local day, YYYY-MM-DD
	Series    map[string]float64 `json:"series"`    // pvmon series → kWh
	Tolerance map[string]float64 `json:"tolerance"` // kWh where solis is coarser than ±0.01
}

// fixtureSource replays recorded samples: at each poll it serves the newest sample at or
// before the clock (recorded at the dump interval).
type fixtureSource struct {
	clock   *clocktest.Clock
	samples []profile.Sample
}

func (f *fixtureSource) Fetch(context.Context) (profile.Sample, error) {
	now := f.clock.Now()
	i := sort.Search(len(f.samples), func(i int) bool { return f.samples[i].At.After(now) }) - 1
	if i < 0 || now.Sub(f.samples[i].At) > 2*pollInterval {
		return profile.Sample{At: now}, errors.New("no recorded sample")
	}
	s := f.samples[i]
	if s.Err != "" {
		return profile.Sample{At: now}, errors.New(s.Err)
	}
	s.At = now
	return s, nil
}

// TestGate1SolisRecording replays a recorded Solis day (pvmon dump) and compares every
// series with the daily values of the solis database (±0.01 kWh unless expected.json
// names a coarser tolerance, spec 16 Gate 1). It runs only when PVMON_GATE1_DIR holds
// solis.fixture.json and expected.json.
func TestGate1SolisRecording(t *testing.T) {
	dir := os.Getenv("PVMON_GATE1_DIR")
	if dir == "" {
		t.Skip("PVMON_GATE1_DIR not set; record a day with pvmon dump (AGENTS.md section 12.2)")
	}
	var fx profile.Fixture
	var want gate1Expected
	readJSONFile(t, filepath.Join(dir, "solis.fixture.json"), &fx)
	readJSONFile(t, filepath.Join(dir, "expected.json"), &want)
	require.NotEmpty(t, fx.Samples)
	day, err := time.ParseInLocation(time.DateOnly, want.Date, berlin)
	require.NoError(t, err)

	r := newRig(t, "meter", fx.Samples[0].At)
	r.src = nil
	c := r.collectorWith(t, &fixtureSource{clock: r.clock, samples: fx.Samples})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, c.Start(ctx))
	r.runUntil(fx.Samples[len(fx.Samples)-1].At)
	require.NoError(t, c.Stop())
	const defaultTolerance = 0.01 // kWh
	for key, kwh := range want.Series {
		m, ch, _ := cut(key)
		tol, ok := want.Tolerance[key]
		if !ok {
			tol = defaultTolerance
		}
		assert.InDelta(t, kwh*1000, r.dayWh(t, m, ch, day), tol*1000, key)
	}
}

func readJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, v))
}
