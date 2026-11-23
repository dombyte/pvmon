package service

import (
	"slices"
	"sort"

	"github.com/dombyte/pvmon/internal/profile"
)

// ProfileSource lists and resolves the built-in profiles (*profile.Loader).
type ProfileSource interface {
	IDs() []string
	Load(ref, baseDir string) (*profile.Loaded, error)
}

// ProfileInfo is one built-in profile for the setup wizard (GET /api/v1/profiles).
type ProfileInfo struct {
	ID        string         `json:"id"`
	Vendor    string         `json:"vendor"`
	Models    []string       `json:"models"`
	Transport string         `json:"transport"`
	Verified  bool           `json:"verified"`
	Netting   string         `json:"netting,omitempty"`
	Blocks    []ProfileBlock `json:"blocks"`
}

// ProfileBlock is one meter block of a profile with the roles it may take.
type ProfileBlock struct {
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	RolesAllowed []string `json:"roles_allowed"`
	Category     string   `json:"category,omitempty"`
	Channels     []string `json:"channels"`
}

// Profiles answers GET /api/v1/profiles.
type Profiles struct{ src ProfileSource }

// NewProfiles builds the service.
func NewProfiles(src ProfileSource) *Profiles { return &Profiles{src: src} }

// List returns every built-in profile, sorted by ID.
func (p *Profiles) List() []ProfileInfo {
	ids := p.src.IDs()
	out := make([]ProfileInfo, 0, len(ids))
	for _, id := range ids {
		l, err := p.src.Load(id, "")
		if err != nil {
			continue // IDs come from the loader itself
		}
		out = append(out, profileInfo(l.Profile))
	}
	return out
}

func profileInfo(pr *profile.Profile) ProfileInfo {
	info := ProfileInfo{
		ID: pr.ID, Vendor: pr.Vendor, Models: pr.Models, Transport: pr.Transport,
		Verified: pr.Verified, Netting: pr.Netting, Blocks: []ProfileBlock{},
	}
	for name, b := range pr.Blocks {
		chans := make([]string, 0, len(b.Channels))
		for c := range b.Channels {
			chans = append(chans, c)
		}
		sort.Strings(chans)
		roles := append([]string{}, b.RolesAllowed...)
		info.Blocks = append(info.Blocks, ProfileBlock{
			Name: name, Role: b.Role, RolesAllowed: roles, Category: b.Category,
			Channels: chans,
		})
	}
	slices.SortFunc(info.Blocks, func(a, b ProfileBlock) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return info
}
