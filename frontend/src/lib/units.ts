// The layout's unit: a size is counted in pixels of a 16px base and written in rem, so the
// scale moves the whole window together.

const BASE = 16

export function rem(units: number): string {
  return `${units / BASE}rem`
}

// The inverse of the above: what one pixel of the screen is worth, which a drag delta comes in.
export function unitScale(): number {
  const size = Number.parseFloat(getComputedStyle(document.documentElement).fontSize)
  return Number.isFinite(size) && size > 0 ? size / BASE : 1
}
