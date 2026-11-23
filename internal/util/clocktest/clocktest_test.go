package clocktest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC)

func recv(ch <-chan time.Time) (time.Time, bool) {
	select {
	case v := <-ch:
		return v, true
	default:
		return time.Time{}, false
	}
}

func TestTimer(t *testing.T) {
	t.Parallel()
	c := New(t0)
	tm := c.NewTimer(10 * time.Second)
	assert.Equal(t, 1, c.Waiters())
	c.Advance(9 * time.Second)
	_, ok := recv(tm.C())
	assert.False(t, ok)
	c.Advance(time.Second)
	v, ok := recv(tm.C())
	require.True(t, ok)
	assert.Equal(t, t0.Add(10*time.Second), v)
	assert.Equal(t, 0, c.Waiters())
	assert.False(t, tm.Stop())
	assert.False(t, tm.Reset(time.Second))
	assert.True(t, tm.Stop())
}

func TestTicker(t *testing.T) {
	t.Parallel()
	c := New(t0)
	tk := c.NewTicker(5 * time.Second)
	c.Advance(5 * time.Second)
	v, ok := recv(tk.C())
	require.True(t, ok)
	assert.Equal(t, t0.Add(5*time.Second), v)
	c.Advance(20 * time.Second) // buffered channel holds one tick, rest dropped
	_, ok = recv(tk.C())
	assert.True(t, ok)
	_, ok = recv(tk.C())
	assert.False(t, ok)
	tk.Reset(time.Second)
	c.Advance(time.Second)
	_, ok = recv(tk.C())
	assert.True(t, ok)
	tk.Stop()
	assert.Equal(t, 0, c.Waiters())
}

func TestSetAndOrder(t *testing.T) {
	t.Parallel()
	c := New(t0)
	a := c.NewTimer(2 * time.Second)
	b := c.NewTimer(time.Second)
	c.Set(t0.Add(3 * time.Second))
	va, _ := recv(a.C())
	vb, _ := recv(b.C())
	assert.True(t, vb.Before(va))
	assert.Equal(t, t0.Add(3*time.Second), c.Now())
	c.Set(t0) // backwards jump only moves the time
	assert.Equal(t, t0, c.Now())
}

func TestBlockUntil(t *testing.T) {
	t.Parallel()
	c := New(t0)
	done := make(chan struct{})
	go func() {
		tm := c.NewTimer(time.Minute)
		<-tm.C()
		close(done)
	}()
	c.BlockUntil(1)
	c.Advance(time.Minute)
	<-done
}
