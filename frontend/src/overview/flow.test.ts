import { describe, expect, it } from 'vitest'
import { layout, type Box } from './flow'
import { categories, isConsumer, type NodeKind } from './power'

const widths = [320, 800, 1400]
const order: NodeKind[] = ['house', ...categories]

function subsets<T>(items: T[]): T[][] {
  return items.reduce<T[][]>((acc, i) => acc.concat(acc.map((s) => [...s, i])), [[]])
}

function combos(): NodeKind[][] {
  const out: NodeKind[][] = []
  for (const pv of [false, true])
    for (const battery of [false, true])
      for (const lp of subsets([...categories])) {
        const kinds: NodeKind[] = ['grid']
        if (pv) kinds.unshift('pv')
        if (battery) kinds.push('battery')
        kinds.push('house', ...lp)
        out.push(kinds)
      }
  return out
}

const overlap = (a: Box, b: Box) =>
  a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h
const onBox = (b: Box, [x, y]: [number, number]) => {
  const inX = x >= b.x - 1e-9 && x <= b.x + b.w + 1e-9
  const inY = y >= b.y - 1e-9 && y <= b.y + b.h + 1e-9
  const edge =
    Math.abs(x - b.x) < 1e-9 ||
    Math.abs(x - b.x - b.w) < 1e-9 ||
    Math.abs(y - b.y) < 1e-9 ||
    Math.abs(y - b.y - b.h) < 1e-9
  return inX && inY && edge
}

describe('layout', () => {
  for (const width of widths) {
    describe(`width ${width}`, () => {
      it.each(combos().map((k) => [k.join(','), k] as const))('%s', (_n, kinds) => {
        const l = layout(kinds, width)
        expect(l.width).toBeGreaterThanOrEqual(640)
        expect(l.width).toBe(Math.max(width, 640))
        expect(l.nodes.map((n) => n.kind).sort()).toEqual([...kinds].sort())
        for (const n of l.nodes) {
          expect(n.x).toBeGreaterThanOrEqual(0)
          expect(n.y).toBeGreaterThanOrEqual(0)
          expect(n.x + n.w).toBeLessThanOrEqual(l.width)
          expect(n.y + n.h).toBeLessThanOrEqual(l.height)
        }
        for (let i = 0; i < l.nodes.length; i++)
          for (let j = i + 1; j < l.nodes.length; j++)
            expect(overlap(l.nodes[i]!, l.nodes[j]!)).toBe(false)

        expect(l.edges).toHaveLength(kinds.length)
        for (const e of l.edges) {
          const box = l.nodes.find((n) => n.kind === e.kind)!
          expect(onBox(box, e.points[0]!)).toBe(true)
          const [lx, ly] = e.points[e.points.length - 1]!
          expect(Math.hypot(lx - l.hub.x, ly - l.hub.y)).toBeCloseTo(l.hub.r, 9)
          if (isConsumer(e.kind)) {
            for (let i = 1; i < e.points.length; i++) {
              const [ax, ay] = e.points[i - 1]!
              const [bx, by] = e.points[i]!
              expect(ax === bx || ay === by).toBe(true)
            }
          }
        }

        const rows = l.nodes.filter((n) => isConsumer(n.kind))
        expect(rows.length).toBeLessThanOrEqual(6)
        const kindsTopDown = [...rows].sort((a, b) => a.y - b.y).map((n) => n.kind)
        expect(kindsTopDown).toEqual(order.filter((k) => kinds.includes(k)))
      })
    })
  }

  it('clamps width to 640 and caps consumers at 6 rows', () => {
    expect(layout(['grid', 'house'], 100).width).toBe(640)
    const l = layout(['house', 'wallbox', 'heatpump', 'heater', 'other', 'house'], 800)
    expect(l.nodes.filter((n) => isConsumer(n.kind))).toHaveLength(6)
  })
})
