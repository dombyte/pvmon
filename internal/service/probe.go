package service

import (
	"sort"
	"time"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/util"
)

// ProbeResult is one read of a device with a draft configuration (POST
// /api/v1/config/probe, spec 11.14). A device that does not answer is a result with Error,
// not a failed request.
type ProbeResult struct {
	Device   string       `json:"device"`
	Profile  string       `json:"profile"`
	Verified bool         `json:"verified"`
	At       time.Time    `json:"at"`
	TookMS   int64        `json:"took_ms"`
	Error    string       `json:"error,omitempty"`
	Blocks   []ProbeBlock `json:"blocks"`
}

// ProbeBlock is one meter block of the profile with its decoded channels.
type ProbeBlock struct {
	Name         string         `json:"name"`
	Role         string         `json:"role"`
	RolesAllowed []string       `json:"roles_allowed"`
	Category     string         `json:"category,omitempty"`
	Channels     []ProbeChannel `json:"channels"`
}

// ProbeChannel is one normalised value; Value is null when the device did not deliver it.
type ProbeChannel struct {
	Name  string   `json:"name"`
	Value *float64 `json:"value"`
	Text  string   `json:"text,omitempty"`
	Unit  string   `json:"unit,omitempty"`
}

// probeDecimals rounds probe values like every JSON number (spec 9.1).
const probeDecimals = 2

// ProbeBlocks decodes every meter block of the profile from one raw sample, sorted by
// name. Without raw data (a failed read) the channels have no values.
func ProbeBlocks(p *profile.Profile, raw *profile.Raw) []ProbeBlock {
	names := make([]string, 0, len(p.Blocks))
	for name := range p.Blocks {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]ProbeBlock, 0, len(names))
	for _, name := range names {
		b := p.Blocks[name]
		var vals map[string]profile.Value
		if raw != nil {
			vals = p.Channels(name, *raw)
		}
		out = append(out, ProbeBlock{
			Name: name, Role: b.Role, RolesAllowed: append([]string{}, b.RolesAllowed...),
			Category: b.Category, Channels: probeChannels(b, vals),
		})
	}
	return out
}

func probeChannels(b profile.Block, vals map[string]profile.Value) []ProbeChannel {
	names := make([]string, 0, len(b.Channels))
	for name, c := range b.Channels {
		if c.Derive == "" { // shares are computed from deltas, never read
			names = append(names, name)
		}
	}
	sort.Strings(names)
	out := make([]ProbeChannel, 0, len(names))
	for _, name := range names {
		ch := ProbeChannel{Name: name, Unit: profile.NormalUnit(b.Channels[name].Unit)}
		ch.Value, ch.Text = probeValue(vals[name])
		out = append(out, ch)
	}
	return out
}

// probeValue is the number or text of a value; a missing value has neither.
func probeValue(v profile.Value) (*float64, string) {
	if v.Text != "" {
		return nil, v.Text
	}
	if !v.Valid {
		return nil, ""
	}
	n := util.Round(v.Num, probeDecimals)
	return &n, ""
}
