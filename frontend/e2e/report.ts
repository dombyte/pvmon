// A report answer for the reports page: October 2026, PV, battery and loadpoints.

function energy(pv: number) {
  return {
    pv_kwh: pv,
    consumption_kwh: 308,
    import_kwh: 112,
    export_kwh: pv - 140,
    charge_kwh: 84,
    discharge_kwh: 78.4,
  }
}

function split(a: number, b: number, c: number) {
  const sum = a + b + c
  return {
    solar_est_kwh: a,
    battery_est_kwh: b,
    grid_est_kwh: c,
    solar_pct: (100 * a) / sum,
    battery_pct: (100 * b) / sum,
    grid_pct: (100 * c) / sum,
  }
}

function fig(id: string, kind: string, better: string, value: number, base: number) {
  return {
    id,
    kind,
    better,
    value,
    compared: value,
    base,
    change: value - base,
    change_pct: kind === 'energy' ? (100 * (value - base)) / base : null,
  }
}

function buckets() {
  return Array.from({ length: 31 }, (_, i) => {
    const d = `2026-10-${String(i + 1).padStart(2, '0')}`
    const missing = i === 4 || i === 13
    return {
      key: d,
      from: d,
      to: d,
      partial: false,
      days: 1,
      complete_days: missing ? 0 : 1,
      total: energy(8 + ((i * 3.7) % 14)),
      self_sufficiency_pct: missing ? null : 60 + (i % 7),
    }
  })
}

const meter = (id: string, label: string, extra: Record<string, unknown>) => ({
  id,
  label,
  complete_days: 29,
  in_kwh: null,
  out_kwh: null,
  ...extra,
})

/** The report of the query in url. */
export function reportOf(url: URL) {
  const compare = url.searchParams.get('compare') ?? 'last_year'
  return {
    from: url.searchParams.get('from') ?? '2026-10-01',
    to: url.searchParams.get('to') ?? '2026-10-31',
    granularity: 'day',
    today_excluded: false,
    summary: {
      days: 31,
      complete_days: 29,
      total: energy(414.2),
      self_sufficiency_pct: 63.6,
      self_consumption_pct: 33.8,
      grid_charge_est_kwh: 26.9,
      sources: split(144.5, 51.5, 112),
      pv_split: split(82.9, 57.1, 274.2),
      battery_ratio: 0.93,
      balance_diff_kwh: -61.6,
      balance_diff_pct: -10.2,
      fallback_days: 1,
    },
    figures: [
      fig('pv', 'energy', 'higher', 414.2, 380.5),
      fig('consumption', 'energy', 'neutral', 308, 312),
      fig('self_sufficiency', 'ratio', 'higher', 63.6, 58.1),
      fig('self_consumption', 'ratio', 'higher', 33.8, 35),
      fig('grid_import', 'energy', 'lower', 112, 131),
      fig('grid_export', 'energy', 'neutral', 274.2, 240),
      fig('battery_charge', 'energy', 'neutral', 84, 80),
      fig('battery_discharge', 'energy', 'neutral', 78.4, 75),
    ],
    comparison:
      compare === 'none'
        ? null
        : {
            mode: compare,
            from: '2025-10-01',
            to: '2025-10-31',
            days: 31,
            complete_days: 31,
            matched: true,
            matched_days: 29,
          },
    buckets: buckets(),
    pv_sources: [
      meter('pv-mppt1', 'South', { out_kwh: 260.1, share_pct: 62.8, specific_yield: 43.4 }),
      meter('pv-mppt2', 'West', { out_kwh: 154.1, share_pct: 37.2 }),
    ],
    batteries: [],
    loadpoints: {
      house_kwh: 154,
      house_solar_est_kwh: 67,
      house_solar_pct: 43.51,
      meters: [
        meter('wb-garage', 'Garage', {
          category: 'wallbox',
          in_kwh: 84,
          solar_est_kwh: 44.3,
          solar_pct: 52.7,
        }),
        meter('heatpump', 'Wärmepumpe', {
          category: 'heatpump',
          in_kwh: 70,
          solar_est_kwh: 33.2,
          solar_pct: 47.4,
        }),
      ],
      categories: null,
    },
    profile: { mean_w: [], peak_w: 4200, peak_at: '2026-10-12T18:15:00+02:00' },
    patterns: {
      best_pv: { date: '2026-10-16', kwh: 21.5 },
      lowest_pv: { date: '2026-10-01', kwh: 8 },
      max_consumption: { date: '2026-10-01', kwh: 11 },
      max_import: { date: '2026-10-01', kwh: 4 },
      weekday_consumption_kwh: 10.8,
      weekend_consumption_kwh: 11.6,
      low_import_days: 0,
    },
    missing_days: ['2026-10-05', '2026-10-14'],
    quality: [
      { id: 'grid', label: 'Grid', missing_days: 0, estimated_days: 0 },
      { id: 'pv', label: 'PV', missing_days: 2, estimated_days: 1 },
    ],
  }
}
