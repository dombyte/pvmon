package profile

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
)

// Raw is the raw data of one poll: Modbus words by address or decoded JSON by endpoint.
type Raw struct {
	Words map[int]uint16
	JSON  map[string]any
}

// Value is one decoded channel value. Valid is false when the data is missing; a missing
// value is never 0.
type Value struct {
	Num   float64
	Text  string // string and enum channels
	Valid bool
}

// ErrMissing marks a value absent from the raw data.
var ErrMissing = errors.New("value missing")

// Channels decodes and normalises every channel of a block. Derive (share) channels are
// left out: they are computed from energy deltas by the collector.
func (p *Profile) Channels(block string, raw Raw) map[string]Value {
	b := p.Blocks[block]
	out := make(map[string]Value, len(b.Channels))
	for _, name := range sortedKeys(b.Channels) {
		c := b.Channels[name]
		if c.Derive != "" || len(c.Product) > 0 {
			continue
		}
		out[name] = p.channelValue(c, raw)
	}
	for _, name := range sortedKeys(b.Channels) {
		if c := b.Channels[name]; len(c.Product) > 0 {
			out[name] = p.productValue(c, raw)
		}
	}
	return out
}

func (p *Profile) channelValue(c Channel, raw Raw) Value {
	v, err := p.rawChannel(c, raw)
	if err != nil {
		return Value{}
	}
	if c.Type == "enum" || c.Type == "string" {
		v.Num = normalise(c, v.Num)
		return v
	}
	n, ok := p.applySign(c, normalise(c, v.Num), raw)
	if !ok {
		return Value{}
	}
	if c.Invert {
		n = -n
	}
	return Value{Num: n, Valid: true}
}

// applySign gives a magnitude the sign of its direction field (+ when discharging).
func (p *Profile) applySign(c Channel, n float64, raw Raw) (float64, bool) {
	if c.SignFrom == nil {
		return n, true
	}
	dir, err := p.raw(c.SignFrom.Field, raw)
	if err != nil {
		return 0, false
	}
	n = math.Abs(n)
	if int(dir.Num) != c.SignFrom.DischargeWhen {
		n = -n
	}
	return n, true
}

// normalise applies scale, offset and the unit factor (energy → Wh, power → W).
func normalise(c Channel, v float64) float64 {
	v = v*c.ScaleOr1() + c.Offset
	if f := energyUnits()[c.Unit]; f > 0 {
		return v * f
	}
	if f := powerUnits()[c.Unit]; f > 0 {
		return v * f
	}
	return v
}

func (p *Profile) productValue(c Channel, raw Raw) Value {
	prod := 1.0
	for _, ref := range c.Product {
		src, _ := p.channel(ref)
		v := p.channelValue(src, raw)
		if !v.Valid {
			return Value{}
		}
		prod *= v.Num
	}
	return Value{Num: normalise(c, prod), Valid: true}
}

func (p *Profile) rawChannel(c Channel, raw Raw) (Value, error) {
	switch {
	case len(c.Sum) > 0:
		return sumPaths(raw.JSON[c.Endpoint], c.Sum)
	case c.ValueID != nil:
		return valueByID(raw.JSON[c.ValueID.IDs], raw.JSON[c.Endpoint], c.ValueID.ID)
	case c.Type == "enum":
		v, err := p.raw(c.Field, raw)
		if err != nil {
			return Value{}, err
		}
		v.Text = c.Values[int(v.Num)]
		return v, nil
	}
	return p.raw(c.Field, raw)
}

func sumPaths(doc any, paths []string) (Value, error) {
	total := 0.0
	for _, path := range paths {
		v, ok := Lookup(doc, path)
		if !ok {
			return Value{}, ErrMissing
		}
		n, err := number(v)
		if err != nil {
			return Value{}, err
		}
		total += n
	}
	return Value{Num: total, Valid: true}, nil
}

// valueByID finds id in the ID array and returns the value at the same index (WARP).
func valueByID(ids, values any, id int) (Value, error) {
	idList, ok1 := ids.([]any)
	valList, ok2 := values.([]any)
	if !ok1 || !ok2 || len(idList) != len(valList) {
		return Value{}, ErrMissing
	}
	i := indexOfID(idList, id)
	if i < 0 {
		return Value{}, ErrMissing
	}
	v, err := number(valList[i])
	if err != nil {
		return Value{}, err
	}
	return Value{Num: v, Valid: true}, nil
}

func indexOfID(ids []any, id int) int {
	for i, x := range ids {
		if n, err := number(x); err == nil && int(n) == id {
			return i
		}
	}
	return -1
}

// raw reads one field without transforms.
func (p *Profile) raw(f Field, raw Raw) (Value, error) {
	if f.Path != "" {
		return jsonValue(raw.JSON[f.Endpoint], f)
	}
	return p.registerValue(f, raw.Words)
}

func jsonValue(doc any, f Field) (Value, error) {
	v, ok := Lookup(doc, f.Path)
	if !ok || v == nil {
		if f.Type == "list" {
			return Value{Num: 0, Valid: true}, nil // devices omit empty lists (Shelly errors)
		}
		return Value{}, ErrMissing
	}
	switch f.Type {
	case "string":
		return Value{Text: fmt.Sprint(v), Valid: true}, nil
	case "list":
		return listLength(v, f.Path)
	}
	n, err := number(v)
	if err != nil {
		return Value{}, fmt.Errorf("%s: %w", f.Path, err)
	}
	return Value{Num: n, Valid: true}, nil
}

func listLength(v any, path string) (Value, error) {
	l, ok := v.([]any)
	if !ok {
		return Value{}, fmt.Errorf("%s: not a list", path)
	}
	return Value{Num: float64(len(l)), Valid: true}, nil
}

// words returns n registers from addr in the profile's word order, each in byte order.
func (p *Profile) words(words map[int]uint16, addr, n int) ([]byte, error) {
	buf := make([]byte, 0, bytesPerWord*n)
	for i := range n {
		w, ok := words[addr+i]
		if !ok {
			return nil, ErrMissing
		}
		if p.Modbus.ByteOrder == "little" {
			w = w<<bitsPerByte | w>>bitsPerByte
		}
		buf = binary.BigEndian.AppendUint16(buf, w)
	}
	if p.Modbus.WordOrder == "little" {
		reverseWords(buf)
	}
	return buf, nil
}

func reverseWords(b []byte) {
	for i, j := 0, len(b)-bytesPerWord; i < j; i, j = i+bytesPerWord, j-bytesPerWord {
		b[i], b[i+1], b[j], b[j+1] = b[j], b[j+1], b[i], b[i+1]
	}
}

func (p *Profile) registerValue(f Field, words map[int]uint16) (Value, error) {
	if len(f.Addr) == 0 {
		return Value{}, ErrMissing
	}
	if len(f.Addr) > 1 {
		return p.multiBitfield(f, words)
	}
	b, err := p.words(words, f.Addr[0], f.Width())
	if err != nil {
		return Value{}, err
	}
	if f.Type == "string" {
		s := strings.TrimRight(string(b), "\x00 ")
		return Value{Text: strings.TrimLeft(s, "\x00 "), Valid: true}, nil
	}
	return Value{Num: decodeNumber(f.Type, b), Valid: true}, nil
}

// multiBitfield joins several 16-bit registers: Addr[0] holds bits 0–15, Addr[1] 16–31, …
func (p *Profile) multiBitfield(f Field, words map[int]uint16) (Value, error) {
	var v uint64
	for i, a := range f.Addr {
		b, err := p.words(words, a, 1)
		if err != nil {
			return Value{}, err
		}
		v |= uint64(binary.BigEndian.Uint16(b)) << (bitsPerWord * i)
	}
	return Value{Num: float64(v), Valid: true}, nil
}

const (
	bitsPerWord  = 16
	bitsPerByte  = 8
	bytesPerWord = 2
)

func decodeNumber(typ string, b []byte) float64 {
	switch typ {
	case "i16":
		return float64(int16(binary.BigEndian.Uint16(b)))
	case "u32":
		return float64(binary.BigEndian.Uint32(b))
	case "i32":
		return float64(int32(binary.BigEndian.Uint32(b)))
	case "u64":
		return float64(binary.BigEndian.Uint64(b))
	case "f32":
		return float64(math.Float32frombits(binary.BigEndian.Uint32(b)))
	case "f64":
		return math.Float64frombits(binary.BigEndian.Uint64(b))
	}
	return float64(binary.BigEndian.Uint16(b)) // u16, bitfield
}
