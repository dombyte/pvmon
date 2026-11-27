package collector_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dombyte/pvmon/internal/storage"
)

// TestOutageEnergyStaysInOutage: the counter is idle, then the load runs only while the
// device cannot be read. The energy lands in the flagged outage slots, not on the last
// slot where power was measured.
func TestOutageEnergyStaysInOutage(t *testing.T) {
	t.Parallel()
	r := newCoarseRun(t, func(now time.Time) float64 {
		switch {
		case now.Before(minutes(5)): // some power long before the outage
			return 600
		case !now.Before(minutes(60)) && now.Before(minutes(90)):
			return 4000
		}
		return 0
	})
	r.failFrom, r.fail = minutes(60).Add(-pollInterval), minutes(90)
	r.session(t, minutes(120))
	before := r.e.total(t, "backup", "energy_in", moreStart, minutes(60))
	assert.InDelta(t, 50, before.Wh, 50+1, "only what was measured before")
	// The outage starts in the last of these slots, so only that one is flagged.
	assert.Zero(t, r.e.total(t, "backup", "energy_in", moreStart, minutes(45)).Flags&
		storage.FlagEstimated)
	outage := r.e.total(t, "backup", "energy_in", minutes(60), minutes(90))
	assert.InDelta(t, 2000, outage.Wh, 101)
	assert.NotZero(t, outage.Flags&storage.FlagEstimated)
}
