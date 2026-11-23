package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/profiles"
)

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(b)
}

func TestParseValid(t *testing.T) {
	t.Parallel()
	p, err := Parse([]byte(readTestdata(t, "modbus.yaml")))
	require.NoError(t, err)
	assert.Equal(t, 10, p.Read.Limit())
	assert.Equal(t, Addrs{202, 203}, p.Status["faults"].Addr)
	assert.Equal(t, LevelNotice, p.Maps["st"].Codes[0].Level)
	assert.Equal(t, LevelFault, p.Maps["bits"].Bits[0].Level)
	assert.Equal(t, 2, p.Blocks["pv"].Channels["power"].Width())
	pv := p.Blocks["pv"]
	assert.True(t, pv.Channels["energy_out"].Historised("energy_out"))
	assert.False(t, pv.Channels["total"].Historised("total"))
	assert.True(t, pv.Channels["power"].Historised("power"))
	assert.False(t, p.Blocks["pv1"].Channels["voltage"].Historised("voltage"))
	assert.InDelta(t, 0.1, pv.Channels["energy_out"].ScaleOr1(), 0)
	assert.InDelta(t, 1.0, pv.Channels["power"].ScaleOr1(), 0)
	assert.True(t, p.Blocks["battery"].Allows(RoleBattery))
	assert.False(t, p.Blocks["battery"].Allows(RoleGrid))

	h, err := Parse([]byte(readTestdata(t, "http.yaml")))
	require.NoError(t, err)
	assert.True(t, h.Blocks["total"].Allows(RoleConsumer))
	assert.Equal(t, "busy", h.Blocks["total"].Channels["state"].Values[1])
	assert.Equal(t, DefaultMaxRegisters, (*ReadOptions)(nil).Limit())
}

func TestParseProblems(t *testing.T) {
	t.Parallel()
	mb := readTestdata(t, "modbus.yaml")
	ht := readTestdata(t, "http.yaml")
	cases := []struct {
		name, base, old, new, want string
	}{
		{"schema", mb, "schema: 1", "schema: 2", "schema: must be 1"},
		{"id", mb, "test/modbus", "Test", "profile: ID"},
		{"transport", mb, "transport: modbus", "transport: mqtt", "transport: must be"},
		{"netting", mb, "netting: app", "netting: x", "netting: must be"},
		{"max_power", mb, "max_power: 10000", "max_power: 0", "max_power: required"},
		{"function", mb, "function: input", "function: coil", "modbus.function"},
		{"mode", mb, "mode: range", "mode: auto", "read.mode"},
		{"maxreg", mb, "max_registers: 10", "max_registers: 200", "read.max_registers"},
		{"rangelen", mb, "[200, 206]", "[200, 220]", "read.ranges.1"},
		{"outside", mb, "addr: 109, type: u16, unit", "addr: 150, type: u16, unit", "not inside"},
		{"collision", mb, "addr: 105, type: u16", "addr: 101, type: u16", "collides"},
		{"width", mb, "addr: 109, type: bitfield", "addr: 109, type: u32", "collides"},
		{"mbtype", mb, "addr: 107, type: i16", "addr: 107, type: number", "not a Modbus type"},
		{"strlen", mb, "length: 3", "length: 0", "required for strings"},
		{"role", mb, "role: pv\n    channels:\n      power: { addr: 100", "role: sun\n    channels:\n      power: { addr: 100", "unknown role"},
		{"rolesallowed", mb, "roles_allowed: [battery]", "roles_allowed: [x]", "unknown role"},
		{"counter", mb, "counter: lifetime", "counter: forever", ".counter"},
		{"resetgroup", mb, "reset_group: daily, ", "", "reset_group: required"},
		{"energyunit", mb, "unit: kWh, counter: lifetime", "unit: W, counter: lifetime", "energy unit"},
		{"powerunit", mb, "type: u32, unit: W }", "type: u32, unit: V }", "power needs W"},
		{"scale0", mb, "scale: 0.1, unit: V", "scale: 0, unit: V", "scale: must not be 0"},
		{"twosources", mb, "{ product: [pv1.voltage", "{ addr: 104, product: [pv1.voltage", "exactly one"},
		{"product", mb, "pv1.current]", "pv1.nope]", "unknown channel"},
		{"productlen", mb, "[pv1.voltage, pv1.current]", "[pv1.voltage]", "needs two"},
		{"derive", mb, "derive: share", "derive: split", "only share"},
		{"deriveof", mb, "of: pv.energy_out", "of: pv.power", "not an energy channel"},
		{"deriveby", mb, "by: power }", "by: nope }", "unknown channel \"nope\""},
		{"signfrom", mb, "sign_from: { addr: 108, type: u16,", "sign_from: { type: u16,", "needs addr or path"},
		{"statuskinds", mb, "type: bitfield, level: fault", "type: bitfield", "exactly one of map"},
		{"statusmap", mb, "map: st", "map: nope", "unknown map"},
		{"statusname", mb, "  operating: {", "  Operating-2: {", "name must match"},
		{"statuslabel", mb, "map: st }", "map: st, label: { en: Op } }", "label: needs de and en"},
		{"statuslevel", mb, "level: fault }\nmaps", "level: bad }\nmaps", "unknown level"},
		{"maplevel", mb, "level: notice, text", "level: info, text", "unknown level"},
		{"maptext", mb, "de: Wartend", "fr: Attente", "needs de and en"},
		{"mapdefault", mb, "default: { level: fault }", "default: { level: x }", "maps.st.default"},
		{"mapempty", mb, "    bits:\n      0: { level: fault, text: { en: Grid, de: Netz } }", "    default: { level: fault }", "has no entries"},
		{"mbhttp", mb, "netting: app", "netting: app\nhttp: { endpoints: [] }", "not allowed for modbus"},
		{"nomodbus", mb, "modbus: { function: input, byte_order: big, word_order: big }", "", "modbus and read"},
		{"mbpath", mb, "addr: 200, type: u16", "addr: 200, path: \"$.x\", type: u16", "not allowed for modbus"},
		{"order", mb, "byte_order: big", "byte_order: mixed", "byte_order"},
		{"words", mb, "word_order: big", "word_order: mixed", "word_order"},
		{"vendor", mb, "vendor: Test", "vendor: \"\"", "vendor: must not"},
		{"enum", ht, "values: { 0: idle, 1: busy }", "unit: \"\"", "required for enum"},
		{"endpoint", ht, "{ endpoint: live, path: \"$.p\"", "{ endpoint: x, path: \"$.p\"", "unknown endpoint"},
		{"jsonpath", ht, "\"$.p\"", "\"p\"", "not a supported JSONPath"},
		{"jsontype", ht, "type: number, unit: W }", "type: f32, unit: W }", "not a JSON type"},
		{"auth", ht, "auth: digest", "auth: ntlm", "http.auth"},
		{"epid", ht, "{ id: ids, url: \"/ids\"", "{ id: live, url: \"/ids\"", "must be unique"},
		{"epurl", ht, "url: \"/live\"", "url: \"live\"", "must start with /"},
		{"noeps", ht, "    - { id: live, url: \"/live\" }\n    - { id: ids, url: \"/ids\", interval: 1h }", "    []", "required for http"},
		{"httpaddr", ht, "path: \"$.c.p\"", "path: \"$.c.p\", addr: 3", "not allowed for http"},
		{"sumpath", ht, "\"$.e[1]\"", "\"e1\"", "not a supported JSONPath"},
		{"sumendpoint", ht, "{ endpoint: live, sum:", "{ endpoint: x, sum:", "unknown endpoint"},
		{"valueid", ht, "ids: ids", "ids: nope", "value_id.ids"},
		{"category", ht, "category: wallbox", "category: car", "unknown category"},
		{"statustext", ht, "text: { en: Error, de: Fehler }", "text: { en: Error }", "status.errors.text"},
		{"httpmodbus", ht, "max_power: 20000", "max_power: 20000\nread: { mode: direct }", "not allowed for http"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Contains(t, tc.base, tc.old, "test setup")
			_, err := Parse([]byte(strings.Replace(tc.base, tc.old, tc.new, 1)))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestParseDecodeErrors(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"", "schema: 1\nbogus: 1\n", "maps: { m: [1] }", "maps: { m: { zz: {} } }",
		"maps: { m: { 1: { level: [x] } } }", "maps: { m: { default: [x] } }",
		"maps: { m: { bits: [x] } }", "blocks: { b: { channels: { c: { addr: [x] } } } }",
		"blocks: { b: { channels: { c: { addr: x } } } }",
		"http: { endpoints: [ { id: a, url: /a, interval: soon } ] }",
	} {
		_, err := Parse([]byte(in))
		assert.Error(t, err, in)
	}
}

func TestLoader(t *testing.T) {
	t.Parallel()
	mb := readTestdata(t, "modbus.yaml")
	builtin := fstest.MapFS{"test/modbus.yaml": {Data: []byte(mb)}}
	files := map[string]string{
		"/cfg/local.yaml":   strings.Replace(mb, "test/modbus", "test/local", 1),
		"/abs/override.yml": mb,
		"/cfg/bad.yaml":     "schema: 1",
	}
	read := func(p string) ([]byte, error) {
		if s, ok := files[p]; ok {
			return []byte(s), nil
		}
		return nil, os.ErrNotExist
	}
	l, err := NewLoader(builtin, read)
	require.NoError(t, err)
	assert.Equal(t, []string{"test/modbus"}, l.IDs())

	got, err := l.Load("test/modbus", "/cfg")
	require.NoError(t, err)
	assert.Empty(t, got.Path)

	got, err = l.Load("./local.yaml", "/cfg")
	require.NoError(t, err)
	assert.Equal(t, "/cfg/local.yaml", got.Path)
	assert.False(t, got.Overrides)

	got, err = l.Load("/abs/override.yml", "/cfg")
	require.NoError(t, err)
	assert.True(t, got.Overrides)

	_, err = l.Load("test/nope", "/cfg")
	require.ErrorIs(t, err, ErrUnknownProfile)
	_, err = l.Load("bad.yaml", "/cfg")
	require.ErrorContains(t, err, "bad.yaml")
	_, err = l.Load("missing.yaml", "/cfg")
	require.ErrorContains(t, err, "read")
}

func TestLoaderErrors(t *testing.T) {
	t.Parallel()
	mb := readTestdata(t, "modbus.yaml")
	_, err := NewLoader(nil, os.ReadFile)
	require.Error(t, err)
	_, err = NewLoader(fstest.MapFS{"test/other.yaml": {Data: []byte(mb)}}, os.ReadFile)
	require.ErrorContains(t, err, "declares ID")
	_, err = NewLoader(fstest.MapFS{"test/modbus.yaml": {Data: []byte("x: [")}}, os.ReadFile)
	require.Error(t, err)
	_, err = NewLoader(fstest.MapFS{"test/modbus.yaml": {Mode: 0o200}}, os.ReadFile)
	require.Error(t, err)
}

func TestBuiltinProfiles(t *testing.T) {
	t.Parallel()
	l, err := NewLoader(profiles.FS(), os.ReadFile)
	require.NoError(t, err)
	assert.Contains(t, l.IDs(), "solis/s6-hybrid")
	assert.Len(t, l.IDs(), 7)
}

func TestHelpers(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "0x0", uintHex(0))
	assert.Equal(t, "0x1015", uintHex(0x1015))
	assert.True(t, IsPath("a.yml"))
	assert.False(t, IsPath("solis/s6-hybrid"))
	_, _, ok := ChannelRef("nodot")
	assert.False(t, ok)
	assert.Len(t, Categories(), 4)
}
