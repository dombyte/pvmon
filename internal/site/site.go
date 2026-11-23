// Package site builds the effective site model from configured connections, devices and
// meters plus their profiles: roles, categories, hierarchy, the series catalogue and the
// capabilities the UI is built from (spec 2, 3, 11).
package site

import (
	"time"

	"github.com/dombyte/pvmon/internal/profile"
)

// ParentSite is the parent ID of meters directly under the site.
const ParentSite = "site"

// Spec is the input of Build, mapped by internal/app from the configuration.
type Spec struct {
	Name        string
	Connections map[string]ConnectionSpec
	Devices     map[string]DeviceSpec
	Meters      map[string]MeterSpec
}

// ConnectionSpec is a configured connection.
type ConnectionSpec struct {
	Address string
	Timeout time.Duration
	Serial  Serial
}

// Serial holds RTU line parameters.
type Serial struct {
	Speed    int
	DataBits int
	Parity   string
	StopBits int
}

// DeviceSpec is a configured device; Connection or an inline Address (plus Timeout) is set.
type DeviceSpec struct {
	Connection string
	Address    string
	Unit       int
	Profile    *profile.Loaded
	Interval   time.Duration
	Timeout    time.Duration
	Read       string
	Rollover   string
	Netting    string
	History    []string
	User       string
	Password   string // passed to the transport, never logged
	Insecure   bool
}

// MeterSpec is a configured meter; empty Role and Category come from the profile block.
type MeterSpec struct {
	Device     string
	Source     string
	Role       string
	Category   string
	Parent     string
	Label      string
	KWp        float64
	CapacityWh float64
	Critical   bool
}

// Site is the effective, validated site model.
type Site struct {
	Name        string
	Rev         int64                  // config_rev of the UI configuration the site was built from
	Connections map[string]*Connection // keyed by normalised address
	Devices     map[string]*Device
	Meters      map[string]*Meter
}

// Connection is one physical access path shared by its devices.
type Connection struct {
	Address string
	Scheme  string // tcp | rtu | http | https
	Timeout time.Duration
	Serial  Serial
	Devices []string // sorted device IDs
}

// Device is a polled device with its resolved profile and options.
type Device struct {
	ID         string
	Connection string // Connection address key
	Unit       int
	Profile    *profile.Loaded
	Interval   time.Duration
	Timeout    time.Duration
	Read       string // effective read mode (modbus)
	Rollover   string
	Netting    string // effective netting for its grid meter
	History    []string
	User       string
	Password   string // passed to the transport, never logged
	Insecure   bool
	Meters     []string // sorted meter IDs provided by this device
	Blocks     []string // sorted profile blocks its meters use; only these are polled
}

// Meter is a logical measuring point.
type Meter struct {
	ID         string
	Device     string
	Source     string
	Role       string
	Category   string
	Parent     string
	Label      string
	KWp        float64
	CapacityWh float64
	Critical   bool
	Block      profile.Block
}
