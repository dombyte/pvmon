// Package simulate provides fake device sources for "pvmon simulate": a small, deterministic
// household model whose values are encoded through the real profiles, so the whole pipeline
// runs as with real devices (spec 13).
package simulate

import (
	"math"
	"time"

	"github.com/dombyte/pvmon/internal/profile"
)

// Model constants of the simulated household.
const (
	peakPV      = 6000.0 // W at noon
	baseLoad    = 450.0  // W
	chargePower = 11000.0
	heatPump    = 1200.0 // W while running
	backupLoad  = 120.0  // W of the house load sits on the inverter's backup output
	battPower   = 1500.0
	sunrise     = 7.0
	daylight    = 10.0
	lifetimeWh  = 1_000_000.0 // initial lifetime counter reading
	hoursPerDay = 24
)

// Powers are the instantaneous powers of the household by role.
type Powers struct {
	// W; battery + = discharging, grid + = import. House is the whole house load,
	// Backup the part of it on the inverter's backup output.
	PV, Battery, Grid, House, Backup, Wallbox, HeatPump float64
}

// Daily windows of the model, in local hours.
const (
	chargeFrom, chargeTo       = 11.0, 14.0
	dischargeFrom, dischargeTo = 19.0, 23.0
	wallboxFrom, wallboxTo     = 18.0, 20.0
	heatMorning, heatEvening   = 5.0, 21.0 // heat pump runs two hours from each
	heatHours                  = 2.0
	loadWave                   = 150.0
)

// PowersAt returns the household powers at local time t.
func PowersAt(t time.Time) Powers {
	h := float64(t.Hour()) + t.Sub(t.Truncate(time.Hour)).Hours()
	p := Powers{PV: pvPower(h), Battery: batteryPower(h)}
	p.House = baseLoad + loadWave*math.Sin(h/hoursPerDay*2*math.Pi)
	p.Backup = backupLoad
	if h >= wallboxFrom && h < wallboxTo {
		p.Wallbox = chargePower
	}
	if within(h, heatMorning, heatHours) || within(h, heatEvening, heatHours) {
		p.HeatPump = heatPump
	}
	p.Grid = p.House + p.Wallbox + p.HeatPump - p.PV - p.Battery
	return p
}

func within(h, from, hours float64) bool { return h >= from && h < from+hours }

func pvPower(h float64) float64 {
	if h <= sunrise || h >= sunrise+daylight {
		return 0
	}
	return peakPV * math.Sin(math.Pi*(h-sunrise)/daylight)
}

// batteryPower charges around noon and discharges at half power in the evening.
func batteryPower(h float64) float64 {
	switch {
	case h >= chargeFrom && h < chargeTo:
		return -battPower
	case h >= dischargeFrom && h < dischargeTo:
		return battPower / 2 //nolint:mnd // half power
	}
	return 0
}

// counter keeps one simulated energy counter.
type counter struct {
	wh    float64
	daily bool
	day   int
}

func (c *counter) add(wh float64, t time.Time) float64 {
	if c.daily && c.day != t.YearDay() {
		c.wh, c.day = 0, t.YearDay()
	}
	c.wh += max(wh, 0)
	return c.wh
}

// powerFor maps a role (and consumer category) to its simulated power.
func powerFor(p Powers, role, category string) float64 {
	switch role {
	case profile.RolePV:
		return p.PV
	case profile.RoleBattery:
		return p.Battery
	case profile.RoleGrid, profile.RoleInverter:
		return p.Grid
	case profile.RoleConsumer:
		return consumerPower(p, category)
	case profile.RoleBackup:
		return p.Backup
	case profile.RolePhase:
		return p.Grid / 3 //nolint:mnd // three phases
	}
	return p.House
}

// consumerPower is the household power of a consumer category; others follow the house.
func consumerPower(p Powers, category string) float64 {
	switch category {
	case "wallbox":
		return p.Wallbox
	case "heatpump":
		return p.HeatPump
	}
	return p.House
}
