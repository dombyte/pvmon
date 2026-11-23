import { describe, expect, it, vi } from 'vitest'
import { preventZoom } from './noZoom'

function tap(el: Element, at: number, x = 10): TouchEvent {
  const e = new Event('touchend', { bubbles: true, cancelable: true }) as TouchEvent
  Object.defineProperties(e, {
    changedTouches: { value: [{ clientX: x, clientY: 10 }] },
    touches: { value: [] },
    timeStamp: { value: at },
  })
  el.dispatchEvent(e)
  return e
}

describe('preventZoom', () => {
  it('cancels a double tap but still clicks, leaves single taps and inputs alone', () => {
    const doc = document.implementation.createHTMLDocument()
    preventZoom(doc)
    const button = doc.createElement('button')
    const input = doc.createElement('input')
    doc.body.append(button, input)
    const click = vi.fn()
    button.addEventListener('click', click)

    expect(tap(button, 1000).defaultPrevented).toBe(false)
    expect(tap(button, 1200).defaultPrevented).toBe(true)
    expect(click).toHaveBeenCalledOnce()
    expect(tap(button, 2000).defaultPrevented).toBe(false)
    expect(tap(button, 2100, 200).defaultPrevented).toBe(false) // another spot
    tap(input, 3000)
    expect(tap(input, 3100).defaultPrevented).toBe(false)

    const gesture = new Event('gesturestart', { cancelable: true })
    doc.dispatchEvent(gesture)
    expect(gesture.defaultPrevented).toBe(true)
  })
})
