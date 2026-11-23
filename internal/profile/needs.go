package profile

import "sort"

// Needs is the set of channels one poll of a device has to decode: block → channel names.
// A nil Needs means everything the profile describes (dumps, fixtures).
type Needs map[string]map[string]bool

// Has reports whether the channel is needed; a nil Needs needs every channel.
func (n Needs) Has(block, channel string) bool {
	return n == nil || n[block][channel]
}

// Blocks returns the blocks with at least one needed channel, sorted.
func (n Needs) Blocks() []string {
	out := make([]string, 0, len(n))
	for b := range n {
		out = append(out, b)
	}
	sort.Strings(out)
	return out
}

// NeedsFor returns the channels the given blocks need: all their channels, the inputs of
// product channels and, for a share channel, the shared counter and the weight channel of
// every block sharing it (a split is only right with all weights).
func (p *Profile) NeedsFor(blocks []string) Needs {
	n := Needs{}
	for _, b := range blocks {
		for name := range p.Blocks[b].Channels {
			n.add(p, b, name)
		}
	}
	return n
}

func (n Needs) add(p *Profile, block, name string) {
	c, ok := p.Blocks[block].Channels[name]
	if !ok || n[block][name] {
		return
	}
	if n[block] == nil {
		n[block] = map[string]bool{}
	}
	n[block][name] = true
	for _, r := range c.Product {
		n.addRef(p, r)
	}
	if c.Derive != "" {
		n.addRef(p, c.Of)
		for _, s := range p.Sharers(c.Of) {
			n.add(p, s[0], s[1])
		}
	}
}

func (n Needs) addRef(p *Profile, ref string) {
	if b, c, ok := ChannelRef(ref); ok {
		n.add(p, b, c)
	}
}

// Sharers returns block and weight channel of every share channel that splits the counter
// "of" (block.channel), sorted by block.
func (p *Profile) Sharers(of string) [][2]string {
	var out [][2]string
	for _, bn := range sortedKeys(p.Blocks) {
		for _, cn := range sortedKeys(p.Blocks[bn].Channels) {
			if c := p.Blocks[bn].Channels[cn]; c.Derive != "" && c.Of == of {
				out = append(out, [2]string{bn, c.By})
			}
		}
	}
	return out
}

// neededFields returns the registers one poll reads: those of the needed channels (with
// their sign registers) and every status field.
func (p *Profile) neededFields(n Needs) fieldRefs {
	var out fieldRefs
	p.eachNeeded(n, func(path string, c Channel) {
		out.add(path, &c.Field)
		if c.SignFrom != nil {
			out.add(path+".sign_from", &c.SignFrom.Field)
		}
	})
	for _, sn := range sortedKeys(p.Status) {
		s := p.Status[sn]
		out.add("status."+sn, &s.Field)
	}
	return out
}

// neededEndpoints returns the IDs of the HTTP endpoints a poll needs: those the needed
// channels and the status read from, plus the ID arrays of value_id channels.
func (p *Profile) neededEndpoints(n Needs) map[string]bool {
	out := map[string]bool{}
	p.eachNeeded(n, func(_ string, c Channel) {
		out[c.Endpoint] = true
		if c.SignFrom != nil {
			out[c.SignFrom.Endpoint] = true
		}
		if c.ValueID != nil {
			out[c.ValueID.IDs] = true
		}
	})
	for _, s := range p.Status {
		out[s.Endpoint] = true
	}
	delete(out, "")
	return out
}

func (p *Profile) eachNeeded(n Needs, f func(path string, c Channel)) {
	for _, bn := range sortedKeys(p.Blocks) {
		for _, cn := range sortedKeys(p.Blocks[bn].Channels) {
			if n.Has(bn, cn) {
				f("blocks."+bn+".channels."+cn, p.Blocks[bn].Channels[cn])
			}
		}
	}
}
