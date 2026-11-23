package profile

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// IDPattern is the pattern of built-in profile IDs (<vendor>/<model>, lower case).
var IDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*/[a-z0-9][a-z0-9-]*$`)

const schemaVersion = 1

type problems []string

func (p *problems) addf(bad bool, path, format string, args ...any) {
	if bad {
		*p = append(*p, path+": "+fmt.Sprintf(format, args...))
	}
}

func in(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// Validate checks a profile completely and returns every problem with its path.
func Validate(p *Profile) []string {
	var ps problems
	validateHeader(p, &ps)
	switch p.Transport {
	case TransportModbus:
		validateModbus(p, &ps)
	case TransportHTTP:
		validateHTTP(p, &ps)
	default:
		ps.addf(true, "transport", "must be modbus or http")
	}
	for _, name := range sortedKeys(p.Blocks) {
		validateBlock(p, name, p.Blocks[name], &ps)
	}
	validateStatus(p, &ps)
	validateMaps(p, &ps)
	return ps
}

func validateHeader(p *Profile, ps *problems) {
	ps.addf(p.Schema != schemaVersion, "schema", "must be %d", schemaVersion)
	ps.addf(!IDPattern.MatchString(p.ID), "profile", "ID %q must be <vendor>/<model>", p.ID)
	ps.addf(p.Version < 1, "version", "must be >= 1")
	ps.addf(p.Vendor == "", "vendor", "must not be empty")
	ps.addf(len(p.Blocks) == 0, "blocks", "must not be empty")
	ps.addf(!in(p.Netting, "", "meter", "app"), "netting", "must be meter or app")
	ps.addf(p.MaxPower < 0, "max_power", "must be >= 0")
	ps.addf(p.MaxPower == 0 && hasLifetime(p), "max_power",
		"required when a counter is lifetime")
}

func hasLifetime(p *Profile) bool {
	for _, b := range p.Blocks {
		for _, c := range b.Channels {
			if c.Counter == CounterLifetime {
				return true
			}
		}
	}
	return false
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ChannelRef splits "block.channel".
func ChannelRef(ref string) (block, channel string, ok bool) {
	block, channel, ok = strings.Cut(ref, ".")
	return block, channel, ok && block != "" && channel != ""
}

func (p *Profile) channel(ref string) (Channel, bool) {
	b, c, ok := ChannelRef(ref)
	if !ok {
		return Channel{}, false
	}
	ch, ok := p.Blocks[b].Channels[c]
	return ch, ok
}
