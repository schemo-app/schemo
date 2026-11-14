import { computed, type ComputedRef } from 'vue'
import { t, te } from '../i18n'

/** Labels each palette colour from `<scope>.color<Name>`; computed, so it follows the language. */
export function useColorLabels(
  scope: 'workspaces' | 'environments',
  colors: readonly string[],
): ComputedRef<Record<string, string>> {
  return computed(() =>
    Object.fromEntries(
      colors.map((color) => {
        const key = `${scope}.color${color.charAt(0).toUpperCase()}${color.slice(1)}`
        return [color, te(key) ? t(key) : color]
      }),
    ),
  )
}
