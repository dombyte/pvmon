package ws

import (
	"time"

	"github.com/dombyte/pvmon/internal/livecache"
	"github.com/dombyte/pvmon/internal/util"
)

// Message types of the protocol (spec 9.3).
const (
	typeSubscribe   = "subscribe"
	typeUnsubscribe = "unsubscribe"
	typePing        = "ping"
	typePong        = "pong"
	typeSnapshot    = "snapshot"
	typeUpdate      = "update"
	typeDeviceState = "device_state"
	typeStatus      = "status"
	typeSiteChanged = "site_changed"
	typeError       = "error"
)

// Error codes sent in error frames.
const (
	codeUnknownKeys = "unknown_keys"
	codeTooManyKeys = "too_many_keys"
	codeBadMessage  = "invalid_message"
)

// jsonDecimals: JSON rounds to 2 decimals, storage and cache keep full precision.
const jsonDecimals = 2

// inMsg is any message from the client.
type inMsg struct {
	Type string   `json:"type"`
	Keys []string `json:"keys"`
}

// value is one key's value in snapshot and update frames; Value is a number or a text.
type value struct {
	Value  any        `json:"value"`
	Unit   string     `json:"unit,omitempty"`
	Approx bool       `json:"approx,omitempty"`
	TS     *time.Time `json:"ts,omitempty"`
}

type snapshotMsg struct {
	Type   string           `json:"type"`
	Values map[string]value `json:"values"`
}

type updateMsg struct {
	Type    string           `json:"type"`
	TS      time.Time        `json:"ts"`
	Values  map[string]value `json:"values"`
	Removed []string         `json:"removed"`
}

type deviceStateMsg struct {
	Type   string `json:"type"`
	Device string `json:"device"`
	State  string `json:"state"`
}

type siteChangedMsg struct {
	Type      string `json:"type"`
	ConfigRev int64  `json:"config_rev"`
}

type statusMsg struct {
	Type     string `json:"type"`
	Device   string `json:"device"`
	Register string `json:"register"`
	Code     uint64 `json:"code"`
	Level    string `json:"level"`
}

type errorMsg struct {
	Type string   `json:"type"`
	Code string   `json:"code"`
	Keys []string `json:"keys,omitempty"`
}

type simpleMsg struct {
	Type string `json:"type"`
}

// wire converts a cache value; withTS adds its timestamp (snapshots only).
func wire(v livecache.Value, withTS bool) value {
	out := value{Unit: v.Unit, Approx: v.Approx, Value: util.Round(v.Num, jsonDecimals)}
	if v.Text != "" {
		out.Value = v.Text
	}
	if withTS {
		at := v.At
		out.TS = &at
	}
	return out
}

// same reports whether the client already has v (compared at JSON precision).
func same(a, b livecache.Value) bool {
	return util.Round(a.Num, jsonDecimals) == util.Round(b.Num, jsonDecimals) &&
		a.Text == b.Text && a.Unit == b.Unit && a.Approx == b.Approx
}
