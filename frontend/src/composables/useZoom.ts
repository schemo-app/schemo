import { clamp, useEventListener } from '@vueuse/core'
import { UISize } from '../../bindings/schemo/internal/domain'
import { DEFAULTS } from '../lib/layout'
import { usePlatform } from './usePlatform'
import { useSetting, useSettings } from './useSettings'

// The scale, walked from the keyboard: + and − step through the three sizes, 0 goes back to the
// middle one. These are not commands of the registry: they are not among the rebindable ones, and
// their note under the row is where they are said. Every window installs this, so the keys work
// wherever the person is looking.

const STEPS: readonly UISize[] = [UISize.SizeSmall, UISize.SizeMedium, UISize.SizeLarge]

const { save } = useSettings()
const appearance = useSetting('appearance')

function step(delta: number): void {
  const at = STEPS.indexOf(appearance.value.size)
  const to = STEPS[clamp(at + delta, 0, STEPS.length - 1)] ?? DEFAULTS.appearance.size
  if (to !== appearance.value.size) void save({ appearance: { size: to } })
}

/** The three steps, drawn as the menu's own rows: the keys below run the same ones. */
export function zoomIn(): void {
  step(1)
}

export function zoomOut(): void {
  step(-1)
}

export function zoomReset(): void {
  if (appearance.value.size !== DEFAULTS.appearance.size) {
    void save({ appearance: { size: DEFAULTS.appearance.size } })
  }
}

/** Called once per window, at its boot, beside the appearance itself. */
export function useZoom(): void {
  const { isMac } = usePlatform()

  useEventListener(window, 'keydown', (event: KeyboardEvent) => {
    // `mod` is the platform's own key, the way the registry reads a combination.
    if (!(isMac.value ? event.metaKey : event.ctrlKey) || event.altKey) return
    switch (event.code) {
      case 'Equal':
      case 'NumpadAdd':
        event.preventDefault()
        zoomIn()
        break
      case 'Minus':
      case 'NumpadSubtract':
        event.preventDefault()
        zoomOut()
        break
      case 'Digit0':
      case 'Numpad0':
        event.preventDefault()
        zoomReset()
        break
    }
  })
}
