package profile

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseTestdata(t *testing.T, name string) *Profile {
	t.Helper()
	p, err := Parse([]byte(readTestdata(t, name)))
	require.NoError(t, err)
	return p
}

func TestDecodeModbus(t *testing.T) {
	t.Parallel()
	p := parseTestdata(t, "modbus.yaml")
	words := map[int]uint16{
		100: 0x0001, 101: 0x0002, // u32 = 65538 W
		102: 123,       // 12.3 kWh
		103: 0, 104: 7, // 7 kWh
		105: 2300, 106: 50, // 230 V, 5 A
		107: 0xFFF6, // i16 -10
		108: 1,      // discharging, status code 1 (unknown → default fault)
		109: 0,
	}
	raw := Raw{Words: words}
	pv := p.Channels("pv", raw)
	assert.InDelta(t, 65538, pv["power"].Num, 0)
	assert.InDelta(t, 12300, pv["energy_out"].Num, 1e-9)
	assert.InDelta(t, 7000, pv["total"].Num, 0)
	pv1 := p.Channels("pv1", raw)
	assert.InDelta(t, 1150, pv1["power"].Num, 1e-9)
	assert.NotContains(t, pv1, "energy_out", "derive channels come from the collector")
	bat := p.Channels("battery", raw)
	assert.InDelta(t, 10, bat["power"].Num, 0, "discharging is positive")
	words[108] = 0
	assert.InDelta(t, -10, p.Channels("battery", raw)["power"].Num, 0)

	delete(words, 106)
	assert.False(t, p.Channels("pv1", raw)["power"].Valid, "missing input → missing product")
	delete(words, 108)
	assert.False(t, p.Channels("battery", raw)["power"].Valid, "missing sign → missing")
}

func TestDecodeTypesAndOrder(t *testing.T) {
	t.Parallel()
	p := &Profile{Modbus: &ModbusOptions{ByteOrder: "big", WordOrder: "big"}}
	f32 := math.Float32bits(230.5)
	f64 := math.Float64bits(-1.25)
	words := map[int]uint16{
		0: uint16(f32 >> 16), 1: uint16(f32),
		2: uint16(f64 >> 48), 3: uint16(f64 >> 32), 4: uint16(f64 >> 16), 5: uint16(f64),
		6: 0xFFFF, 7: 0xFFFE, // i32 -2
		8: 0, 9: 0, 10: 0, 11: 9, // u64 9
		12: 'P'<<8 | 'V', 13: 'M' << 8, // "PVM"
		14: 0x0102,
	}
	cases := []struct {
		typ  string
		addr int
		want float64
	}{
		{"f32", 0, 230.5},
		{"f64", 2, -1.25},
		{"i32", 6, -2},
		{"u64", 8, 9},
		{"u16", 14, 0x0102},
		{"bitfield", 14, 0x0102},
		{"i16", 6, -1},
	}
	for _, c := range cases {
		v, err := p.registerValue(Field{Addr: Addrs{c.addr}, Type: c.typ}, words)
		require.NoError(t, err, c.typ)
		assert.InDelta(t, c.want, v.Num, 1e-9, c.typ)
	}
	s, err := p.registerValue(Field{Addr: Addrs{12}, Type: "string", Length: 2}, words)
	require.NoError(t, err)
	assert.Equal(t, "PVM", s.Text)

	p.Modbus = &ModbusOptions{ByteOrder: "little", WordOrder: "little"}
	v, err := p.registerValue(Field{Addr: Addrs{6}, Type: "u32"}, map[int]uint16{6: 0x0100, 7: 0x0200})
	require.NoError(t, err)
	assert.InDelta(t, 0x00020001, v.Num, 0, "low word first, bytes swapped")

	_, err = p.registerValue(Field{Addr: Addrs{99}, Type: "u16"}, words)
	require.ErrorIs(t, err, ErrMissing)
	_, err = p.registerValue(Field{Type: "u16"}, words)
	require.ErrorIs(t, err, ErrMissing)
	_, err = p.registerValue(Field{Addr: Addrs{14, 99}, Type: "bitfield"}, words)
	require.ErrorIs(t, err, ErrMissing)
}

func TestStatusModbus(t *testing.T) {
	t.Parallel()
	p := parseTestdata(t, "modbus.yaml")
	words := map[int]uint16{108: 3, 109: 0, 202: 0, 203: 0}
	st := p.Statuses(Raw{Words: words})
	assert.Equal(t, LevelOK, st["operating"].Level)
	assert.Equal(t, "Generating", st["operating"].Texts[0]["en"])
	assert.Equal(t, LevelOK, st["faults"].Level)
	assert.Equal(t, LevelOK, st["bms"].Level)

	words[108] = 0x77
	words[202] = 1 // mapped bit 0
	words[203] = 1 // bit 16, unmapped → fault
	words[109] = 4
	st = p.Statuses(Raw{Words: words})
	assert.Equal(t, LevelFault, st["operating"].Level, "unknown code uses default")
	assert.Equal(t, "Unknown code 0x0077", st["operating"].Texts[0]["en"])
	assert.Equal(t, LevelFault, st["faults"].Level)
	assert.Len(t, st["faults"].Texts, 2)
	assert.Equal(t, "Unknown bit 16", st["faults"].Texts[1]["en"])
	assert.Equal(t, LevelFault, st["bms"].Level)

	delete(words, 108)
	assert.False(t, p.Statuses(Raw{Words: words})["operating"].Valid)
}

func TestStatusMaps(t *testing.T) {
	t.Parallel()
	p := &Profile{Modbus: &ModbusOptions{}}
	m := Map{Codes: map[uint64]MapEntry{1: {Level: LevelOK}}}
	sv := StatusValue{Code: 5, Level: LevelOK}
	p.applyMap(&sv, Status{}, m)
	assert.Equal(t, LevelNotice, sv.Level, "unknown code without default")
	sv = StatusValue{Code: 0b10, Level: LevelOK}
	applyBits(&sv, Map{Default: &MapEntry{Level: LevelNotice}})
	assert.Equal(t, LevelNotice, sv.Level)
	assert.Equal(t, 0, levelRank("x"))
}

func httpRaw(t *testing.T, bodies map[string]string) Raw {
	t.Helper()
	s := Sample{JSON: map[string]json.RawMessage{}}
	for k, v := range bodies {
		s.JSON[k] = json.RawMessage(v)
	}
	raw, err := s.Raw()
	require.NoError(t, err)
	return raw
}

func TestDecodeHTTP(t *testing.T) {
	t.Parallel()
	p := parseTestdata(t, "http.yaml")
	raw := httpRaw(t, map[string]string{
		"live": `{"p": -1500.5, "e": [100, 250], "s": 1, "c": {"p": 2.5},
			"errors": [], "v": true, "code": 0, "x": [1, 209], "vals": [3, 4]}`,
		"ids": `[74, 209]`,
	})
	raw.JSON["live"].(map[string]any)["vals"] = []any{float64(1), float64(2.5)}
	tot := p.Channels("total", raw)
	assert.InDelta(t, -1500.5, tot["power"].Num, 0)
	assert.InDelta(t, 350, tot["energy_in"].Num, 0)
	assert.Equal(t, "busy", tot["state"].Text)
	assert.False(t, tot["energy_out"].Valid, "value_id: values endpoint is not an array")
	assert.InDelta(t, 2500, p.Channels("charger", raw)["power"].Num, 0, "kW → W")

	st := p.Statuses(raw)
	assert.Equal(t, LevelOK, st["errors"].Level)
	assert.Equal(t, LevelOK, st["valid"].Level)
	assert.Equal(t, LevelOK, st["code"].Level)

	raw = httpRaw(t, map[string]string{
		"live": `{"errors": ["overheat"], "v": false, "code": 3, "e": [1, "x"]}`,
	})
	st = p.Statuses(raw)
	assert.Equal(t, LevelFault, st["errors"].Level)
	assert.Equal(t, "Error", st["errors"].Texts[0]["en"])
	assert.Equal(t, LevelFault, st["valid"].Level)
	assert.Equal(t, LevelNotice, st["code"].Level)
	tot = p.Channels("total", raw)
	assert.False(t, tot["power"].Valid)
	assert.False(t, tot["energy_in"].Valid, "non-numeric sum part")
	assert.False(t, tot["state"].Valid)
}

func TestValueByID(t *testing.T) {
	t.Parallel()
	ids := []any{float64(74), float64(209)}
	v, err := valueByID(ids, []any{float64(1), float64(12.5)}, 209)
	require.NoError(t, err)
	assert.InDelta(t, 12.5, v.Num, 0)
	_, err = valueByID(ids, []any{float64(1)}, 209)
	require.ErrorIs(t, err, ErrMissing, "length mismatch")
	_, err = valueByID(ids, []any{float64(1), "x"}, 209)
	require.Error(t, err)
	_, err = valueByID(ids, []any{float64(1), float64(2)}, 1)
	require.ErrorIs(t, err, ErrMissing)
}

func TestJSONHelpers(t *testing.T) {
	t.Parallel()
	doc := map[string]any{"a": map[string]any{"b": []any{float64(1), map[string]any{"c": "2"}}}}
	v, ok := Lookup(doc, "$.a.b[1].c")
	require.True(t, ok)
	assert.Equal(t, "2", v)
	for _, path := range []string{"$.a.x", "$.a.b[5]", "$.a[0]", "$.a.b.c", "bad"} {
		_, ok := Lookup(doc, path)
		assert.False(t, ok, path)
	}
	for in, want := range map[any]float64{true: 1, false: 0, "2.5": 2.5, float64(3): 3} {
		n, err := number(in)
		require.NoError(t, err)
		assert.InDelta(t, want, n, 0)
	}
	_, err := number("x")
	require.Error(t, err)
	_, err = number(nil)
	require.Error(t, err)
	empty, err := jsonValue(map[string]any{}, Field{Path: "$.errors", Type: "list"})
	require.NoError(t, err)
	assert.True(t, empty.Valid, "a missing list is empty")
	_, err = jsonValue([]any{}, Field{Path: "$[0]"})
	require.ErrorIs(t, err, ErrMissing)
	_, err = jsonValue(map[string]any{"l": 1.0}, Field{Path: "$.l", Type: "list"})
	require.Error(t, err)
	s, err := jsonValue(map[string]any{"s": 12.0}, Field{Path: "$.s", Type: "string"})
	require.NoError(t, err)
	assert.Equal(t, "12", s.Text)
	_, err = Sample{JSON: map[string]json.RawMessage{"x": json.RawMessage("{")}}.Raw()
	require.Error(t, err)
}

func TestRequests(t *testing.T) {
	t.Parallel()
	p := parseTestdata(t, "modbus.yaml")
	assert.Equal(t, []Request{{100, 10}, {200, 7}}, p.Requests(ReadRange, nil), "dumps")
	direct := p.Requests(ReadDirect, nil)
	assert.Contains(t, direct, Request{100, 2})
	assert.Contains(t, direct, Request{204, 3})
	assert.Len(t, direct, 12)
	h := parseTestdata(t, "http.yaml")
	assert.Nil(t, h.Requests(ReadRange, nil))
	eps := h.Endpoints(nil, func(Endpoint) bool { return false })
	require.Len(t, eps, 1)
	assert.Equal(t, "live", eps[0].ID)
	assert.Len(t, h.Endpoints(nil, func(e Endpoint) bool { return e.Interval == Interval(time.Hour) }), 2)
	assert.Nil(t, p.Endpoints(nil, nil))
}

func TestRequestsOnlyWhatIsNeeded(t *testing.T) {
	t.Parallel()
	p := parseTestdata(t, "modbus.yaml")
	// battery: power 107 with its sign 108, soc 109; status 108, 202–203, 109. No pv
	// registers.
	n := p.NeedsFor([]string{"battery"})
	assert.Equal(t, []Request{{107, 3}, {202, 2}}, p.Requests(ReadRange, n))
	assert.Equal(t, []Request{{107, 1}, {108, 1}, {109, 1}, {202, 1}, {203, 1}},
		p.Requests(ReadDirect, n))
}

func TestNeedsFollowDependencies(t *testing.T) {
	t.Parallel()
	p := parseTestdata(t, "modbus.yaml")
	n := p.NeedsFor([]string{"pv1"})
	assert.True(t, n.Has("pv1", "power"))
	assert.True(t, n.Has("pv1", "voltage"), "product input")
	assert.True(t, n.Has("pv", "energy_out"), "shared counter")
	assert.False(t, n.Has("pv", "power"))
	assert.False(t, n.Has("pv", "total"))
	assert.False(t, n.Has("battery", "power"))
	assert.Equal(t, []string{"pv", "pv1"}, n.Blocks())
	assert.True(t, Needs(nil).Has("x", "y"), "nil needs everything")
	assert.Equal(t, [][2]string{{"pv1", "power"}}, p.Sharers("pv.energy_out"))
	// pv.energy_out 102, pv1 105–106; status 108, 109, 202–203: small gaps are read along.
	assert.Equal(t, []Request{{102, 8}, {202, 2}}, p.Requests(ReadRange, n))
}

func TestSpansSplitLargeGaps(t *testing.T) {
	t.Parallel()
	used := map[int]bool{10: true, 11: true, 11 + mergeGap: true, 40 + mergeGap: true}
	assert.Equal(t, []Request{{10, mergeGap + 2}, {40 + mergeGap, 1}}, spans(used, 0, 100))
	assert.Empty(t, spans(used, 200, 300))
}
