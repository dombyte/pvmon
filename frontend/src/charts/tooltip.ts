// HTML pieces of the chart tooltips (spec 11.7): every row carries the colour dot of its
// series, so a value can be matched to its line or bar.

export function escapeHTML(s: string): string {
  return s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`)
}

/** One tooltip row: colour dot (none for derived values), name, value. */
export function tooltipRow(name: string, value: string, color?: string): string {
  const dot = color
    ? `<span class="size-2.5 shrink-0 rounded-full" style="background:${escapeHTML(color)}"></span>`
    : '<span class="size-2.5 shrink-0"></span>'
  return (
    `<div class="flex items-center gap-2">${dot}<span class="flex-1">${escapeHTML(name)}</span>` +
    `<span class="ml-4 tabular-nums">${escapeHTML(value)}</span></div>`
  )
}

/** Tooltip body: a bold title, then the rows (already HTML). */
export function tooltipBody(title: string, rows: string[], extra = ''): string {
  return (
    `<div class="min-w-44"><p class="mb-1 font-semibold">${escapeHTML(title)}</p>` +
    `${rows.join('')}${extra}</div>`
  )
}
