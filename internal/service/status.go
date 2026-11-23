package service

import (
	"context"
	"slices"
	"time"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/storage"
)

// Levels of the generic device status (spec 12): the profile levels plus the connection.
const (
	LevelOffline = "offline" // the device does not answer
	LevelNone    = "none"    // pvmon did not run: no data
)

// overviewLimit bounds the changes of one device on the status page.
const overviewLimit = 5000

// LevelChange is a change of the generic level of a device.
type LevelChange struct {
	At    time.Time `json:"at"`
	Level string    `json:"level"`
}

// DeviceStatus is one device of GET /api/v1/status: the generic level over time, oldest
// first; the first change is the level active at the start of the range.
type DeviceStatus struct {
	ID      string        `json:"id"`
	Changes []LevelChange `json:"changes"`
}

func statusRegisters(p *profile.Profile) []StatusRegister {
	out := make([]StatusRegister, 0, len(p.Status))
	for _, name := range sortedKeys(p.Status) {
		out = append(out, StatusRegister{Name: name, Label: p.Status[name].Label})
	}
	return out
}

// describe returns the level and texts of a stored change.
func describe(p *profile.Profile, e storage.StatusEvent) (string, []profile.Text) {
	if e.Register == storage.ConnectionRegister {
		return connectionLevel(e.Code), nil
	}
	sv := p.DescribeStatus(e.Register, uint64(e.Code))
	if !sv.Valid {
		return profile.LevelNotice, nil // a register the profile no longer has
	}
	return sv.Level, sv.Texts
}

func connectionLevel(code int64) string {
	switch code {
	case storage.ConnectionOnline:
		return profile.LevelOK
	case storage.ConnectionOffline:
		return LevelOffline
	default:
		return LevelNone
	}
}

// Overview returns the generic level of every device in [from, to), sorted by ID.
func (s *Devices) Overview(ctx context.Context, from, to time.Time) ([]DeviceStatus, error) {
	out := make([]DeviceStatus, 0, len(s.site.Devices))
	for _, id := range sortedKeys(s.site.Devices) {
		d, ev, err := s.events(ctx, id, from, to, overviewLimit)
		if err != nil {
			return nil, err
		}
		out = append(out, DeviceStatus{ID: id, Changes: s.levelChanges(d.Profile.Profile, ev)})
	}
	return out, nil
}

// levelChanges replays the changes oldest first and keeps every change of the generic
// level: the connection when it is not online, else the worst register level.
func (s *Devices) levelChanges(p *profile.Profile, ev []storage.StatusEvent) []LevelChange {
	slices.SortStableFunc(ev, func(a, b storage.StatusEvent) int { return int(a.At - b.At) })
	levels := map[string]string{}
	out := []LevelChange{}
	for i, e := range ev {
		levels[e.Register], _ = describe(p, e)
		if i+1 < len(ev) && ev[i+1].At == e.At {
			continue // one poll: decide after all its changes
		}
		level := deviceLevel(levels)
		if n := len(out); n == 0 || out[n-1].Level != level {
			out = append(out, LevelChange{At: time.UnixMilli(e.At).In(s.loc), Level: level})
		}
	}
	return out
}

// deviceLevel is the generic level from the current level of every register.
func deviceLevel(levels map[string]string) string {
	conn := levels[storage.ConnectionRegister]
	if conn == LevelOffline || conn == LevelNone {
		return conn
	}
	worst := profile.LevelOK
	for reg, l := range levels {
		if reg != storage.ConnectionRegister && levelRank(l) > levelRank(worst) {
			worst = l
		}
	}
	return worst
}

func levelRank(l string) int {
	return slices.Index([]string{profile.LevelOK, profile.LevelNotice, profile.LevelFault}, l)
}
