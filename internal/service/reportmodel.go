package service

import (
	"time"

	"github.com/dombyte/pvmon/internal/report"
)

// NewReportModel turns a report into its JSON body.
func NewReportModel(r report.Report) ReportModel {
	l := r.Layout
	out := ReportModel{
		From: date(r.Range.From), To: date(r.Range.To), Granularity: r.Granularity.String(),
		TodayExcluded: r.TodayExcluded, Summary: summaryModel(r.Summary, l),
		Figures: figureModels(r.Figures), Buckets: bucketModels(r.Buckets, l),
		PVSources: sectionModels(r.PVSources), Batteries: sectionModels(r.Batteries),
		Profile: profileModel(r.Profile), Patterns: patternsModel(r.Patterns),
		MissingDays: []string{}, Quality: []ReportQuality{},
	}
	if c := r.Comparison; c != nil {
		out.Comparison = &ComparisonModel{
			Mode: c.Mode.String(), From: date(c.Range.From), To: date(c.Range.To),
			Days: c.Summary.Days, CompleteDays: c.Summary.CompleteDays, Matched: c.Matched,
			MatchedDays: c.MatchedDays,
		}
	}
	if r.Loadpoints != nil {
		out.Loadpoints = loadpointsModel(r.Loadpoints)
	}
	for _, d := range r.MissingDays {
		out.MissingDays = append(out.MissingDays, date(d))
	}
	for _, q := range r.Quality {
		out.Quality = append(out.Quality, ReportQuality(q))
	}
	return out
}

func energyKWh(e report.Energy, l report.Layout) EnergyKWh {
	return EnergyKWh{
		PV: someIf(l.HasPV(), e.PV), Consumption: kwh(e.Consumption), Import: kwh(e.Import),
		Export: kwh(e.Export), Charge: someIf(l.HasBattery(), e.Charge),
		Discharge: someIf(l.HasBattery(), e.Discharge),
	}
}

func splitModel(s report.Split) SplitModel {
	return SplitModel{
		SolarKWh: kwh(s.Solar), BatteryKWh: kwh(s.Battery), GridKWh: kwh(s.Grid),
		SolarPct: pct(s.SolarShare), BatteryPct: pct(s.BatteryShare), GridPct: pct(s.GridShare),
	}
}

func summaryModel(s report.Summary, l report.Layout) SummaryModel {
	out := SummaryModel{
		Days: s.Days, CompleteDays: s.CompleteDays, Total: energyKWh(s.Total, l),
		SelfSufficiency: pct(s.SelfSufficiency), SelfConsumption: pct(s.SelfConsumption),
		GridCharge: someIf(l.HasBattery(), s.GridCharge), Sources: splitModel(s.Sources),
		BatteryRatio: round(s.BatteryRatio), BalanceDiff: kwhp(s.BalanceDiff),
		BalanceDiffPct: pct(s.BalanceDiffShare), FallbackDays: s.FallbackDays,
	}
	if s.PerDay != nil {
		pd := energyKWh(*s.PerDay, l)
		out.PerDay = &pd
	}
	if l.HasPV() {
		ps := splitModel(s.PVSplit)
		out.PVSplit = &ps
	}
	return out
}

func figureModels(fs []report.Figure) []FigureModel {
	out := make([]FigureModel, len(fs))
	for i, f := range fs {
		conv := kwhp
		if f.Kind == report.KindRatio {
			conv = pct
		}
		out[i] = FigureModel{
			ID: f.ID, Kind: f.Kind.String(), Better: f.Better.String(), Value: conv(f.Value),
			Compared: conv(f.Compared), Base: conv(f.Base), Change: conv(f.Change),
			ChangePct: pct(f.ChangePct),
		}
	}
	return out
}

func bucketModels(bs []report.Bucket, l report.Layout) []BucketModel {
	out := make([]BucketModel, len(bs))
	for i, b := range bs {
		out[i] = BucketModel{
			Key: b.Key, From: date(b.From), To: date(b.To), Partial: b.Partial,
			Days: b.Summary.Days, CompleteDays: b.Summary.CompleteDays,
			Total: energyKWh(b.Summary.Total, l), SelfSufficiency: pct(b.Summary.SelfSufficiency),
		}
	}
	return out
}

func meterModel(m report.MeterEnergy, days int) ReportMeter {
	return ReportMeter{
		ID: m.ID, Label: m.Label, Category: m.Category, CompleteDays: days,
		InKWh: someIf(m.In != 0, m.In), OutKWh: someIf(m.Out != 0, m.Out),
		SharePct: pct(m.Share), SpecificYield: round(m.SpecificYield), Ratio: round(m.Ratio),
		SolarPct: pct(m.SolarShare), SolarKWh: someIf(m.SolarShare != nil, m.Solar),
	}
}

func sectionModels(s *report.Section) []ReportMeter {
	if s == nil {
		return []ReportMeter{}
	}
	out := make([]ReportMeter, len(s.Meters))
	for i, m := range s.Meters {
		out[i] = meterModel(m, s.CompleteDays)
	}
	return out
}

func loadpointsModel(lp *report.Loadpoints) *LoadpointsModel {
	out := &LoadpointsModel{Meters: sectionModels(&lp.Section), HouseKWh: kwhp(lp.House)}
	if lp.HouseSolarShare != nil {
		out.HouseSolarKWh, out.HouseSolarPct = kwhp(&lp.HouseSolar), pct(lp.HouseSolarShare)
	}
	for _, c := range lp.Categories {
		out.Categories = append(out.Categories, ReportMeter{
			ID: c.Category, Category: c.Category, CompleteDays: lp.CompleteDays,
			InKWh: someIf(true, c.In), SolarKWh: someIf(c.SolarShare != nil, c.Solar),
			SolarPct: pct(c.SolarShare),
		})
	}
	return out
}

func profileModel(p report.Profile) ProfileModel {
	out := ProfileModel{MeanW: make([]*float64, report.SlotsPerDay), PeakW: round(p.Peak)}
	for i, v := range p.Mean {
		out.MeanW[i] = round(v)
	}
	if p.Peak != nil {
		at := p.PeakAt.Format(time.RFC3339)
		out.PeakAt = &at
	}
	return out
}

func dayValue(d *report.DayValue) *DayValueModel {
	if d == nil {
		return nil
	}
	return &DayValueModel{Date: date(d.Date), KWh: kwh(d.Value)}
}

func patternsModel(p report.Patterns) PatternsModel {
	return PatternsModel{
		BestPV: dayValue(p.BestPV), LowestPV: dayValue(p.LowestPV),
		MaxConsumption: dayValue(p.MaxConsumption), MaxImport: dayValue(p.MaxImport),
		WeekdayConsumption: kwhp(p.WeekdayConsumption),
		WeekendConsumption: kwhp(p.WeekendConsumption), LowImportDays: p.LowImportDays,
	}
}
