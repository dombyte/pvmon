package profile

import "sort"

// Request is one Modbus read: Count registers from Start.
type Request struct {
	Start int
	Count int
}

// mergeGap is the largest run of unneeded registers read to save a request: a request
// costs about as much as a few dozen registers on RS485 at 9600 baud.
const mergeGap = 16

// Requests returns the Modbus reads of one poll for the given read mode and needs. Range
// mode reads, within each profile range, only the spans holding needed registers, joining
// spans less than mergeGap apart; direct mode reads each needed field on its own (spec
// 4.3). Nil needs read everything the profile describes (whole ranges), as for dumps.
func (p *Profile) Requests(mode string, n Needs) []Request {
	switch {
	case p.Transport != TransportModbus:
		return nil
	case n == nil && mode == ReadRange:
		out := make([]Request, len(p.Read.Ranges))
		for i, r := range p.Read.Ranges {
			out[i] = Request{Start: r[0], Count: r[1] - r[0] + 1}
		}
		return out
	case n == nil:
		return directRequests(p.fields())
	case mode == ReadRange:
		return p.rangeRequests(p.neededFields(n))
	}
	return directRequests(p.neededFields(n))
}

func (p *Profile) rangeRequests(fields fieldRefs) []Request {
	used := map[int]bool{}
	for _, f := range fields {
		for _, a := range f.field.Addr {
			for i := range f.field.Width() {
				used[a+i] = true
			}
		}
	}
	var out []Request
	for _, r := range p.Read.Ranges {
		out = append(out, spans(used, r[0], r[1])...)
	}
	return out
}

// spans returns the requests covering the used registers in [from, to].
func spans(used map[int]bool, from, to int) []Request {
	var out []Request
	start, last := -1, -1
	for a := from; a <= to; a++ {
		if !used[a] {
			continue
		}
		if start >= 0 && a-last > mergeGap {
			out = append(out, Request{Start: start, Count: last - start + 1})
			start = -1
		}
		if start < 0 {
			start = a
		}
		last = a
	}
	if start >= 0 {
		out = append(out, Request{Start: start, Count: last - start + 1})
	}
	return out
}

func directRequests(fields fieldRefs) []Request {
	seen := map[Request]bool{}
	var out []Request
	for _, f := range fields {
		for _, a := range f.field.Addr {
			r := Request{Start: a, Count: f.field.Width()}
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Start != out[j].Start {
			return out[i].Start < out[j].Start
		}
		return out[i].Count < out[j].Count
	})
	return out
}

// Endpoints returns the HTTP endpoints a poll with the given needs (nil: everything) reads;
// slow endpoints (Interval > 0) are included only when due reports true for them.
func (p *Profile) Endpoints(n Needs, due func(Endpoint) bool) []Endpoint {
	if p.HTTP == nil {
		return nil
	}
	used := p.neededEndpoints(n)
	var out []Endpoint
	for _, e := range p.HTTP.Endpoints {
		if n != nil && !used[e.ID] {
			continue
		}
		if e.Interval == 0 || due(e) {
			out = append(out, e)
		}
	}
	return out
}
