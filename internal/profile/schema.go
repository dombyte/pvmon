// Package profile holds the declarative device profiles (spec 4): schema, loader for
// built-in and local profiles, validation, decoder and status maps. Everything
// vendor-specific lives in the YAML; this package only interprets it.
package profile

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Profile is one parsed device profile (spec 4.2).
type Profile struct {
	Schema    int               `yaml:"schema"`
	ID        string            `yaml:"profile"`
	Version   int               `yaml:"version"`
	Vendor    string            `yaml:"vendor"`
	Models    []string          `yaml:"models"`
	Transport string            `yaml:"transport"`
	Verified  bool              `yaml:"verified"`
	Modbus    *ModbusOptions    `yaml:"modbus"`
	Read      *ReadOptions      `yaml:"read"`
	HTTP      *HTTPOptions      `yaml:"http"`
	MaxPower  float64           `yaml:"max_power"`
	Netting   string            `yaml:"netting"`
	Blocks    map[string]Block  `yaml:"blocks"`
	Status    map[string]Status `yaml:"status"`
	Maps      map[string]Map    `yaml:"maps"`
}

// Transports.
const (
	TransportModbus = "modbus"
	TransportHTTP   = "http"
)

// Read modes (spec 4.3).
const (
	ReadRange  = "range"
	ReadDirect = "direct"
)

// DefaultMaxRegisters is the Modbus limit per request unless the profile lowers it.
const DefaultMaxRegisters = 125

// ModbusOptions selects function code and byte/word order.
type ModbusOptions struct {
	Function  string `yaml:"function"`   // input (04) | holding (03)
	ByteOrder string `yaml:"byte_order"` // big | little
	WordOrder string `yaml:"word_order"` // big | little
}

// ReadOptions describes how registers are fetched.
type ReadOptions struct {
	Mode         string   `yaml:"mode"`
	MaxRegisters int      `yaml:"max_registers"`
	Ranges       [][2]int `yaml:"ranges"`
}

// Limit returns the effective max registers per request.
func (r *ReadOptions) Limit() int {
	if r == nil || r.MaxRegisters == 0 {
		return DefaultMaxRegisters
	}
	return r.MaxRegisters
}

// HTTPOptions lists the endpoints of an HTTP device.
type HTTPOptions struct {
	Auth      string     `yaml:"auth"` // "" | basic | digest
	Endpoints []Endpoint `yaml:"endpoints"`
}

// Endpoint is one GET URL; Interval > 0 polls it less often than the device.
type Endpoint struct {
	ID       string   `yaml:"id"`
	URL      string   `yaml:"url"`
	Interval Interval `yaml:"interval"`
}

// Interval is a Go duration string in a profile ("1h").
type Interval time.Duration

// UnmarshalYAML parses a duration string.
func (i *Interval) UnmarshalYAML(n *yaml.Node) error {
	d, err := time.ParseDuration(n.Value)
	if err != nil {
		return fmt.Errorf("line %d: interval %q: %w", n.Line, n.Value, err)
	}
	*i = Interval(d)
	return nil
}

// Field locates one raw value: a Modbus address or a JSON path, plus its type.
type Field struct {
	Endpoint string `yaml:"endpoint"`
	Path     string `yaml:"path"`
	Addr     Addrs  `yaml:"addr"`
	Type     string `yaml:"type"`
	Length   int    `yaml:"length"` // registers of a string
}

// Addrs is one register address or a list of them (status bitfields over several words).
type Addrs []int

// UnmarshalYAML accepts a scalar or a sequence of addresses.
func (a *Addrs) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.SequenceNode {
		var list []int
		if err := n.Decode(&list); err != nil {
			return fmt.Errorf("decode addresses: %w", err)
		}
		*a = list
		return nil
	}
	var v int
	if err := n.Decode(&v); err != nil {
		return fmt.Errorf("decode address: %w", err)
	}
	*a = Addrs{v}
	return nil
}

// Roles of blocks (spec 2 and the follow-up table of 4.2).
const (
	RolePV       = "pv"
	RoleGrid     = "grid"
	RoleBattery  = "battery"
	RoleConsumer = "consumer"
	RoleBackup   = "backup"
	RoleInverter = "inverter"
	RolePhase    = "phase"
)

// Block is one meter the device can provide.
type Block struct {
	Role         string             `yaml:"role"`
	RolesAllowed []string           `yaml:"roles_allowed"`
	Category     string             `yaml:"category"`
	Channels     map[string]Channel `yaml:"channels"`
}

// Allows reports whether the block may be used with role.
func (b Block) Allows(role string) bool {
	if role == b.Role {
		return true
	}
	for _, r := range b.RolesAllowed {
		if r == role {
			return true
		}
	}
	return false
}

// Counter kinds (spec 5.3).
const (
	CounterNone       = ""
	CounterLifetime   = "lifetime"
	CounterDailyReset = "daily_reset"
)

// Channel is one normalised quantity of a block.
type Channel struct {
	Field       `yaml:",inline"`
	Sum         []string       `yaml:"sum"`
	Product     []string       `yaml:"product"`
	ValueID     *ValueID       `yaml:"value_id"`
	Derive      string         `yaml:"derive"`
	Of          string         `yaml:"of"`
	By          string         `yaml:"by"`
	Scale       *float64       `yaml:"scale"`
	Offset      float64        `yaml:"offset"`
	Invert      bool           `yaml:"invert"`
	SignFrom    *SignFrom      `yaml:"sign_from"`
	Unit        string         `yaml:"unit"`
	Counter     string         `yaml:"counter"`
	ResetGroup  string         `yaml:"reset_group"`
	Resolution  float64        `yaml:"resolution"`
	History     *bool          `yaml:"history"`
	Values      map[int]string `yaml:"values"` // type enum: code → state name
	RenamedFrom string         `yaml:"renamed_from"`
}

// ScaleOr1 returns the scale, 1 when unset.
func (c Channel) ScaleOr1() float64 {
	if c.Scale == nil {
		return 1
	}
	return *c.Scale
}

// IsEnergy reports whether the channel is an energy counter, an integrated energy channel
// or a share of one.
func (c Channel) IsEnergy() bool {
	return c.Counter != CounterNone || energyUnits()[c.Unit] > 0 || c.Derive != ""
}

// Historised reports whether the channel is stored by default (spec 7.2): power, soc and
// energy channels unless history: false; others only with history: true.
func (c Channel) Historised(name string) bool {
	if c.History != nil {
		return *c.History
	}
	return name == "power" || name == "soc" || c.IsEnergy()
}

// ValueID looks a value up by ID in a parallel ID array (WARP).
type ValueID struct {
	IDs string `yaml:"ids"` // endpoint returning the ID array
	ID  int    `yaml:"id"`
}

// SignFrom gives a magnitude its sign from a direction register or path.
type SignFrom struct {
	Field         `yaml:",inline"`
	DischargeWhen int `yaml:"discharge_when"`
}

// Status levels.
const (
	LevelOK     = "ok"
	LevelNotice = "notice"
	LevelFault  = "fault"
)

// Status is one status register or JSON value mapped to ok / notice / fault.
type Status struct {
	Field    `yaml:",inline"`
	Map      string `yaml:"map"`
	Level    string `yaml:"level"`    // bitfield without map: any set bit has this level
	NonEmpty string `yaml:"nonempty"` // type list: non-empty → level
	False    string `yaml:"false"`    // type bool: false → level
	NonZero  string `yaml:"nonzero"`  // any number: non-zero → level
	Text     Text   `yaml:"text"`
	Label    Text   `yaml:"label"` // display name on the device page; default the key
}

// Text is a translated text keyed by language.
type Text map[string]string

// Languages every text must provide.
func Languages() []string { return []string{"de", "en"} }

// MapEntry is one code or bit of a status map.
type MapEntry struct {
	Level string `yaml:"level"`
	Text  Text   `yaml:"text"`
}

// Map is a status map: codes (or bits for bitfields) plus an optional default.
type Map struct {
	Default *MapEntry
	Codes   map[uint64]MapEntry
	Bits    map[int]MapEntry
}

// UnmarshalYAML reads "default", "bits" and numeric code keys (decimal or 0x hex).
func (m *Map) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: map must be a mapping", n.Line)
	}
	m.Codes = map[uint64]MapEntry{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if err := m.entry(n.Content[i], n.Content[i+1]); err != nil {
			return err
		}
	}
	return nil
}

func (m *Map) entry(key, val *yaml.Node) error {
	switch key.Value {
	case "default":
		m.Default = &MapEntry{}
		return decodeStrict(val, m.Default)
	case "bits":
		m.Bits = map[int]MapEntry{}
		return decodeStrict(val, &m.Bits)
	}
	code, err := strconv.ParseUint(strings.ToLower(key.Value), 0, 64)
	if err != nil {
		return fmt.Errorf("line %d: map key %q is not a code", key.Line, key.Value)
	}
	var e MapEntry
	if err := decodeStrict(val, &e); err != nil {
		return err
	}
	m.Codes[code] = e
	return nil
}

func decodeStrict(n *yaml.Node, v any) error {
	if err := n.Decode(v); err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	return nil
}

const (
	whPerKWh = 1000
	whPerMWh = 1000 * whPerKWh
	wPerKW   = 1000
)

// energyUnits maps energy units to their factor in Wh.
// NormalUnit is the unit of a channel after decoding: energy in Wh, power in W.
func NormalUnit(u string) string {
	if energyUnits()[u] > 0 {
		return "Wh"
	}
	if powerUnits()[u] > 0 {
		return "W"
	}
	return u
}

func energyUnits() map[string]float64 {
	return map[string]float64{"Wh": 1, "kWh": whPerKWh, "MWh": whPerMWh}
}

// powerUnits maps power units to their factor in W.
func powerUnits() map[string]float64 {
	return map[string]float64{"W": 1, "kW": wPerKW}
}
