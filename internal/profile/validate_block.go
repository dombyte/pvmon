package profile

import (
	"regexp"
	"strconv"
	"strings"
)

const hexBase = 16

// Roles returns every block role.
func Roles() []string {
	return []string{
		RolePV, RoleGrid, RoleBattery, RoleConsumer, RoleBackup, RoleInverter, RolePhase,
	}
}

// Categories returns the consumer categories (spec 2).
func Categories() []string { return []string{"wallbox", "heatpump", "heater", "other"} }

func validateBlock(p *Profile, name string, b Block, ps *problems) {
	path := "blocks." + name
	ps.addf(!in(b.Role, Roles()...), path+".role", "unknown role %q", b.Role)
	for _, r := range b.RolesAllowed {
		ps.addf(!in(r, Roles()...), path+".roles_allowed", "unknown role %q", r)
	}
	ps.addf(b.Category != "" && !in(b.Category, Categories()...), path+".category",
		"unknown category %q", b.Category)
	ps.addf(len(b.Channels) == 0, path+".channels", "must not be empty")
	for _, cn := range sortedKeys(b.Channels) {
		validateChannel(p, name, cn, b.Channels[cn], ps)
	}
}

// sources counts how a channel gets its value; exactly one is allowed.
func (c Channel) sources() int {
	n := 0
	for _, set := range []bool{
		len(c.Addr) > 0 || c.Path != "", len(c.Sum) > 0, len(c.Product) > 0,
		c.ValueID != nil, c.Derive != "",
	} {
		if set {
			n++
		}
	}
	return n
}

func validateChannel(p *Profile, block, name string, c Channel, ps *problems) {
	path := "blocks." + block + ".channels." + name
	ps.addf(c.sources() != 1, path,
		"needs exactly one of addr/path, sum, product, value_id, derive")
	validateUnits(path, name, c, ps)
	validateProduct(p, path, c, ps)
	validateSum(p, path, c, ps)
	validateValueID(p, path, c, ps)
	validateDerive(p, block, path, c, ps)
	ps.addf(c.SignFrom != nil && len(c.SignFrom.Addr) == 0 && c.SignFrom.Path == "",
		path+".sign_from", "needs addr or path")
}

func validateCounter(path string, c Channel, ps *problems) {
	ps.addf(!in(c.Counter, CounterNone, CounterLifetime, CounterDailyReset), path+".counter",
		"must be lifetime or daily_reset")
	ps.addf(c.Counter == CounterDailyReset && c.ResetGroup == "", path+".reset_group",
		"required for daily_reset")
	ps.addf(c.Counter != CounterNone && energyUnits()[c.Unit] == 0, path+".unit",
		"counters need an energy unit (Wh, kWh, MWh)")
}

func validateUnits(path, name string, c Channel, ps *problems) {
	validateCounter(path, c, ps)
	ps.addf(strings.HasPrefix(name, "energy_") && energyUnits()[c.Unit] == 0 &&
		c.Derive == "", path+".unit", "energy channels need an energy unit")
	ps.addf(name == "power" && c.Unit != "" && powerUnits()[c.Unit] == 0, path+".unit",
		"power needs W or kW")
	ps.addf(c.Scale != nil && *c.Scale == 0, path+".scale", "must not be 0")
	ps.addf(c.Resolution < 0, path+".resolution", "must be >= 0")
	ps.addf(c.Type == "enum" && len(c.Values) == 0, path+".values", "required for enum")
}

func validateProduct(p *Profile, path string, c Channel, ps *problems) {
	ps.addf(len(c.Product) != 0 && len(c.Product) != 2, path+".product", "needs two channels")
	for _, ref := range c.Product {
		_, ok := p.channel(ref)
		ps.addf(!ok, path+".product", "unknown channel %q", ref)
	}
}

func validateSum(p *Profile, path string, c Channel, ps *problems) {
	for _, sp := range c.Sum {
		ps.addf(p.Transport == TransportHTTP && !ValidPath(sp), path+".sum",
			"%q is not a supported JSONPath", sp)
	}
	ps.addf(len(c.Sum) > 0 && p.Transport == TransportHTTP && !hasEndpoint(p, c.Endpoint),
		path+".endpoint", "unknown endpoint %q", c.Endpoint)
}

func validateValueID(p *Profile, path string, c Channel, ps *problems) {
	if c.ValueID != nil {
		ps.addf(!hasEndpoint(p, c.ValueID.IDs), path+".value_id.ids", "unknown endpoint %q",
			c.ValueID.IDs)
		ps.addf(!hasEndpoint(p, c.Endpoint), path+".endpoint", "unknown endpoint %q", c.Endpoint)
	}
}

func validateDerive(p *Profile, block, path string, c Channel, ps *problems) {
	if c.Derive != "" {
		of, ok := p.channel(c.Of)
		ps.addf(c.Derive != "share", path+".derive", "only share is supported")
		ps.addf(!ok || !of.IsEnergy(), path+".of", "%q is not an energy channel", c.Of)
		_, byOK := p.Blocks[block].Channels[c.By]
		ps.addf(!byOK, path+".by", "unknown channel %q in this block", c.By)
	}
}

func hasEndpoint(p *Profile, id string) bool {
	if p.HTTP == nil {
		return false
	}
	for _, e := range p.HTTP.Endpoints {
		if e.ID == id {
			return true
		}
	}
	return false
}

func validateStatus(p *Profile, ps *problems) {
	for _, name := range sortedKeys(p.Status) {
		validateOneStatus(p, "status."+name, p.Status[name], ps)
	}
}

func (s Status) levels() []string { return []string{s.Level, s.NonEmpty, s.False, s.NonZero} }

// kinds counts how the status is mapped; exactly one way is allowed.
func (s Status) kinds() int {
	n := 0
	if s.Map != "" {
		n++
	}
	for _, l := range s.levels() {
		if l != "" {
			n++
		}
	}
	return n
}

// statusName is the pattern of status keys; it keeps them apart from reserved registers
// such as "@connection".
var statusName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func validateOneStatus(p *Profile, path string, s Status, ps *problems) {
	ps.addf(!statusName.MatchString(strings.TrimPrefix(path, "status.")), path,
		"name must match %s", statusName)
	ps.addf(s.Label != nil && !complete(s.Label), path+".label",
		"needs %s", strings.Join(Languages(), " and "))
	ps.addf(s.kinds() != 1, path, "needs exactly one of map, level, nonempty, false, nonzero")
	_, mapOK := p.Maps[s.Map]
	ps.addf(s.Map != "" && !mapOK, path+".map", "unknown map %q", s.Map)
	for _, lvl := range s.levels() {
		ps.addf(lvl != "" && !in(lvl, LevelOK, LevelNotice, LevelFault), path,
			"unknown level %q", lvl)
	}
	ps.addf(s.Map == "" && s.Level == "" && !complete(s.Text), path+".text",
		"needs %s", strings.Join(Languages(), " and "))
}

func complete(t Text) bool {
	for _, l := range Languages() {
		if t[l] == "" {
			return false
		}
	}
	return true
}

func validateMaps(p *Profile, ps *problems) {
	for _, name := range sortedKeys(p.Maps) {
		m := p.Maps[name]
		path := "maps." + name
		if m.Default != nil {
			ps.addf(!in(m.Default.Level, LevelOK, LevelNotice, LevelFault), path+".default",
				"unknown level %q", m.Default.Level)
		}
		for code, e := range m.Codes {
			validateEntry(path, code, e, ps)
		}
		for bit, e := range m.Bits {
			validateEntry(path+".bits", uint64(bit), e, ps)
		}
		ps.addf(len(m.Codes) == 0 && len(m.Bits) == 0, path, "has no entries")
	}
}

func validateEntry(path string, code uint64, e MapEntry, ps *problems) {
	p := path + "." + strings.ToLower(uintHex(code))
	ps.addf(!in(e.Level, LevelOK, LevelNotice, LevelFault), p, "unknown level %q", e.Level)
	ps.addf(!complete(e.Text), p+".text", "needs %s", strings.Join(Languages(), " and "))
}

func uintHex(v uint64) string { return "0x" + strconv.FormatUint(v, hexBase) }
