package profile

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"strconv"
)

// Encode builds raw poll data from normalised channel values keyed "block.channel"; it is
// the inverse of the decoder for simulations and tests. Only channels with a value are
// written (blocks may share registers, e.g. one meter seen as grid or as pv); registers
// of the profile without a value stay 0. Composite channels (product, derive) are
// skipped, a sum is written to its first path and value_id channels get matching ID and
// value arrays.
func (p *Profile) Encode(values map[string]float64) (Sample, error) {
	if p.Transport == TransportModbus {
		return Sample{Words: p.encodeWords(values)}, nil
	}
	docs := map[string]any{}
	p.eachValue(values, func(c Channel, v float64) {
		encodeJSON(docs, c, denormalise(c, signed(c, v)))
	})
	for _, name := range sortedKeys(p.Status) {
		s := p.Status[name]
		if s.Path != "" {
			setPath(docs, s.Endpoint, s.Path, p.statusOK(s))
		}
	}
	return marshalDocs(docs)
}

func marshalDocs(docs map[string]any) (Sample, error) {
	out := Sample{JSON: map[string]json.RawMessage{}}
	for id, doc := range docs {
		b, err := json.Marshal(doc)
		if err != nil {
			return Sample{}, err //nolint:wrapcheck // only NaN can fail; callers pass numbers
		}
		out.JSON[id] = b
	}
	return out, nil
}

// statusOK returns the JSON value of a healthy status.
func (p *Profile) statusOK(s Status) any {
	switch {
	case s.Type == "list":
		return []any{}
	case s.False != "":
		return true
	}
	return p.okCode(s)
}

// okCode returns the smallest code a status map maps to ok (0 for bitfields and others).
func (p *Profile) okCode(s Status) float64 {
	m, ok := p.Maps[s.Map]
	if !ok || len(m.Bits) > 0 {
		return 0
	}
	best, found := uint64(0), false
	for code, e := range m.Codes {
		if e.Level == LevelOK && (!found || code < best) {
			best, found = code, true
		}
	}
	return float64(best)
}

// signed undoes invert; sign_from magnitudes are handled by the encoders.
func signed(c Channel, v float64) float64 {
	if c.Invert {
		return -v
	}
	return v
}

// denormalise turns a normalised value back into the raw device value.
func denormalise(c Channel, v float64) float64 {
	if f := energyUnits()[c.Unit]; f > 0 {
		v /= f
	} else if f := powerUnits()[c.Unit]; f > 0 {
		v /= f
	}
	return (v - c.Offset) / c.ScaleOr1()
}

func (p *Profile) encodeWords(values map[string]float64) map[int]uint16 {
	words := map[int]uint16{}
	for _, f := range p.fields() {
		for _, a := range f.field.Addr {
			p.putWords(words, a, f.field, 0)
		}
	}
	for _, s := range p.Status {
		if len(s.Addr) > 0 {
			p.putWords(words, s.Addr[0], s.Field, p.okCode(s))
		}
	}
	p.eachValue(values, func(c Channel, v float64) { p.encodeChannel(words, c, v) })
	return words
}

// eachValue calls f for every channel with a value, in block and channel order.
func (p *Profile) eachValue(values map[string]float64, f func(Channel, float64)) {
	for _, bn := range sortedKeys(p.Blocks) {
		for _, cn := range sortedKeys(p.Blocks[bn].Channels) {
			if v, ok := values[bn+"."+cn]; ok {
				f(p.Blocks[bn].Channels[cn], v)
			}
		}
	}
}

func (p *Profile) encodeChannel(words map[int]uint16, c Channel, v float64) {
	if len(c.Addr) == 0 {
		return
	}
	v = signed(c, v)
	if c.SignFrom != nil {
		p.putWords(words, c.SignFrom.Addr[0], c.SignFrom.Field, direction(c, v))
		v = math.Abs(v)
	}
	p.putWords(words, c.Addr[0], c.Field, denormalise(c, v))
}

func direction(c Channel, v float64) float64 {
	if v > 0 {
		return float64(c.SignFrom.DischargeWhen)
	}
	if c.SignFrom.DischargeWhen == 0 {
		return 1
	}
	return 0
}

// putWords writes v as the field's type at addr in the profile's byte and word order.
func (p *Profile) putWords(words map[int]uint16, addr int, f Field, v float64) {
	b := encodeNumber(f.Type, v, f.Width())
	if p.Modbus.WordOrder == "little" {
		reverseWords(b)
	}
	for i := 0; i+1 < len(b); i += bytesPerWord {
		w := binary.BigEndian.Uint16(b[i:])
		if p.Modbus.ByteOrder == "little" {
			w = w<<bitsPerByte | w>>bitsPerByte
		}
		words[addr+i/bytesPerWord] = w
	}
}

func encodeNumber(typ string, v float64, width int) []byte {
	r := math.Round(v)
	switch typ {
	case "f32":
		return binary.BigEndian.AppendUint32(nil, math.Float32bits(float32(v)))
	case "f64":
		return binary.BigEndian.AppendUint64(nil, math.Float64bits(v))
	case "u32", "i32":
		return binary.BigEndian.AppendUint32(nil, uint32(int64(r)))
	case "u64":
		return binary.BigEndian.AppendUint64(nil, uint64(r))
	case "string":
		return make([]byte, bytesPerWord*width)
	}
	return binary.BigEndian.AppendUint16(nil, uint16(int64(r)))
}

func encodeJSON(docs map[string]any, c Channel, v float64) {
	switch {
	case c.ValueID != nil:
		appendValueID(docs, c, v)
	case len(c.Sum) > 0:
		setPath(docs, c.Endpoint, c.Sum[0], v)
		for _, path := range c.Sum[1:] {
			setPath(docs, c.Endpoint, path, 0.0)
		}
	case c.Path != "":
		setPath(docs, c.Endpoint, c.Path, v)
	}
}

func appendValueID(docs map[string]any, c Channel, v float64) {
	ids, ok := docs[c.ValueID.IDs].([]any)
	if !ok {
		ids = nil
	}
	vals, ok := docs[c.Endpoint].([]any)
	if !ok {
		vals = nil
	}
	docs[c.ValueID.IDs] = append(ids, float64(c.ValueID.ID))
	docs[c.Endpoint] = append(vals, v)
}

// setPath stores v at a JSONPath of the subset, creating objects and arrays as needed.
func setPath(docs map[string]any, endpoint, path string, v any) {
	toks := pathTokens(path)
	if len(toks) == 0 {
		return
	}
	docs[endpoint] = setIn(docs[endpoint], toks, v)
}

func setIn(cur any, toks []string, v any) any {
	if len(toks) == 0 {
		return v
	}
	tok := toks[0]
	if tok[0] == '[' {
		i, err := strconv.Atoi(tok[1 : len(tok)-1])
		if err != nil {
			return cur // ValidPath guarantees digits
		}
		arr, ok := cur.([]any)
		if !ok {
			arr = nil
		}
		for len(arr) <= i {
			arr = append(arr, 0.0)
		}
		arr[i] = setIn(arr[i], toks[1:], v)
		return arr
	}
	obj, ok := cur.(map[string]any)
	if !ok {
		obj = map[string]any{}
	}
	obj[tok] = setIn(obj[tok], toks[1:], v)
	return obj
}
