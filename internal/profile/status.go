package profile

import (
	"fmt"
	"math/bits"
	"sort"
)

// StatusValue is the evaluated state of one status entry.
type StatusValue struct {
	Code  uint64
	Level string
	Texts []Text // texts of the code or of every set bit
	Valid bool
}

// levelRank orders levels so the worst wins.
func levelRank(l string) int {
	return map[string]int{LevelOK: 0, LevelNotice: 1, LevelFault: 2}[l] //nolint:mnd // rank
}

// Statuses evaluates every status entry of the profile.
func (p *Profile) Statuses(raw Raw) map[string]StatusValue {
	out := make(map[string]StatusValue, len(p.Status))
	for name, s := range p.Status {
		out[name] = p.statusValue(s, raw)
	}
	return out
}

func (p *Profile) statusValue(s Status, raw Raw) StatusValue {
	v, err := p.raw(s.Field, raw)
	if err != nil || v.Num < 0 {
		return StatusValue{}
	}
	return p.evaluate(s, uint64(v.Num))
}

// DescribeStatus evaluates a stored code of the status entry name (level and texts);
// an unknown name is invalid.
func (p *Profile) DescribeStatus(name string, code uint64) StatusValue {
	s, ok := p.Status[name]
	if !ok {
		return StatusValue{}
	}
	return p.evaluate(s, code)
}

func (p *Profile) evaluate(s Status, code uint64) StatusValue {
	sv := StatusValue{Code: code, Level: LevelOK, Valid: true}
	switch {
	case s.Map != "":
		p.applyMap(&sv, s, p.Maps[s.Map])
	case s.Level != "":
		sv.setIf(code != 0, s.Level, nil)
	case s.NonEmpty != "":
		sv.setIf(code != 0, s.NonEmpty, s.Text)
	case s.False != "":
		sv.setIf(code == 0, s.False, s.Text)
	default:
		sv.setIf(code != 0, s.NonZero, s.Text)
	}
	return sv
}

func (sv *StatusValue) setIf(cond bool, level string, text Text) {
	if !cond {
		return
	}
	sv.raise(level)
	if text != nil {
		sv.Texts = append(sv.Texts, text)
	}
}

func (sv *StatusValue) raise(level string) {
	if levelRank(level) > levelRank(sv.Level) {
		sv.Level = level
	}
}

func (p *Profile) applyMap(sv *StatusValue, s Status, m Map) {
	if s.Type == "bitfield" || len(m.Bits) > 0 {
		applyBits(sv, m)
		return
	}
	e, ok := m.Codes[sv.Code]
	switch {
	case ok:
		sv.Level = e.Level
		sv.Texts = []Text{e.Text}
	case m.Default != nil:
		sv.Level = m.Default.Level
		sv.Texts = []Text{unknownCode(sv.Code)}
	default:
		sv.Level = LevelNotice
		sv.Texts = []Text{unknownCode(sv.Code)}
	}
}

// applyBits raises the level for every set bit; unmapped bits use the default or fault.
func applyBits(sv *StatusValue, m Map) {
	var set []int
	for v := sv.Code; v != 0; v &= v - 1 {
		set = append(set, bits.TrailingZeros64(v))
	}
	sort.Ints(set)
	for _, bit := range set {
		e, ok := m.Bits[bit]
		switch {
		case ok:
			sv.raise(e.Level)
			sv.Texts = append(sv.Texts, e.Text)
		case m.Default != nil:
			sv.raise(m.Default.Level)
			sv.Texts = append(sv.Texts, unknownBit(bit))
		default:
			sv.raise(LevelFault)
			sv.Texts = append(sv.Texts, unknownBit(bit))
		}
	}
}

func unknownCode(code uint64) Text {
	c := fmt.Sprintf("0x%04X", code)
	return Text{"en": "Unknown code " + c, "de": "Unbekannter Code " + c}
}

func unknownBit(bit int) Text {
	return Text{
		"en": fmt.Sprintf("Unknown bit %d", bit),
		"de": fmt.Sprintf("Unbekanntes Bit %d", bit),
	}
}
