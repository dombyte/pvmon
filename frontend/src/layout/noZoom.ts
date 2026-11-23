// No page zoom on phones (spec 11.2). CSS touch-action covers Chromium; iOS Safari still
// zooms on pinch and on a double tap, so both gestures are cancelled here. A cancelled
// second tap still reaches its control as a click, so fast tapping keeps working.

const doubleTapMs = 300
const sameSpotPx = 24
const typing = new Set(['INPUT', 'TEXTAREA', 'SELECT'])

export function preventZoom(doc: Document): void {
  doc.addEventListener('gesturestart', (e) => e.preventDefault(), { passive: false })
  let last = { at: 0, x: 0, y: 0 }
  doc.addEventListener(
    'touchend',
    (e) => {
      const touch = e.changedTouches[0]
      if (!touch || e.touches.length > 0) return
      const tap = { at: e.timeStamp, x: touch.clientX, y: touch.clientY }
      const double =
        tap.at - last.at < doubleTapMs &&
        Math.abs(tap.x - last.x) < sameSpotPx &&
        Math.abs(tap.y - last.y) < sameSpotPx
      last = tap
      const target = e.target
      if (!double || !(target instanceof Element) || typing.has(target.tagName)) return
      e.preventDefault()
      target.dispatchEvent(
        new MouseEvent('click', { bubbles: true, cancelable: true, view: doc.defaultView }),
      )
    },
    { passive: false },
  )
}
