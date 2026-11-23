import { inject, provide, ref, type InjectionKey, type Ref } from 'vue'
import { errorKey, probeDevice } from '@/api/client'
import type { ConfigObject, ProbeResult, ProfileInfo } from '@/api/types'
import {
  changesOf,
  draftOf,
  proposeMeters,
  removeDevice,
  roleOf,
  upsert,
  type BlockInfo,
  type DraftObject,
} from './draft'

/** The state the wizard steps share (spec 11.14). */
export interface Wizard {
  orig: Ref<ConfigObject[]>
  draft: Ref<DraftObject[]>
  profiles: Ref<ProfileInfo[]>
  /** The newest probe per device; an error key when the request itself failed. */
  probes: Ref<Record<string, ProbeResult | { failed: string }>>
  blocksOf: (device: string) => BlockInfo[]
  /** A meter's role, from the profile when the meter names none. */
  roleOf: (meter: DraftObject) => string
  apply: (objs: DraftObject[], probe?: ProbeResult) => void
  /** Adds a meter for each of the device's blocks. */
  propose: (device: string, blocks: BlockInfo[]) => void
  remove: (device: string) => void
  setMeter: (m: DraftObject) => void
  dropMeter: (id: string) => void
  probe: (device: string) => Promise<void>
  reset: (objects: ConfigObject[]) => void
}

const key: InjectionKey<Wizard> = Symbol('wizard')

/** The probe of a device when it answered the request (it may still report an error). */
export function probeOf(w: Wizard, device: string): ProbeResult | undefined {
  const p = w.probes.value[device]
  return p && 'blocks' in p ? p : undefined
}

export function createWizard(): Wizard {
  const orig = ref<ConfigObject[]>([])
  const draft = ref<DraftObject[]>([])
  const profiles = ref<ProfileInfo[]>([])
  const probes = ref<Record<string, ProbeResult | { failed: string }>>({})

  function blocksOf(device: string): BlockInfo[] {
    const dev = draft.value.find((o) => o.kind === 'device' && o.id === device)
    const p = profiles.value.find((x) => x.id === dev?.body.profile)
    if (p) return p.blocks
    const probed = probes.value[device]
    if (probed && 'blocks' in probed) {
      return probed.blocks.map((b) => ({
        ...b,
        channels: b.channels.map((c) => c.name),
      }))
    }
    return []
  }

  /** Adds the meters proposed for blocks (all, or only the suggested ones) and turns an
   * inverter's grid meter diagnostic when a dedicated one arrives. */
  function propose(device: string, blocks: BlockInfo[], all: boolean): void {
    const { proposals, demote } = proposeMeters(device, blocks, {
      draft: draft.value,
      probe: probeOf(w, device),
      blocksOf,
      saved: (id) => orig.value.some((o) => o.kind === 'meter' && o.id === id),
    })
    const gone = new Set(demote.map((d) => d.from))
    const kept = draft.value.filter((o) => !(o.kind === 'meter' && gone.has(o.id)))
    const meters = proposals.filter((p) => all || p.include).map((p) => p.meter)
    draft.value = upsert(kept, [...demote.map((d) => d.meter), ...meters])
  }

  const w: Wizard = {
    orig,
    draft,
    profiles,
    probes,
    blocksOf,
    roleOf: (m) => roleOf(m, blocksOf),
    /** Adds or replaces a device; a new one gets the proposed meters at once. */
    apply(objs, probe) {
      const dev = objs.find((o) => o.kind === 'device')
      if (!dev) return
      const isNew = !draft.value.some((o) => o.kind === 'device' && o.id === dev.id)
      draft.value = upsert(draft.value, objs)
      if (probe) probes.value = { ...probes.value, [dev.id]: probe }
      if (isNew) propose(dev.id, blocksOf(dev.id), false)
    },
    propose: (device, blocks) => propose(device, blocks, true),
    remove(device) {
      draft.value = removeDevice(draft.value, device)
    },
    setMeter(m) {
      draft.value = upsert(draft.value, [m])
    },
    dropMeter(id) {
      draft.value = draft.value.filter((o) => !(o.kind === 'meter' && o.id === id))
    },
    /** Probes a device of the draft with its pending changes. */
    async probe(device) {
      const mine = changesOf(orig.value, draft.value).filter(
        (c) => c.kind !== 'meter' && (c.id === device || c.kind === 'connection'),
      )
      try {
        const res = await probeDevice(mine, device)
        probes.value = { ...probes.value, [device]: res }
      } catch (err) {
        probes.value = { ...probes.value, [device]: { failed: errorKey(err) } }
      }
    },
    reset(objects) {
      orig.value = objects
      draft.value = draftOf(objects)
      probes.value = {}
    },
  }
  provide(key, w)
  return w
}

export function useWizard(): Wizard {
  const w = inject(key)
  if (!w) throw new Error('useWizard outside the wizard')
  return w
}
