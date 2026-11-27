package collector_test

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/profiles"
)

// profileFlag verifies one device template in every scenario.
var profileFlag = flag.String("profile", "",
	"device profile for TestProfileSimulation: a built-in ID or a .yaml path "+
		"(relative to the repository root or absolute)")

const repoRoot = "../.."

// simDevice is a profile under test and the blocks that become meters.
type simDevice struct {
	name    string
	profile *profile.Loaded
	blocks  []string
}

// TestProfileSimulation verifies device templates against the core. Everything it needs
// comes from the profile. Without -profile it runs every built-in profile in the clean
// scenario (PVMON_SIM=full: every scenario). With -profile it runs that one in every
// scenario:
//
//	go test -run ProfileSimulation ./internal/collector -profile=my-device.yaml
func TestProfileSimulation(t *testing.T) {
	t.Parallel()
	loader, err := profile.NewLoader(profiles.FS(), os.ReadFile)
	require.NoError(t, err)
	refs, all := loader.IDs(), os.Getenv("PVMON_SIM") == "full"
	if *profileFlag != "" {
		refs, all = []string{*profileFlag}, true
	}
	for _, ref := range refs {
		p, err := loader.Load(ref, repoRoot)
		require.NoError(t, err)
		devices := simDevices(p)
		require.NotEmpty(t, devices, "%s: no block can be a meter", ref)
		for _, d := range devices {
			for _, sc := range scenarios() {
				if !all && sc.name != "clean" {
					continue
				}
				t.Run(d.name+"/"+sc.name, func(t *testing.T) {
					t.Parallel()
					newSim(t, d, sc).run(t).check(t)
				})
			}
		}
	}
}

// otherView is a block that replaces the main view of its meter in a setup of its own.
type otherView struct {
	at    int // index in the main setup
	block string
}

// simDevices turns a profile into the setups to test. Phase blocks are details of another
// block. Blocks that read the same power register, or a second grid block, are views of
// one meter: the first one (with energy channels if any) joins the main setup, every other
// one gets a setup of its own in its place.
func simDevices(p *profile.Loaded) []simDevice {
	names := slices.Sorted(maps.Keys(p.Blocks))
	// Blocks with energy channels first, so they are the main view.
	slices.SortStableFunc(names, func(a, b string) int {
		return rank(p.Blocks[a]) - rank(p.Blocks[b])
	})
	var main []string
	var others []otherView
	taken := map[string]int{} // view key → index in main
	for _, n := range names {
		b := p.Blocks[n]
		if b.Role == profile.RolePhase {
			continue
		}
		key := viewKey(n, b)
		if i, ok := taken[key]; ok {
			others = append(others, otherView{at: i, block: n})
			continue
		}
		taken[key] = len(main)
		main = append(main, n)
	}
	if len(main) == 0 {
		return nil
	}
	out := []simDevice{{name: p.ID, profile: p, blocks: main}}
	for _, o := range others {
		blocks := slices.Clone(main)
		blocks[o.at] = o.block
		out = append(out, simDevice{
			name: p.ID + " as " + o.block, profile: p, blocks: blocks,
		})
	}
	return out
}

func rank(b profile.Block) int {
	if hasEnergy(b) {
		return 0
	}
	return 1
}

// viewKey is equal for blocks that cannot be meters of one site at the same time.
func viewKey(name string, b profile.Block) string {
	if b.Role == profile.RoleGrid {
		return "grid"
	}
	c, ok := b.Channels["power"]
	if !ok || len(c.Product) > 0 {
		return "block " + name
	}
	return fmt.Sprint("power ", c.Addr, c.Endpoint, c.Path, c.Sum, c.ValueID)
}

// shareParent is the block whose counter block name splits, if any.
func shareParent(b profile.Block) (string, bool) {
	for _, c := range b.Channels {
		if c.Derive != "" {
			of, _, ok := profile.ChannelRef(c.Of)
			return of, ok
		}
	}
	return "", false
}
