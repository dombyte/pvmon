import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { createAppI18n } from '@/i18n'
import { createAppRouter } from '@/router'
import LastDays from './LastDays.vue'
import type { DayPoint } from './trend'

const point = (date: string, pv: number | null, running = false): DayPoint => ({
  date,
  running,
  pv,
  consumption: 10000,
  gridIn: 6100,
  gridOut: 300,
  selfSufficiency: pv === null ? null : 52,
})

function render(points: DayPoint[]) {
  return mount(LastDays, {
    props: { points, keys: ['pv', 'consumption'] },
    global: { plugins: [createAppI18n('de'), createAppRouter()] },
  })
}

describe('LastDays', () => {
  const points = [point('2026-11-19', 2500, true), point('2026-11-18', 5000), point('2026-11-17', null)]

  it('lists every day with bars scaled to the largest value', () => {
    const w = render(points)
    const rows = w.findAll('li')
    expect(rows).toHaveLength(3)
    expect(w.find('h2').text()).toBe('Letzte 3 Tage')
    expect(rows[0]?.text()).toContain('Heute (läuft)')
    expect(rows[1]?.text()).toContain('Mi., 18. Nov.')
    expect(rows[1]?.text()).toContain('Autarkie 52')
    const widths = rows[1]?.findAll('span.block').map((s) => s.attributes('style'))
    expect(widths?.[0]).toContain('width: 50%')
    expect(widths?.[1]).toContain('width: 100%')
    // A missing day has an empty track and no value, never 0 kWh.
    expect(rows[2]?.find('.border-dashed').exists()).toBe(false)
    expect(rows[2]?.text()).toContain('–')
  })

  it('expands one day; only finished days offer the report', async () => {
    const w = render(points)
    const buttons = w.findAll('button')
    await buttons[1]?.trigger('click')
    expect(buttons[1]?.attributes('aria-expanded')).toBe('true')
    const link = w.find('a')
    expect(link.text()).toBe('Tagesreport öffnen')
    expect(link.attributes('href')).toBe('/reports?from=2026-11-18&to=2026-11-18')
    expect(w.text()).toContain('Netzbezug 6,1')
    await buttons[0]?.trigger('click')
    expect(buttons[1]?.attributes('aria-expanded')).toBe('false')
    expect(w.find('a').exists()).toBe(false)
    expect(w.text()).toContain('Einspeisung 0,3')
  })
})
