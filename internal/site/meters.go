package site

import (
	"sort"

	"github.com/dombyte/pvmon/internal/profile"
)

// CategoryOther is the default consumer category.
const CategoryOther = "other"

func (b *builder) addMeter(id string, m MeterSpec) {
	path := "meters." + id
	dev, ok := b.spec.Devices[m.Device]
	b.addf(!ok, path+".device", "unknown device %q", m.Device)
	if !ok || dev.Profile == nil {
		return
	}
	blk, ok := dev.Profile.Blocks[m.Source]
	b.addf(!ok, path+".source", "profile %s has no block %q", dev.Profile.ID, m.Source)
	if !ok {
		return
	}
	meter := &Meter{
		ID: id, Device: m.Device, Source: m.Source, Role: m.Role, Category: m.Category,
		Parent: m.Parent, Label: m.Label, KWp: m.KWp, CapacityWh: m.CapacityWh,
		Critical: m.Critical, Block: blk,
	}
	if meter.Role == "" {
		meter.Role = blk.Role
	}
	b.addf(!blk.Allows(meter.Role), path+".role", "block %s.%s does not allow role %q",
		dev.Profile.ID, m.Source, meter.Role)
	b.applyCategory(path, meter)
	if meter.Label == "" {
		meter.Label = id
	}
	b.site.Meters[id] = meter
}

func (b *builder) applyCategory(path string, m *Meter) {
	if m.Role != profile.RoleConsumer {
		b.addf(m.Category != "", path+".category", "only consumers have a category")
		m.Category = ""
		b.applyParent(path, m)
		return
	}
	if m.Category == "" {
		m.Category = m.Block.Category
	}
	if m.Category == "" {
		m.Category = CategoryOther
	}
	b.addf(!contains(profile.Categories(), m.Category), path+".category",
		"unknown category %q", m.Category)
	if m.Parent == "" {
		m.Parent = ParentSite
	}
}

// applyParent: a PV string may sit below the PV total of its inverter (spec 2); other
// non-consumers have no parent.
func (b *builder) applyParent(path string, m *Meter) {
	if m.Role == profile.RolePV {
		if m.Parent == "" {
			m.Parent = ParentSite
		}
		return
	}
	b.addf(m.Parent != "" && m.Parent != ParentSite, path+".parent",
		"only consumers and pv meters can have a meter as parent")
	m.Parent = ""
}

// addBlock records a profile block a meter of the device uses, keeping the list sorted.
func (d *Device) addBlock(block string) {
	if !contains(d.Blocks, block) {
		d.Blocks = append(d.Blocks, block)
		sort.Strings(d.Blocks)
	}
}

// Nested reports whether the meter sits inside another meter (a PV string below its
// total, a consumer sub-meter); site quantities count only meters that are not nested.
func (m *Meter) Nested() bool { return m.Parent != "" && m.Parent != ParentSite }

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// checkHierarchy verifies parents exist, have the right role and form no cycle: a
// consumer's parent is a consumer, a PV string's parent is a top-level PV meter.
func (b *builder) checkHierarchy() {
	for _, id := range sorted(b.site.Meters) {
		m := b.site.Meters[id]
		if m.Parent == "" || m.Parent == ParentSite {
			continue
		}
		path := "meters." + id + ".parent"
		parent, ok := b.site.Meters[m.Parent]
		b.addf(!ok, path, "unknown meter %q", m.Parent)
		if !ok {
			continue
		}
		if m.Role == profile.RolePV {
			b.addf(parent.Role != profile.RolePV || parent.Parent != ParentSite, path,
				"parent %q is not a top-level pv meter", m.Parent)
			continue
		}
		b.addf(parent.Role != profile.RoleConsumer, path, "parent %q is not a consumer", m.Parent)
		b.addf(b.inCycle(id), path, "parent chain forms a cycle")
	}
}

func (b *builder) inCycle(start string) bool {
	seen := map[string]bool{}
	for cur := start; cur != "" && cur != ParentSite; {
		if seen[cur] {
			return true
		}
		seen[cur] = true
		m, ok := b.site.Meters[cur]
		if !ok {
			return false
		}
		cur = m.Parent
	}
	return false
}

// checkGrid requires exactly one grid meter once any meter exists; an empty site is the
// first start before the setup wizard (spec 3, 11.14).
func (b *builder) checkGrid() {
	if len(b.site.Meters) == 0 {
		return
	}
	var grids []string
	for _, id := range sorted(b.site.Meters) {
		if b.site.Meters[id].Role == profile.RoleGrid {
			grids = append(grids, id)
		}
	}
	b.addf(len(grids) == 0, "meters", "exactly one grid meter is required, found none")
	b.addf(len(grids) > 1, "meters", "exactly one grid meter is required, found %v", grids)
}

// link fills the back references device → meters and connection → devices.
func (b *builder) link() {
	for _, id := range sorted(b.site.Meters) {
		m := b.site.Meters[id]
		if d, ok := b.site.Devices[m.Device]; ok {
			d.Meters = append(d.Meters, id)
			d.addBlock(m.Source)
		}
	}
	for _, id := range sorted(b.site.Devices) {
		d := b.site.Devices[id]
		if c, ok := b.site.Connections[d.Connection]; ok {
			c.Devices = append(c.Devices, id)
		}
	}
	for _, c := range b.site.Connections {
		sort.Strings(c.Devices)
	}
}
