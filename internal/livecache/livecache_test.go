package livecache_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/eventbus"
	"github.com/dombyte/pvmon/internal/livecache"
	"github.com/dombyte/pvmon/internal/livecache/mocks"
	"github.com/dombyte/pvmon/internal/util/clocktest"
)

func TestCache(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	clock := clocktest.New(t0)
	pub := mocks.NewMockPublisher(t)
	c, err := livecache.New(pub, clock)
	require.NoError(t, err)

	pub.EXPECT().Publish(eventbus.ValueChanged{Key: "pv/power", At: t0}).Once()
	c.Set("pv/power", livecache.Value{Num: 1, Unit: "W", At: t0}, 5*time.Second)
	c.Set("pv/power", livecache.Value{Num: 1, Unit: "W", At: t0}, 5*time.Second) // unchanged
	pub.EXPECT().Publish(mock.Anything).Once()
	c.Set("pv/power", livecache.Value{Num: 2, At: t0}, 5*time.Second)

	v, ok := c.Get("pv/power")
	require.True(t, ok)
	assert.InDelta(t, 2, v.Num, 0)
	clock.Advance(15 * time.Second)
	_, ok = c.Get("pv/power")
	assert.True(t, ok, "exactly 3 intervals old is still valid")
	clock.Advance(time.Second)
	_, ok = c.Get("pv/power")
	assert.False(t, ok, "older than 3 intervals is missing")
	_, ok = c.Get("nope")
	assert.False(t, ok)
	c.Delete("pv/power")

	_, err = livecache.New(nil, clock)
	require.Error(t, err)
}
