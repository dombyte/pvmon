package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/dombyte/pvmon/internal/health"
	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/internal/util"
)

// dateLayout is the local date format of data_since.
const dateLayout = "2006-01-02"

// FirstSlotSource tells where the stored data starts.
type FirstSlotSource interface {
	FirstSlot(ctx context.Context) (slot int64, ok bool, err error)
}

// SiteModel is the body of GET /api/v1/site (spec 9.1).
type SiteModel struct {
	Name         string            `json:"name"`
	TZ           string            `json:"tz"`
	ConfigRev    int64             `json:"config_rev"`
	DataSince    *string           `json:"data_since"`
	Capabilities CapabilitiesModel `json:"capabilities"`
	Meters       []MeterModel      `json:"meters"`
	Devices      []DeviceModel     `json:"devices"`
	SiteSeries   []string          `json:"site_series"`
}

// CapabilitiesModel is what the site has; the UI builds its layout from it.
type CapabilitiesModel struct {
	PV         int            `json:"pv"`
	Battery    int            `json:"battery"`
	Grid       bool           `json:"grid"`
	Backup     bool           `json:"backup"`
	Loadpoints map[string]int `json:"loadpoints"`
}

// MeterModel is one meter; it carries no connection details.
type MeterModel struct {
	ID         string   `json:"id"`
	Role       string   `json:"role"`
	Category   string   `json:"category,omitempty"`
	Label      string   `json:"label"`
	Device     string   `json:"device"`
	Parent     string   `json:"parent,omitempty"`
	CapacityWh float64  `json:"capacity_wh,omitempty"`
	KWp        float64  `json:"kwp,omitempty"`
	Series     []string `json:"series"`
}

// DeviceModel is one device; addresses and credentials are never exposed.
type DeviceModel struct {
	ID       string `json:"id"`
	Profile  string `json:"profile"`
	Verified bool   `json:"verified"`
	State    string `json:"state"`
}

// Site builds the site model from the effective site model.
type Site struct {
	site  *site.Site
	loc   *time.Location
	first FirstSlotSource
	snap  SnapshotSource
}

// NewSite builds the service.
func NewSite(s *site.Site, loc *time.Location, first FirstSlotSource,
	snap SnapshotSource,
) (*Site, error) {
	if err := util.RequireAll("site service", util.D("site", s), util.D("location", loc),
		util.D("first slot source", first), util.D("snapshot source", snap)); err != nil {
		return nil, err
	}
	return &Site{site: s, loc: loc, first: first, snap: snap}, nil
}

// Model returns the current site model.
func (s *Site) Model(ctx context.Context) (SiteModel, error) {
	since, err := s.dataSince(ctx)
	if err != nil {
		return SiteModel{}, err
	}
	c := s.site.Capabilities()
	return SiteModel{
		Name: s.site.Name, TZ: s.loc.String(), ConfigRev: s.site.Rev, DataSince: since,
		Capabilities: CapabilitiesModel{
			PV: c.PV, Battery: c.Battery, Grid: c.Grid, Backup: c.Backup,
			Loadpoints: c.Loadpoints,
		},
		Meters: s.meters(), Devices: s.devices(), SiteSeries: s.site.Quantities(),
	}, nil
}

func (s *Site) dataSince(ctx context.Context) (*string, error) {
	slot, ok, err := s.first.FirstSlot(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: data since: %w", err)
	}
	if !ok {
		return nil, nil //nolint:nilnil // no data is a valid answer: JSON null
	}
	d := time.Unix(slot*storage.SlotSeconds, 0).In(s.loc).Format(dateLayout)
	return &d, nil
}

func (s *Site) meters() []MeterModel {
	out := make([]MeterModel, 0, len(s.site.Meters))
	for _, id := range sortedKeys(s.site.Meters) {
		m := s.site.Meters[id]
		mm := MeterModel{
			ID: id, Role: m.Role, Label: m.Label, Device: m.Device,
			CapacityWh: m.CapacityWh, KWp: m.KWp, Series: meterSeries(m),
		}
		switch m.Role {
		case profile.RoleConsumer:
			mm.Category, mm.Parent = m.Category, m.Parent
		case profile.RolePV: // "site", or the PV total a string belongs to
			mm.Parent = m.Parent
		}
		out = append(out, mm)
	}
	return out
}

// meterSeries lists the live channel keys of the meter's profile block, sorted.
func meterSeries(m *site.Meter) []string {
	out := make([]string, 0, len(m.Block.Channels))
	for name := range m.Block.Channels {
		out = append(out, m.ID+"/"+name)
	}
	sort.Strings(out)
	return out
}

func (s *Site) devices() []DeviceModel {
	states := s.snap.Snapshot().Devices
	out := make([]DeviceModel, 0, len(s.site.Devices))
	for _, id := range sortedKeys(s.site.Devices) {
		d := s.site.Devices[id]
		state, ok := states[id]
		if !ok {
			state = health.DeviceUnconfigured
		}
		out = append(out, DeviceModel{
			ID: id, Profile: d.Profile.ID, Verified: d.Profile.Verified, State: state,
		})
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
