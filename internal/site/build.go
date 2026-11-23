package site

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"github.com/dombyte/pvmon/internal/profile"
)

// Problem is one domain validation finding with its config path.
type Problem struct {
	Path    string
	Message string
}

// Error lists every problem found while building the site.
type Error struct {
	Problems []Problem
}

func (e *Error) Error() string {
	parts := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		parts[i] = p.Path + ": " + p.Message
	}
	return "site: " + strings.Join(parts, "; ")
}

type builder struct {
	spec  Spec
	site  *Site
	probs []Problem
}

func (b *builder) addf(bad bool, path, format string, args ...any) {
	if bad {
		b.probs = append(b.probs, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
	}
}

// Build validates the spec against the profiles and returns the effective site. All
// problems are reported at once as *Error.
func Build(spec Spec) (*Site, error) {
	b := &builder{spec: spec, site: &Site{
		Name: spec.Name, Connections: map[string]*Connection{},
		Devices: map[string]*Device{}, Meters: map[string]*Meter{},
	}}
	for _, id := range sorted(spec.Connections) {
		b.addConnection("connections."+id, spec.Connections[id])
	}
	for _, id := range sorted(spec.Devices) {
		b.addDevice(id, spec.Devices[id])
	}
	for _, id := range sorted(spec.Meters) {
		b.addMeter(id, spec.Meters[id])
	}
	b.checkHierarchy()
	b.checkGrid()
	b.link()
	if len(b.probs) > 0 {
		sort.SliceStable(b.probs, func(i, j int) bool { return b.probs[i].Path < b.probs[j].Path })
		return nil, &Error{Problems: b.probs}
	}
	return b.site, nil
}

func sorted[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Default ports per scheme.
const (
	modbusTCPPort = "502"
	maxModbusUnit = 247
)

// ParseAddress normalises a connection address and returns its key and scheme.
func ParseAddress(addr string) (key, scheme string, err error) {
	u, err := url.Parse(addr)
	if err != nil {
		return "", "", fmt.Errorf("invalid address %q: %w", addr, err)
	}
	parse, ok := addressParsers()[u.Scheme]
	if !ok {
		return "", "", fmt.Errorf("address %q: scheme must be tcp, rtu, http or https", addr)
	}
	key, err = parse(u)
	if err != nil {
		return "", "", fmt.Errorf("address %q: %w", addr, err)
	}
	return key, u.Scheme, nil
}

func addressParsers() map[string]func(*url.URL) (string, error) {
	httpParser := func(u *url.URL) (string, error) {
		if u.Host == "" {
			return "", errors.New("has no host")
		}
		return u.Scheme + "://" + u.Host, nil
	}
	return map[string]func(*url.URL) (string, error){
		"tcp": func(u *url.URL) (string, error) {
			host, port := u.Hostname(), u.Port()
			if port == "" {
				port = modbusTCPPort
			}
			if host == "" || u.Path != "" {
				return "", errors.New("must be tcp://host[:port]")
			}
			return "tcp://" + net.JoinHostPort(host, port), nil
		},
		"rtu": func(u *url.URL) (string, error) {
			if u.Host != "" || u.Path == "" {
				return "", errors.New("must be rtu:///dev/<port>")
			}
			return "rtu://" + u.Path, nil
		},
		"http":  httpParser,
		"https": httpParser,
	}
}

// addConnection registers a connection and returns its key ("" on error).
func (b *builder) addConnection(path string, c ConnectionSpec) string {
	key, scheme, err := ParseAddress(c.Address)
	if err != nil {
		b.addf(true, path+".address", "%v", err)
		return ""
	}
	if prev, ok := b.site.Connections[key]; ok {
		b.addf(scheme == "rtu" && prev.Serial != c.Serial, path,
			"serial parameters conflict with another connection to %s", key)
		return key
	}
	b.site.Connections[key] = &Connection{
		Address: key, Scheme: scheme, Timeout: c.Timeout, Serial: c.Serial,
	}
	return key
}

func (b *builder) connectionFor(id string, d DeviceSpec) string {
	path := "devices." + id
	if d.Connection == "" {
		return b.addConnection(path+".address", ConnectionSpec{
			Address: d.Address, Timeout: d.Timeout, Serial: defaultSerial(),
		})
	}
	c, ok := b.spec.Connections[d.Connection]
	b.addf(!ok, path+".connection", "unknown connection %q", d.Connection)
	if !ok {
		return ""
	}
	key, _, err := ParseAddress(c.Address)
	if err != nil {
		return ""
	}
	return key
}

func defaultSerial() Serial {
	const speed, bits = 9600, 8
	return Serial{Speed: speed, DataBits: bits, Parity: "N", StopBits: 1}
}

func (b *builder) addDevice(id string, d DeviceSpec) {
	path := "devices." + id
	key := b.connectionFor(id, d)
	if d.Profile == nil {
		b.addf(true, path+".profile", "no profile")
		return
	}
	if key == "" {
		return
	}
	conn := b.site.Connections[key]
	dev := &Device{
		ID: id, Connection: key, Unit: d.Unit, Profile: d.Profile, Interval: d.Interval,
		Timeout: d.Timeout, Rollover: d.Rollover, History: d.History, User: d.User,
		Password: d.Password, Insecure: d.Insecure,
	}
	if dev.Timeout <= 0 {
		dev.Timeout = conn.Timeout
	}
	b.checkTransport(path, conn.Scheme, d)
	dev.Read = b.readMode(path, d)
	dev.Netting = netting(d)
	b.addf(d.Rollover != "" && !hasDailyReset(d.Profile.Profile), path+".rollover",
		"profile %s has no daily_reset counter", d.Profile.ID)
	b.site.Devices[id] = dev
}

func (b *builder) checkTransport(path, scheme string, d DeviceSpec) {
	isModbus := scheme == "tcp" || scheme == "rtu"
	want := profile.TransportHTTP
	if isModbus {
		want = profile.TransportModbus
	}
	b.addf(d.Profile.Transport != want, path+".profile",
		"profile %s needs a %s connection", d.Profile.ID, d.Profile.Transport)
	b.addf(isModbus && (d.Unit < 0 || d.Unit > maxModbusUnit), path+".unit",
		"must be 0..%d", maxModbusUnit)
}

// readMode applies the per-device override; range may be switched to direct only.
func (b *builder) readMode(path string, d DeviceSpec) string {
	if d.Profile.Read == nil {
		return ""
	}
	mode := d.Profile.Read.Mode
	if d.Read == "" || d.Read == mode {
		return mode
	}
	b.addf(d.Read == profile.ReadRange, path+".read",
		"profile %s uses direct reads; range is not allowed (its ranges are unverified)",
		d.Profile.ID)
	return profile.ReadDirect
}

func netting(d DeviceSpec) string {
	switch {
	case d.Netting != "":
		return d.Netting
	case d.Profile.Netting != "":
		return d.Profile.Netting
	default:
		return "meter"
	}
}

func hasDailyReset(p *profile.Profile) bool {
	for _, blk := range p.Blocks {
		for _, c := range blk.Channels {
			if c.Counter == profile.CounterDailyReset {
				return true
			}
		}
	}
	return false
}
