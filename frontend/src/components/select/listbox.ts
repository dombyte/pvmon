// Keyboard model of the themed select (WAI-ARIA listbox): pure, so it is tested without a DOM.

/** The option index a key moves to, or null when the key does not move. */
export function moveIndex(i: number, key: string, n: number): number | null {
  if (n === 0) return null
  switch (key) {
    case 'ArrowDown':
      return Math.min(i + 1, n - 1)
    case 'ArrowUp':
      return Math.max(i - 1, 0)
    case 'Home':
    case 'PageUp':
      return 0
    case 'End':
    case 'PageDown':
      return n - 1
    default:
      return null
  }
}

/** First option whose label starts with the typed text (type-ahead), searching after i. */
export function typeAhead(labels: string[], typed: string, i: number): number | null {
  const q = typed.toLocaleLowerCase()
  if (!q) return null
  const order = [...labels.keys()].map((k) => (k + i + 1) % labels.length)
  return order.find((k) => labels[k]?.toLocaleLowerCase().startsWith(q)) ?? null
}

/** Open the list upwards when it does not fit below the button but fits better above. */
export function opensUp(rect: { top: number; bottom: number }, listHeight: number, viewport: number) {
  const below = viewport - rect.bottom
  return below < listHeight && rect.top > below
}
