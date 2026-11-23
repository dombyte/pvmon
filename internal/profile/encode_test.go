package profile

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/profiles"
)

// TestEncodeRoundTrip encodes values with every built-in profile and decodes them again.
func TestEncodeRoundTrip(t *testing.T) {
	t.Parallel()
	l, err := NewLoader(profiles.FS(), os.ReadFile)
	require.NoError(t, err)
	for _, id := range l.IDs() {
		p, err := l.Load(id, ".")
		require.NoError(t, err)
		want := encodable(p.Profile)
		s, err := p.Encode(want)
		require.NoError(t, err, id)
		raw, err := s.Raw()
		require.NoError(t, err, id)
		for bn := range p.Blocks {
			compareBlock(t, id, bn, p.Channels(bn, raw), want)
		}
		for name, st := range p.Statuses(raw) {
			assert.Equal(t, LevelOK, st.Level, "%s status %s", id, name)
		}
	}
}

func encodable(p *Profile) map[string]float64 {
	want := map[string]float64{}
	for _, bn := range sortedKeys(p.Blocks) {
		for cn, c := range p.Blocks[bn].Channels {
			if len(c.Product) == 0 && c.Derive == "" && c.Type != "enum" {
				want[bn+"."+cn] = roundTripValue(c)
			}
		}
	}
	return want
}

func compareBlock(t *testing.T, id, bn string, got map[string]Value, want map[string]float64) {
	t.Helper()
	for cn, v := range got {
		if w, ok := want[bn+"."+cn]; ok {
			assert.InDelta(t, w, v.Num, 1e-3*max(1, w), "%s %s.%s", id, bn, cn)
		}
	}
}

// roundTripValue picks a value representable by the channel's raw type and scale.
func roundTripValue(c Channel) float64 {
	v := 123.0
	if c.Counter != "" || energyUnits()[c.Unit] > 0 {
		v = 4_500_000
	}
	if c.Type == "u16" || c.Type == "i16" {
		v = 1200
	}
	if f := energyUnits()[c.Unit]; f > 0 {
		v = float64(int(v/(f*c.ScaleOr1()))) * f * c.ScaleOr1()
	}
	return v
}

func TestEncodeNegativeAndOrders(t *testing.T) {
	t.Parallel()
	p := parseTestdata(t, "modbus.yaml")
	p.Modbus.ByteOrder, p.Modbus.WordOrder = "little", "little"
	s, err := p.Encode(map[string]float64{"battery.power": -10, "pv.power": 70000})
	require.NoError(t, err)
	raw, err := s.Raw()
	require.NoError(t, err)
	assert.InDelta(t, -10, p.Channels("battery", raw)["power"].Num, 0)
	assert.InDelta(t, 70000, p.Channels("pv", raw)["power"].Num, 0)
	assert.Equal(t, 0.0, direction(Channel{SignFrom: &SignFrom{DischargeWhen: 1}}, -1))
	assert.Equal(t, 1.0, direction(Channel{SignFrom: &SignFrom{}}, -1))
	assert.Len(t, encodeNumber("u64", 1, 4), 8)
	assert.Len(t, encodeNumber("f64", 1, 4), 8)
	setPath(map[string]any{}, "x", "$", 1)
}
