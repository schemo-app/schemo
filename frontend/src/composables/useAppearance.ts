import { watch } from 'vue'
import { useSettings } from './useSettings'
import { applyLook } from '../lib/windowLook'

// The appearance as attributes on the root element, where the stylesheet reads them. Every value
// but the defaults has a rule, so what is written here is written whole. The writing itself is
// `lib/windowLook`'s: this is the window that has the settings, and one that has them on its
// address uses the same two lines without this.

/** Called once per window, at its boot. */
export function useAppearance(): void {
  const { settings } = useSettings()

  watch(
    () => settings.value?.appearance,
    (held) => {
      if (!held) return
      applyLook({
        accent: held.accent,
        size: held.size,
        mono: held.monoFont,
        monoSize: held.monoSize,
      })
    },
    { immediate: true }
  )
}
