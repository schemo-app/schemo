import { ref } from 'vue'
import { DraftService } from '../../bindings/schemo/internal/transport/wails'
import type { AuthType, Scheme } from '../../bindings/schemo/internal/domain'
import { textOf } from '../i18n'

// Global cache of authorization schemes fetched from the backend
const schemes = ref<Scheme[]>([])

DraftService.AuthSchemes()
    .then((list) => {
      schemes.value = list ?? []
    })
    .catch(() => {})

/** One line of the menu a scheme is chosen from: the same shape the pane's select draws. */
type MenuOption = { key: string; label: string; hint?: string }

/** The name a scheme wears in the menu; the catalogue key of the scheme itself is the fallback. */
function menuName(scheme: Scheme): string {
  return textOf(scheme.menu || scheme.label)
}

/** Composable providing methods to query supported authentication schemes. */
export function useAuthSchemes() {
  /** Returns authentication schemes, filtering out schemes requiring a parent context when uninherited. */
  const offered = (canInherit: boolean) => schemes.value.filter((s) => canInherit || !s.needsParent)
  /** Finds an authentication scheme definition by its auth type identifier. */
  const schemeOf = (type: AuthType) => schemes.value.find((s) => s.type === type) ?? null

  return {
    offered,
    schemeOf,
    /** What a scheme is called in the menu, or the bare type while the list has not arrived. */
    nameOf: (type: AuthType) => {
      const scheme = schemeOf(type)
      return scheme ? menuName(scheme) : type
    },
    /**
     * The menu, in the groups the design cuts it into: the schemes with nothing to fill in, the main
     * ones that have fields, and the rest. A group with nothing in it is not drawn.
     */
    menuFor: (canInherit: boolean): MenuOption[][] => {
      const all = offered(canInherit)
      const option = (s: Scheme): MenuOption => ({
        key: s.type,
        label: menuName(s),
        hint: s.where ? textOf(s.where) : undefined,
      })
      return [
        all.filter((s) => !s.fields?.length).map(option),
        all.filter((s) => s.primary && s.fields?.length).map(option),
        all.filter((s) => !s.primary && s.fields?.length).map(option),
      ].filter((group) => group.length)
    },
  }
}