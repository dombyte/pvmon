package profile

import (
	"fmt"
	"regexp"
	"strings"
)

// fieldRef is one raw value location with its problem path.
type fieldRef struct {
	path  string
	field Field
}

// fields returns every raw location of the profile: channels, sign sources and status.
// Channels without addr/path (sum, product, …) are skipped.
func (p *Profile) fields() []fieldRef {
	var out fieldRefs
	for _, bn := range sortedKeys(p.Blocks) {
		for _, cn := range sortedKeys(p.Blocks[bn].Channels) {
			c := p.Blocks[bn].Channels[cn]
			path := "blocks." + bn + ".channels." + cn
			out.add(path, &c.Field)
			if c.SignFrom != nil {
				out.add(path+".sign_from", &c.SignFrom.Field)
			}
		}
	}
	for _, sn := range sortedKeys(p.Status) {
		s := p.Status[sn]
		out.add("status."+sn, &s.Field)
	}
	return out
}

type fieldRefs []fieldRef

// add records f when it locates a raw value (nil and composite channels are skipped).
func (r *fieldRefs) add(path string, f *Field) {
	if f != nil && (len(f.Addr) > 0 || f.Path != "") {
		*r = append(*r, fieldRef{path, *f})
	}
}

// Modbus register types and their width in registers (string: Field.Length).
func registerWidths() map[string]int {
	const one, two, four = 1, 2, 4
	return map[string]int{
		"u16": one, "i16": one, "bitfield": one, "u32": two, "i32": two, "f32": two,
		"u64": four, "f64": four, "string": 0,
	}
}

// Width returns the number of registers a Modbus field spans per address.
func (f Field) Width() int {
	if f.Type == "string" {
		return f.Length
	}
	return registerWidths()[f.Type]
}

func validateModbus(p *Profile, ps *problems) {
	ps.addf(p.HTTP != nil, "http", "not allowed for modbus profiles")
	if p.Modbus == nil || p.Read == nil {
		ps.addf(true, "modbus", "modbus and read sections are required")
		return
	}
	m := p.Modbus
	ps.addf(!in(m.Function, "input", "holding"), "modbus.function", "must be input or holding")
	ps.addf(!in(m.ByteOrder, "", "big", "little"), "modbus.byte_order", "must be big or little")
	ps.addf(!in(m.WordOrder, "", "big", "little"), "modbus.word_order", "must be big or little")
	validateRanges(p.Read, ps)
	used := map[int]fieldRef{}
	for _, f := range p.fields() {
		validateRegisterField(p.Read, f, used, ps)
	}
}

func validateRanges(r *ReadOptions, ps *problems) {
	ps.addf(!in(r.Mode, ReadRange, ReadDirect), "read.mode", "must be range or direct")
	ps.addf(r.MaxRegisters < 0 || r.MaxRegisters > DefaultMaxRegisters, "read.max_registers",
		"must be 1..%d", DefaultMaxRegisters)
	ps.addf(r.Mode == ReadRange && len(r.Ranges) == 0, "read.ranges", "required for range mode")
	for i, rg := range r.Ranges {
		n := rg[1] - rg[0] + 1
		ps.addf(n < 1 || n > r.Limit(), fmt.Sprintf("read.ranges.%d", i),
			"%d registers, must be 1..%d", n, r.Limit())
	}
}

func validateRegisterField(r *ReadOptions, f fieldRef, used map[int]fieldRef, ps *problems) {
	w, ok := registerWidths()[f.field.Type]
	ps.addf(!ok, f.path+".type", "%q is not a Modbus type", f.field.Type)
	ps.addf(len(f.field.Addr) == 0, f.path+".addr", "required for modbus")
	ps.addf(f.field.Path != "", f.path+".path", "not allowed for modbus")
	if f.field.Type == "string" {
		w = f.field.Length
		ps.addf(w < 1, f.path+".length", "required for strings")
	}
	for _, a := range f.field.Addr {
		ps.addf(r.Mode == ReadRange && !insideRange(r.Ranges, a, w), f.path+".addr",
			"%d..%d not inside a read range", a, a+w-1)
		checkCollision(f, a, w, used, ps)
	}
}

func insideRange(ranges [][2]int, addr, width int) bool {
	for _, rg := range ranges {
		if addr >= rg[0] && addr+width-1 <= rg[1] {
			return true
		}
	}
	return false
}

// checkCollision reports registers read by two fields with different start or width.
func checkCollision(f fieldRef, addr, width int, used map[int]fieldRef, ps *problems) {
	for reg := addr; reg < addr+max(width, 1); reg++ {
		prev, ok := used[reg]
		if !ok {
			used[reg] = fieldRef{path: f.path, field: Field{
				Addr: Addrs{addr}, Type: f.field.Type, Length: f.field.Length,
			}}
			continue
		}
		// The same registers may be read as different types (code and bitfield), but a
		// value starting inside another or spanning a different width is a collision.
		same := prev.field.Addr[0] == addr && prev.field.Width() == width
		ps.addf(!same, f.path+".addr", "register %d collides with %s", reg, prev.path)
		return
	}
}

var jsonPath = regexp.MustCompile(`^\$(\.[A-Za-z_][A-Za-z0-9_]*|\[\d+\])*$`)

// ValidPath reports whether s is in the supported JSONPath subset ($.a.b[0].c).
func ValidPath(s string) bool { return jsonPath.MatchString(s) }

func httpTypes() []string {
	return []string{"number", "bool", "string", "list", "enum", "u16", "i16", "u32", "i32"}
}

func validateHTTP(p *Profile, ps *problems) {
	ps.addf(p.Modbus != nil || p.Read != nil, "modbus", "not allowed for http profiles")
	if p.HTTP == nil || len(p.HTTP.Endpoints) == 0 {
		ps.addf(true, "http.endpoints", "required for http profiles")
		return
	}
	ps.addf(!in(p.HTTP.Auth, "", "basic", "digest"), "http.auth", "must be basic or digest")
	seen := validateEndpoints(p.HTTP.Endpoints, ps)
	for _, f := range p.fields() {
		validateJSONField(f, seen, ps)
	}
}

func validateEndpoints(eps []Endpoint, ps *problems) map[string]bool {
	seen := map[string]bool{}
	for i, e := range eps {
		path := fmt.Sprintf("http.endpoints.%d", i)
		ps.addf(e.ID == "" || seen[e.ID], path+".id", "must be unique and not empty")
		ps.addf(!strings.HasPrefix(e.URL, "/"), path+".url", "must start with /")
		seen[e.ID] = true
	}
	return seen
}

func validateJSONField(f fieldRef, endpoints map[string]bool, ps *problems) {
	ps.addf(!endpoints[f.field.Endpoint], f.path+".endpoint",
		"unknown endpoint %q", f.field.Endpoint)
	ps.addf(len(f.field.Addr) > 0, f.path+".addr", "not allowed for http")
	ps.addf(!ValidPath(f.field.Path), f.path+".path", "%q is not a supported JSONPath",
		f.field.Path)
	ps.addf(f.field.Type != "" && !in(f.field.Type, httpTypes()...), f.path+".type",
		"%q is not a JSON type", f.field.Type)
}
