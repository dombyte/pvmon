package service

import (
	"time"

	"github.com/dombyte/pvmon/internal/util"
)

// Report JSON (spec 9.1, 12): energy in kWh, shares in percent, 2 decimals; null = no
// data. Fields ending in _est_kwh are this app's estimates (≈).

const (
	whPerKWh = 1000
	hundred  = 100
)

// ReportModel is the body of GET /api/v1/report?format=json.
type ReportModel struct {
	From          string           `json:"from"`
	To            string           `json:"to"`
	Granularity   string           `json:"granularity"`
	TodayExcluded bool             `json:"today_excluded"`
	Summary       SummaryModel     `json:"summary"`
	Figures       []FigureModel    `json:"figures"`
	Comparison    *ComparisonModel `json:"comparison"`
	Buckets       []BucketModel    `json:"buckets"`
	PVSources     []ReportMeter    `json:"pv_sources"`
	Batteries     []ReportMeter    `json:"batteries"`
	Loadpoints    *LoadpointsModel `json:"loadpoints"`
	Profile       ProfileModel     `json:"profile"`
	Patterns      PatternsModel    `json:"patterns"`
	MissingDays   []string         `json:"missing_days"`
	Quality       []ReportQuality  `json:"quality"`
}

// EnergyKWh are the site quantities in kWh; absent quantities are null.
type EnergyKWh struct {
	PV          *float64 `json:"pv_kwh"`
	Consumption float64  `json:"consumption_kwh"`
	Import      float64  `json:"import_kwh"`
	Export      float64  `json:"export_kwh"`
	Charge      *float64 `json:"charge_kwh"`
	Discharge   *float64 `json:"discharge_kwh"`
}

// SplitModel is a split into solar, battery and grid (≈).
type SplitModel struct {
	SolarKWh   float64  `json:"solar_est_kwh"`
	BatteryKWh float64  `json:"battery_est_kwh"`
	GridKWh    float64  `json:"grid_est_kwh"`
	SolarPct   *float64 `json:"solar_pct"`
	BatteryPct *float64 `json:"battery_pct"`
	GridPct    *float64 `json:"grid_pct"`
}

// SummaryModel is the summary of the complete days.
type SummaryModel struct {
	Days            int         `json:"days"`
	CompleteDays    int         `json:"complete_days"`
	Total           EnergyKWh   `json:"total"`
	PerDay          *EnergyKWh  `json:"per_day"`
	SelfSufficiency *float64    `json:"self_sufficiency_pct"`
	SelfConsumption *float64    `json:"self_consumption_pct"`
	GridCharge      *float64    `json:"grid_charge_est_kwh"`
	Sources         SplitModel  `json:"sources"`
	PVSplit         *SplitModel `json:"pv_split"`
	BatteryRatio    *float64    `json:"battery_ratio"`
	BalanceDiff     *float64    `json:"balance_diff_kwh"`
	BalanceDiffPct  *float64    `json:"balance_diff_pct"`
	FallbackDays    int         `json:"fallback_days"`
}

// FigureModel is a key figure: energy in kWh (change in kWh and %), ratios in percent
// (change in points).
type FigureModel struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Better    string   `json:"better"`
	Value     *float64 `json:"value"`
	Compared  *float64 `json:"compared"`
	Base      *float64 `json:"base"`
	Change    *float64 `json:"change"`
	ChangePct *float64 `json:"change_pct"`
}

// ComparisonModel is the comparison period.
type ComparisonModel struct {
	Mode         string `json:"mode"`
	From         string `json:"from"`
	To           string `json:"to"`
	Days         int    `json:"days"`
	CompleteDays int    `json:"complete_days"`
	Matched      bool   `json:"matched"`
	MatchedDays  int    `json:"matched_days"`
}

// BucketModel is one day, month or year of the range.
type BucketModel struct {
	Key             string    `json:"key"`
	From            string    `json:"from"`
	To              string    `json:"to"`
	Partial         bool      `json:"partial"`
	Days            int       `json:"days"`
	CompleteDays    int       `json:"complete_days"`
	Total           EnergyKWh `json:"total"`
	SelfSufficiency *float64  `json:"self_sufficiency_pct"`
}

// ReportMeter is one PV source, battery or loadpoint.
type ReportMeter struct {
	ID            string   `json:"id"`
	Label         string   `json:"label"`
	Category      string   `json:"category,omitempty"`
	CompleteDays  int      `json:"complete_days"`
	InKWh         *float64 `json:"in_kwh"`
	OutKWh        *float64 `json:"out_kwh"`
	SharePct      *float64 `json:"share_pct,omitempty"`
	SpecificYield *float64 `json:"specific_yield,omitempty"`
	Ratio         *float64 `json:"ratio,omitempty"`
	SolarKWh      *float64 `json:"solar_est_kwh,omitempty"`
	SolarPct      *float64 `json:"solar_pct,omitempty"`
}

// LoadpointsModel lists loadpoints and their categories.
type LoadpointsModel struct {
	HouseSolarKWh *float64      `json:"house_solar_est_kwh"`
	HouseSolarPct *float64      `json:"house_solar_pct"`
	HouseKWh      *float64      `json:"house_kwh"`
	Meters        []ReportMeter `json:"meters"`
	Categories    []ReportMeter `json:"categories"`
}

// ProfileModel is the load profile: mean W per 15-minute slot of day, peak 15-min mean.
type ProfileModel struct {
	MeanW  []*float64 `json:"mean_w"`
	PeakW  *float64   `json:"peak_w"`
	PeakAt *string    `json:"peak_at"`
}

// DayValueModel is a day and its energy.
type DayValueModel struct {
	Date string  `json:"date"`
	KWh  float64 `json:"kwh"`
}

// PatternsModel are the day-level observations.
type PatternsModel struct {
	BestPV             *DayValueModel `json:"best_pv"`
	LowestPV           *DayValueModel `json:"lowest_pv"`
	MaxConsumption     *DayValueModel `json:"max_consumption"`
	MaxImport          *DayValueModel `json:"max_import"`
	WeekdayConsumption *float64       `json:"weekday_consumption_kwh"`
	WeekendConsumption *float64       `json:"weekend_consumption_kwh"`
	LowImportDays      int            `json:"low_import_days"`
}

// ReportQuality is one meter's data quality.
type ReportQuality struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	MissingDays   int    `json:"missing_days"`
	EstimatedDays int    `json:"estimated_days"`
}

func kwh(wh float64) float64 { return util.Round(wh/whPerKWh, decimals) }

func kwhp(wh *float64) *float64 {
	if wh == nil {
		return nil
	}
	v := kwh(*wh)
	return &v
}

func pct(f *float64) *float64 {
	if f == nil {
		return nil
	}
	v := util.Round(*f*hundred, decimals)
	return &v
}

func round(f *float64) *float64 {
	if f == nil {
		return nil
	}
	v := util.Round(*f, decimals)
	return &v
}

func date(t time.Time) string { return t.Format(time.DateOnly) }

// someIf returns &kwh(v) when ok, else nil.
func someIf(ok bool, wh float64) *float64 {
	if !ok {
		return nil
	}
	v := kwh(wh)
	return &v
}
