package eventbus

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPublishSubscribe(t *testing.T) {
	t.Parallel()
	b := New()
	newest := b.Subscribe(1, DropNewest)
	oldest := b.Subscribe(0, DropOldest)
	b.Publish(ValueChanged{Key: "a"})
	b.Publish(DeviceState{Device: "d", State: "offline"})
	b.Publish(StatusChanged{Device: "d"})

	assert.Equal(t, ValueChanged{Key: "a"}, <-newest.C())
	assert.Equal(t, uint64(2), newest.Dropped())
	assert.Equal(t, StatusChanged{Device: "d"}, <-oldest.C())
	assert.Equal(t, uint64(2), oldest.Dropped())

	b.Unsubscribe(newest)
	b.Unsubscribe(newest)
	_, open := <-newest.C()
	assert.False(t, open)
	b.Publish(ValueChanged{Key: "b"}) // no panic on removed subscriber
	assert.Equal(t, ValueChanged{Key: "b"}, <-oldest.C())
}
