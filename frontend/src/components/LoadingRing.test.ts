import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { createAppI18n } from '@/i18n'
import LoadingRing from './LoadingRing.vue'

describe('LoadingRing', () => {
  it('announces the state and hides the ring from screen readers', () => {
    const w = mount(LoadingRing, { global: { plugins: [createAppI18n('de')] } })
    expect(w.get('[role="status"]').text()).toBe('Wird geladen')
    expect(w.get('svg').attributes('aria-hidden')).toBe('true')
    expect(w.findAll('circle')).toHaveLength(3)
  })
})
