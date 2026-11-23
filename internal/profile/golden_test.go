package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/profiles"
)

// TestGolden decodes the fixture of every built-in profile and compares all channels and
// status levels with its golden file (profiles/<vendor>/<model>.{fixture,golden}.json).
func TestGolden(t *testing.T) {
	t.Parallel()
	l, err := NewLoader(profiles.FS(), os.ReadFile)
	require.NoError(t, err)
	for _, id := range l.IDs() {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			p, err := l.Load(id, ".")
			require.NoError(t, err)
			base := filepath.Join("..", "..", "profiles", id)
			var fx Fixture
			var gold Golden
			readJSON(t, base+".fixture.json", &fx)
			readJSON(t, base+".golden.json", &gold)
			require.Equal(t, id, fx.Profile)
			require.Len(t, gold.Samples, len(fx.Samples))
			for i, s := range fx.Samples {
				compareSample(t, p.Profile, s, gold.Samples[i])
			}
		})
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err, "every built-in profile ships a fixture and golden values")
	require.NoError(t, json.Unmarshal(b, v), path)
}

func compareSample(t *testing.T, p *Profile, s Sample, want GoldenSample) {
	t.Helper()
	raw, err := s.Raw()
	require.NoError(t, err)
	got := map[string]any{}
	for _, bn := range sortedKeys(p.Blocks) {
		for cn, v := range p.Channels(bn, raw) {
			got[bn+"."+cn] = goldenValue(v)
		}
	}
	for key, w := range want.Channels {
		g, ok := got[key]
		require.True(t, ok, "channel %s not decoded", key)
		compareValue(t, key, w, g)
	}
	for key, g := range got {
		_, listed := want.Channels[key]
		assert.True(t, listed || g == nil, "channel %s decoded but missing in golden", key)
	}
	for name, sv := range p.Statuses(raw) {
		assert.Equal(t, want.Status[name], levelOrMissing(sv), "status %s", name)
	}
}

func compareValue(t *testing.T, key string, want, got any) {
	t.Helper()
	wf, isNum := want.(float64)
	if !isNum {
		assert.Equal(t, want, got, key)
		return
	}
	gf, ok := got.(float64)
	require.True(t, ok, "%s: got %v, want number %v", key, got, want)
	assert.InDelta(t, wf, gf, 1e-6, key)
}

func goldenValue(v Value) any {
	switch {
	case !v.Valid:
		return nil
	case v.Text != "":
		return v.Text
	default:
		return v.Num
	}
}

func levelOrMissing(sv StatusValue) string {
	if !sv.Valid {
		return "missing"
	}
	return strings.ToLower(sv.Level)
}
