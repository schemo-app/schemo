import { computed, type ComputedRef } from 'vue'
import { t, te } from '../i18n'

/**
 * The word a person reads for each colour of a palette, from the catalogue's `<scope>.color<Name>`.
 * A computed and not a plain object: a map made once would keep the words of the language it was
 * made in. A colour with no word is read as itself; a test holds the catalogue to the palette.
 */
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
