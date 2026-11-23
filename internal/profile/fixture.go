package profile

import (
	"encoding/json"
	"fmt"
	"time"
)

// Fixture is a recorded or documentation-based sequence of raw polls of one device. It is
// the output format of "pvmon dump" and the input of golden and replay tests.
type Fixture struct {
	Profile  string    `json:"profile"`
	Version  int       `json:"version"`            // profile version the fixture was made with
	Source   string    `json:"source"`             // dump | documentation
	Interval string    `json:"interval,omitempty"` // poll interval of a dump
	Samples  []Sample  `json:"samples"`
	Created  time.Time `json:"created,omitzero"`
}

// Sample is one poll: Modbus words by address or JSON bodies by endpoint ID. Err is set
// for a failed poll so replays see gaps.
type Sample struct {
	At    time.Time                  `json:"at"`
	Words map[int]uint16             `json:"words,omitempty"`
	JSON  map[string]json.RawMessage `json:"json,omitempty"`
	Err   string                     `json:"err,omitempty"`
}

// Raw decodes the sample into the decoder input.
func (s Sample) Raw() (Raw, error) {
	raw := Raw{Words: s.Words, JSON: make(map[string]any, len(s.JSON))}
	for id, body := range s.JSON {
		var v any
		if err := json.Unmarshal(body, &v); err != nil {
			return Raw{}, fmt.Errorf("profile: sample endpoint %s: %w", id, err)
		}
		raw.JSON[id] = v
	}
	return raw, nil
}

// Golden holds the expected normalised values per sample of a fixture: channels as
// "block.channel" → value (numbers, or strings for enum/string channels) and status as
// name → level.
type Golden struct {
	Samples []GoldenSample `json:"samples"`
}

// GoldenSample is the expectation for one sample.
type GoldenSample struct {
	Channels map[string]any    `json:"channels"`
	Status   map[string]string `json:"status"`
}
