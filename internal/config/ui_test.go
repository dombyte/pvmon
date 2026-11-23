package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yamlv3 "go.yaml.in/yaml/v3"
)

func TestUISettingKeys(t *testing.T) {
	t.Parallel()
	known := KnownKeys()
	keys := UISettingKeys()
	assert.Contains(t, keys, "site.name")
	for _, k := range keys {
		assert.True(t, matchesAny(known, k), "%s is a known key", k)
	}
}

func TestSecretPaths(t *testing.T) {
	t.Parallel()
	assert.Equal(t, map[string][]string{"devices": {"auth.password"}}, SecretPaths())
}

func TestCheckLayer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		layer map[string]any
		want  []string
	}{
		{"flat known", map[string]any{"site.name": "x", "devices.wr1.unit": 2}, nil},
		{"nested known", map[string]any{"site": map[string]any{"name": "x"}}, nil},
		{"flat unknown", map[string]any{"site.nope": 1, "site.name": "x"}, []string{"site.nope"}},
		{
			"nested unknown",
			map[string]any{"devices": map[string]any{"wr1": map[string]any{"bogus": 1}}},
			[]string{"devices.wr1.bogus"},
		},
		{"empty", map[string]any{}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, p := range CheckLayer(tc.layer) {
				assert.Equal(t, "unknown key", p.Message)
				got = append(got, p.Path)
			}
			assert.ElementsMatch(t, tc.want, got)
		})
	}
}

const uiYAML = `
connections:
  lan: { address: "tcp://10.0.0.20:502" }
devices:
  wr1: { connection: lan, unit: 1, profile: solis/s6-hybrid }
`

func TestExport(t *testing.T) {
	t.Parallel()
	res, err := Load(Source{
		Data: []byte(uiYAML),
		DB: map[string]any{
			"devices.warp.address":       "http://10.0.0.41",
			"devices.warp.profile":       "tinkerforge/warp3",
			"devices.warp.auth.user":     "admin",
			"devices.warp.auth.password": "plain",
			"devices.ref.address":        "http://10.0.0.42",
			"devices.ref.profile":        "tinkerforge/warp3",
			"devices.ref.auth.user":      "admin",
			"devices.ref.auth.password":  "${env:X}",
		},
		Getenv: getenv(map[string]string{"X": "v"}),
	})
	require.NoError(t, err)
	out, err := Export(res)
	require.NoError(t, err)
	assert.NotContains(t, string(out), "plain")
	var got map[string]any
	require.NoError(t, yamlv3.Unmarshal(out, &got))
	devs := got["devices"].(map[string]any)
	auth := func(id string) any {
		return devs[id].(map[string]any)["auth"].(map[string]any)["password"]
	}
	assert.Equal(t, "***", auth("warp"))
	assert.Equal(t, "${env:X}", auth("ref"))
	assert.NotContains(t, got, "app", "defaults are not exported")
	assert.NotContains(t, devs["wr1"].(map[string]any), "interval")
}

func TestResultObjectValueIDs(t *testing.T) {
	t.Parallel()
	res, err := Load(Source{
		Data: []byte(uiYAML + "site: { name: Home }\n"),
		DB: map[string]any{
			"devices.warp.address":       "http://10.0.0.41",
			"devices.warp.profile":       "tinkerforge/warp3",
			"devices.warp.auth.user":     "admin",
			"devices.warp.auth.password": "plain",
		},
	})
	require.NoError(t, err)

	obj, ok := res.Object("devices", "warp")
	require.True(t, ok)
	assert.Equal(t, "tinkerforge/warp3", obj["profile"])
	assert.Equal(t, "***", obj["auth"].(map[string]any)["password"])
	_, ok = res.Object("devices", "nope")
	assert.False(t, ok)

	assert.Equal(t, "Home", res.Value("site.name"))
	assert.Equal(t, 8080, res.Value("app.port"), "default")
	assert.ElementsMatch(t, []string{"wr1", "warp"}, res.ObjectIDs("devices"))
	assert.ElementsMatch(t, []string{"lan"}, res.ObjectIDs("connections"))
	assert.Empty(t, res.ObjectIDs("meters"))
}

func TestResultLocked(t *testing.T) {
	t.Parallel()
	res, err := Load(Source{
		Data:    []byte(uiYAML + "site: { name: Home }\n"),
		Environ: []string{"PVMON_APP__PORT=9000", "PVMON_CONNECTIONS__LAN__TIMEOUT=3s"},
	})
	require.NoError(t, err)
	tests := []struct {
		path string
		want bool
	}{
		{"site.name", true},
		{"app.port", true},
		{"devices.wr1", true},
		{"devices.wr1.unit", true},
		{"connections.lan", true},
		{"site.interval", false},
		{"devices.wr2", false},
		{"devices.wr", false},
		{"app", true},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, res.Locked(tc.path), tc.path)
	}
}

func TestLoadFileObjectWinsOverDBObject(t *testing.T) {
	t.Parallel()
	res, err := Load(Source{
		Data: []byte(uiYAML),
		DB: map[string]any{
			"devices.wr1.unit":     9,
			"devices.wr1.timeout":  "7s",
			"devices.warp.address": "http://10.0.0.41",
			"devices.warp.profile": "tinkerforge/warp3",
			"devices.warp.unit":    4,
		},
	})
	require.NoError(t, err)
	wr1 := res.Config.Devices["wr1"]
	assert.Equal(t, 1, wr1.Unit, "file object replaces the DB object entirely")
	obj, _ := res.Object("devices", "wr1")
	assert.NotContains(t, obj, "timeout", "no leak from the DB object")
	assert.Equal(t, 4, res.Config.Devices["warp"].Unit, "other DB objects stay")
}

func TestProblemsErrDedupes(t *testing.T) {
	t.Parallel()
	ps := Problems{
		{Path: "b", Message: "x"},
		{Path: "a", Message: "y"},
		{Path: "b", Message: "x"},
		{Path: "b", Message: "z"},
	}
	var ve *ValidationError
	require.ErrorAs(t, ps.Err(), &ve)
	assert.Equal(t, Problems{
		{Path: "a", Message: "y"}, {Path: "b", Message: "x"}, {Path: "b", Message: "z"},
	}, ve.Problems)
	assert.NoError(t, Problems(nil).Err())
}

func TestValueTypesEnvStrings(t *testing.T) {
	t.Parallel()
	res := &Result{Layers: map[string]any{
		"auth":    map[string]any{"public_read": "false"},
		"storage": map[string]any{"max_backups": "3", "retention": "2y"},
		"status":  map[string]any{"offline_after": 5},
		"site":    map[string]any{"name": "x", "interval": "nope"},
		"app":     map[string]any{"port": "x"},
		"other":   "y",
	}}
	assert.Equal(t, false, res.Value("auth.public_read"))
	assert.Equal(t, 3, res.Value("storage.max_backups"))
	assert.Equal(t, "2y", res.Value("storage.retention"))
	assert.Equal(t, 5, res.Value("status.offline_after"), "non-strings stay")
	assert.Equal(t, "x", res.Value("app.port"), "an int that does not parse stays text")
	assert.Equal(t, "y", res.Value("other"), "unknown keys stay")
	res.Layers["auth"] = map[string]any{"public_read": "maybe"}
	assert.Equal(t, "maybe", res.Value("auth.public_read"))
}
