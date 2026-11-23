package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dombyte/pvmon/internal/health"
	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/site"
	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/internal/util"
)

// ErrNotFound reports an unknown device.
var ErrNotFound = errors.New("service: not found")

// statusLimit bounds the status changes of one request (spec 9.1).
const statusLimit = 1000

// StatusReader reads stored status changes.
type StatusReader interface {
	StatusEvents(ctx context.Context, device string, from, to int64, limit int,
	) ([]storage.StatusEvent, error)
}

// DeviceInfo is one device of GET /api/v1/devices; addresses and credentials are never
// exposed.
type DeviceInfo struct {
	ID        string   `json:"id"`
	Profile   string   `json:"profile"`
	Vendor    string   `json:"vendor"`
	Models    []string `json:"models"`
	Transport string   `json:"transport"`
	Verified  bool     `json:"verified"`
	State     string   `json:"state"`
	IntervalS float64  `json:"interval_s"`
	Meters    []string `json:"meters"`
	// LastPoll is the newest poll since start, nil before the first one.
	LastPoll *LastPoll `json:"last_poll"`
}

// LastPoll is the newest poll of a device: start, duration from the first request to the
// decoded sample, and whether it succeeded.
type LastPoll struct {
	At     time.Time `json:"at"`
	TookMS int64     `json:"took_ms"`
	OK     bool      `json:"ok"`
}

// DeviceDetail is GET /api/v1/devices/{id}: the device, the meters it provides with
// their live channels, its status registers and the netting of its grid meter.
type DeviceDetail struct {
	DeviceInfo
	Netting string           `json:"netting,omitempty"`
	Read    string           `json:"read,omitempty"`
	Blocks  []DeviceBlock    `json:"blocks"`
	Status  []StatusRegister `json:"status"`
}

// StatusRegister is a status register of the profile with its display name.
type StatusRegister struct {
	Name  string       `json:"name"`
	Label profile.Text `json:"label,omitempty"`
}

// DeviceBlock is a meter of the device with its channels.
type DeviceBlock struct {
	Meter    string          `json:"meter"`
	Label    string          `json:"label"`
	Role     string          `json:"role"`
	Block    string          `json:"block"`
	Channels []DeviceChannel `json:"channels"`
}

// DeviceChannel is one live channel: its key for the WebSocket and its unit.
type DeviceChannel struct {
	Name string `json:"name"`
	Key  string `json:"key"`
	Unit string `json:"unit,omitempty"`
}

// StatusEventModel is one status change with its level and texts.
type StatusEventModel struct {
	Register string         `json:"register"`
	At       time.Time      `json:"at"`
	Code     int64          `json:"code"`
	Level    string         `json:"level"`
	Texts    []profile.Text `json:"texts"`
}

// Devices answers the device endpoints.
type Devices struct {
	site   *site.Site
	snap   SnapshotSource
	status StatusReader
	loc    *time.Location
}

// NewDevices builds the service.
func NewDevices(s *site.Site, snap SnapshotSource, status StatusReader, loc *time.Location,
) (*Devices, error) {
	if err := util.RequireAll("service", util.D("site", s), util.D("snapshot", snap),
		util.D("status", status), util.D("location", loc)); err != nil {
		return nil, err
	}
	return &Devices{site: s, snap: snap, status: status, loc: loc}, nil
}

// List returns every device, sorted by ID.
func (s *Devices) List() []DeviceInfo {
	snap := s.snap.Snapshot()
	out := make([]DeviceInfo, 0, len(s.site.Devices))
	for _, id := range sortedKeys(s.site.Devices) {
		out = append(out, s.info(s.site.Devices[id], snap))
	}
	return out
}

func (s *Devices) info(d *site.Device, snap health.Snapshot) DeviceInfo {
	state, ok := snap.Devices[d.ID]
	if !ok {
		state = health.DeviceUnconfigured
	}
	p := d.Profile
	return DeviceInfo{
		ID: d.ID, Profile: p.ID, Vendor: p.Vendor, Models: p.Models, Transport: p.Transport,
		Verified: p.Verified, State: state, IntervalS: d.Interval.Seconds(),
		Meters: append([]string{}, d.Meters...), LastPoll: lastPoll(snap.Polls, d.ID, s.loc),
	}
}

func lastPoll(polls map[string]health.Poll, id string, loc *time.Location) *LastPoll {
	p, ok := polls[id]
	if !ok {
		return nil
	}
	return &LastPoll{At: p.At.In(loc), TookMS: p.Took.Milliseconds(), OK: p.OK}
}

// Get returns one device with its meters and channels.
func (s *Devices) Get(id string) (DeviceDetail, error) {
	d, ok := s.site.Devices[id]
	if !ok {
		return DeviceDetail{}, ErrNotFound
	}
	out := DeviceDetail{
		DeviceInfo: s.info(d, s.snap.Snapshot()), Read: d.Read,
		Status: statusRegisters(d.Profile.Profile),
	}
	for _, mid := range d.Meters {
		m := s.site.Meters[mid]
		if m.Role == profile.RoleGrid {
			out.Netting = d.Netting
		}
		out.Blocks = append(out.Blocks, deviceBlock(m))
	}
	return out, nil
}

func deviceBlock(m *site.Meter) DeviceBlock {
	b := DeviceBlock{Meter: m.ID, Label: m.Label, Role: m.Role, Block: m.Source}
	for _, name := range sortedKeys(m.Block.Channels) {
		b.Channels = append(b.Channels, DeviceChannel{
			Name: name, Key: m.ID + "/" + name, Unit: m.Block.Channels[name].Unit,
		})
	}
	return b
}

// Status returns the status changes of a device in [from, to), newest first, followed by
// the change of each register active at from. Levels and texts come from the profile; the
// connection register has the levels ok, offline and none.
func (s *Devices) Status(ctx context.Context, id string, from, to time.Time,
) ([]StatusEventModel, error) {
	d, ev, err := s.events(ctx, id, from, to, statusLimit)
	if err != nil {
		return nil, err
	}
	out := make([]StatusEventModel, len(ev))
	for i, e := range ev {
		level, texts := describe(d.Profile.Profile, e)
		out[i] = StatusEventModel{
			Register: e.Register, At: time.UnixMilli(e.At).In(s.loc), Code: e.Code,
			Level: level, Texts: texts,
		}
	}
	return out, nil
}

// events reads the stored changes of a device.
func (s *Devices) events(ctx context.Context, id string, from, to time.Time, limit int,
) (*site.Device, []storage.StatusEvent, error) {
	d, ok := s.site.Devices[id]
	if !ok {
		return nil, nil, ErrNotFound
	}
	if to.Before(from) {
		return nil, nil, ErrInvalidRange
	}
	ev, err := s.status.StatusEvents(ctx, id, from.UnixMilli(), to.UnixMilli(), limit)
	if err != nil {
		return nil, nil, fmt.Errorf("service: status: %w", err)
	}
	return d, ev, nil
}
