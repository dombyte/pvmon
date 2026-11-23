package service_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/service"
)

func TestProbeBlocks(t *testing.T) {
	t.Parallel()
	p := loadProfile(t, "shelly/pro-3em").Profile
	sample, err := p.Encode(map[string]float64{"total.power": 1234.567, "a.voltage": 230})
	require.NoError(t, err)
	raw, err := sample.Raw()
	require.NoError(t, err)
	blocks := service.ProbeBlocks(p, &raw)
	require.Len(t, blocks, 4)
	total := blocks[3]
	assert.Equal(t, "total", total.Name)
	assert.Equal(t, "grid", total.Role)
	assert.Equal(t, []string{"grid", "consumer"}, total.RolesAllowed)
	require.Equal(t, "power", total.Channels[2].Name)
	assert.InDelta(t, 1234.57, *total.Channels[2].Value, 1e-9, "rounded to 2 decimals")
	assert.Equal(t, "W", total.Channels[2].Unit)
	assert.Equal(t, "a", blocks[0].Name)

	none := service.ProbeBlocks(p, nil)
	require.Len(t, none, 4)
	for _, c := range none[3].Channels {
		assert.Nil(t, c.Value, c.Name)
	}
}

func TestProbeValueText(t *testing.T) {
	t.Parallel()
	p := loadProfile(t, "shelly/pro-3em").Profile
	ch := p.Blocks["total"].Channels["power"]
	ch.Type, ch.Path = "string", "$.name"
	p.Blocks["total"].Channels["power"] = ch
	raw := profile.Raw{JSON: map[string]any{"live": map[string]any{"name": "pro"}}}
	got := service.ProbeBlocks(p, &raw)[3].Channels[2]
	assert.Equal(t, "pro", got.Text)
	assert.Nil(t, got.Value)
}
