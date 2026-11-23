package maintenance

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/storage"
)

// ErrSolisMap marks an invalid import map; the message lists every problem.
var ErrSolisMap = errors.New("maintenance: invalid import map")

// SolisMap assigns solis series to the new site (spec 7.8). Energy maps a solis key
// without level suffix (pv_energy) to "<meter>/<channel>"; Status maps solis status keys
// to the status registers of one device's profile.
type SolisMap struct {
	Energy map[string]string `yaml:"energy"`
	Status struct {
		Device    string            `yaml:"device"`
		Registers map[string]string `yaml:"registers"`
	} `yaml:"status"`
}

// ParseSolisMap decodes a map file; unknown fields fail.
func ParseSolisMap(data []byte) (*SolisMap, error) {
	var m SolisMap
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSolisMap, err)
	}
	return &m, nil
}

// Resolve checks the map against the site and returns the target series per solis key.
// Every problem is reported, not only the first.
func (m *SolisMap) Resolve(s *site.Site) (map[string]storage.SeriesDef, error) {
	defs := energyDefs(s)
	out := make(map[string]storage.SeriesDef, len(m.Energy))
	var problems []string
	if len(m.Energy) == 0 {
		problems = append(problems, "energy: no series mapped")
	}
	used := map[string]string{}
	for _, key := range sortedKeys(m.Energy) {
		target := m.Energy[key]
		def, ok := defs[target]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf(
				"energy.%s: %q is no energy series of the site", key, target))
		case used[target] != "":
			problems = append(problems, fmt.Sprintf(
				"energy.%s: %q is already the target of %s", key, target, used[target]))
		default:
			used[target] = key
			out[key] = def
		}
	}
	problems = append(problems, m.statusProblems(s)...)
	if len(problems) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrSolisMap, strings.Join(problems, "; "))
	}
	return out, nil
}

// energyDefs returns the visible energy series of the site by "<meter>/<channel>".
func energyDefs(s *site.Site) map[string]storage.SeriesDef {
	out := map[string]storage.SeriesDef{}
	for _, si := range s.Series() {
		if si.Kind != site.KindEnergy || si.Hidden {
			continue
		}
		out[si.Key()] = storage.SeriesDef{
			SeriesKey: storage.SeriesKey{Meter: si.Meter, Channel: si.Channel},
			Kind:      si.Kind, Origin: si.Origin, Unit: si.Unit,
		}
	}
	return out
}

func (m *SolisMap) statusProblems(s *site.Site) []string {
	if len(m.Status.Registers) == 0 {
		return nil
	}
	dev, ok := s.Devices[m.Status.Device]
	if !ok {
		return []string{fmt.Sprintf("status.device: unknown device %q", m.Status.Device)}
	}
	var problems []string
	for _, key := range sortedKeys(m.Status.Registers) {
		reg := m.Status.Registers[key]
		if _, ok := dev.Profile.Status[reg]; !ok {
			problems = append(problems, fmt.Sprintf(
				"status.registers.%s: profile %s has no status %q", key, dev.Profile.ID, reg))
		}
	}
	return problems
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
