// Geometry of the desktop energy flow (spec 11.5). Pure: capabilities and width in,
// boxes and polylines out. PV top, grid left, battery bottom, consumers in a right column
// hanging off a vertical bus, so edges never cross.
import { isConsumer, type NodeKind } from './power'

export interface Box {
  kind: NodeKind
  x: number
  y: number
  w: number
  h: number
}

export interface Edge {
  kind: NodeKind
  /** Polyline from the node to the hub. */
  points: [number, number][]
}

export interface FlowLayout {
  width: number
  height: number
  hub: { x: number; y: number; r: number }
  nodes: Box[]
  edges: Edge[]
}

const minWidth = 640
const nodeH = 76
const nodeW = 210
const rowGap = 20
const hubR = 22
const vGap = 70
const busGap = 36
const maxRows = 6

/** Layout for the given node kinds (already filtered by capabilities) and card width. */
export function layout(kinds: NodeKind[], width: number): FlowLayout {
  const w = Math.max(width, minWidth)
  const consumers = kinds.filter(isConsumer).slice(0, maxRows)
  const columnH = consumers.length * nodeH + (consumers.length - 1) * rowGap
  const crossH = 2 * nodeH + 2 * vGap
  const height = Math.max(columnH, crossH)
  const hub = { x: Math.round(w * 0.42), y: height / 2, r: hubR }
  const nodes: Box[] = []
  const edges: Edge[] = []
  const has = (k: NodeKind) => kinds.includes(k)
  if (has('pv')) {
    nodes.push({ kind: 'pv', x: hub.x - nodeW / 2, y: 0, w: nodeW, h: nodeH })
    edges.push({ kind: 'pv', points: [[hub.x, nodeH], [hub.x, hub.y - hubR]] })
  }
  if (has('grid')) {
    nodes.push({ kind: 'grid', x: 0, y: hub.y - nodeH / 2, w: nodeW, h: nodeH })
    edges.push({ kind: 'grid', points: [[nodeW, hub.y], [hub.x - hubR, hub.y]] })
  }
  if (has('battery')) {
    const y = height - nodeH
    nodes.push({ kind: 'battery', x: hub.x - nodeW / 2, y, w: nodeW, h: nodeH })
    edges.push({ kind: 'battery', points: [[hub.x, y], [hub.x, hub.y + hubR]] })
  }
  const colX = w - nodeW
  const busX = colX - busGap
  const top = (height - columnH) / 2
  consumers.forEach((kind, i) => {
    const y = top + i * (nodeH + rowGap)
    const cy = y + nodeH / 2
    nodes.push({ kind, x: colX, y, w: nodeW, h: nodeH })
    edges.push({
      kind,
      points: [[colX, cy], [busX, cy], [busX, hub.y], [hub.x + hubR, hub.y]],
    })
  })
  return { width: w, height, hub, nodes, edges }
}
