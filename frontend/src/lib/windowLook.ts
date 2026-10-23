import { rem } from './units'

export function markPlatform(): void {
  if (navigator.userAgent.includes('Mac')) {
    document.documentElement.dataset.mac = '1'
  } else {
    delete document.documentElement.dataset.mac
  }

  if (/Linux|X11/.test(navigator.userAgent)) {
    document.documentElement.dataset.linux = '1'
  } else {
    delete document.documentElement.dataset.linux
  }
}

// The frame's own choices, written on a window's address by Go when it was made: the accent, the
// scale and the code's font. A window that takes its look from here draws itself without asking the
// settings, which is what a window showing a picture and a version wants.

/** The four choices, as the settings carry them. */
export interface WindowLook {
  accent: string
  size: string
  mono: string
  monoSize: number
}

/** Reads the ones this window was opened with; a window opened without them is drawn at the defaults. */
export function lookFromUrl(search: string): Partial<WindowLook> {
  const params = new URLSearchParams(search)
  const monoSize = Number(params.get('monoSize'))
  return {
    accent: params.get('accent') ?? '',
    size: params.get('size') ?? '',
    mono: params.get('mono') ?? '',
    monoSize: Number.isFinite(monoSize) ? monoSize : 0,
  }
}

/** Writes them onto the root element, which is where the stylesheet reads them. The code's size is a
 *  length like every other: it goes in the units the layout counts in, so it grows with the window. */
export function applyLook(look: Partial<WindowLook>): void {
  const root = document.documentElement
  if (look.accent) root.dataset.accent = look.accent
  if (look.size) root.dataset.size = look.size
  if (look.mono) root.dataset.mono = look.mono
  if (look.monoSize) root.style.setProperty('--mono-size', rem(look.monoSize))
}
