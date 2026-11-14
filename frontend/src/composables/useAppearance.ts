import { watch } from 'vue'
import { useSettings } from './useSettings'
import { applyLook } from '../lib/windowLook'

// Writes the appearance as root attributes the stylesheet reads.

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
