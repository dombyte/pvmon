package configstore

import "time"

// ObjectView is one object as the settings API shows it (secrets masked).
type ObjectView struct {
	Kind       string         `json:"kind"`
	ID         string         `json:"id"`
	Locked     bool           `json:"locked"` // defined in config.yaml or env: read-only
	Body       map[string]any `json:"body"`
	UpdatedAt  *time.Time     `json:"updated_at"`
	UpdatedBy  string         `json:"updated_by,omitempty"`
	Unreadable []string       `json:"unreadable,omitempty"` // secrets to enter again
}

// SettingView is one UI setting with its effective value.
type SettingView struct {
	Value  any  `json:"value"`
	Locked bool `json:"locked"` // set in config.yaml or env
	Stored bool `json:"stored"` // set in the UI configuration (else default or file)
}

// Overview is the whole configuration as the settings pages need it.
type Overview struct {
	Rev      int64                  `json:"config_rev"`
	Objects  []ObjectView           `json:"objects"`
	Settings map[string]SettingView `json:"settings"`
}

// View returns the view of a stored object.
func (sv *Service) View(k Key, o Object) ObjectView {
	at := o.UpdatedAt
	return ObjectView{
		Kind: k.Kind, ID: k.ID, Body: sv.Masked(k.Kind, o), UpdatedAt: &at,
		UpdatedBy: o.UpdatedBy, Unreadable: o.Unreadable,
	}
}
