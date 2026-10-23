import type { IconName } from '../components/ui/Icon.vue'

export type CategoryId =
  | 'account'
  | 'general'
  | 'appearance'
  | 'shortcuts'
  | 'requests'
  | 'proxy'
  | 'extension'
  | 'mcp'
  | 'updates'

export type Category = {
  id: CategoryId
  icon: IconName
  tile: string
}

export const CATEGORIES: readonly Category[] = [
  { id: 'account', icon: 'User', tile: 'var(--accent)' },
  { id: 'general', icon: 'Settings2', tile: 'var(--text-tertiary)' },
  { id: 'appearance', icon: 'Contrast', tile: 'var(--purple)' },
  { id: 'shortcuts', icon: 'Keyboard', tile: 'var(--text-secondary)' },
  { id: 'requests', icon: 'ArrowRightLeft', tile: 'var(--green)' },
  { id: 'proxy', icon: 'Shield', tile: 'var(--orange)' },
  { id: 'extension', icon: 'Puzzle', tile: 'var(--red)' },
  { id: 'mcp', icon: 'CodeXml', tile: 'var(--teal)' },
  { id: 'updates', icon: 'RefreshCw', tile: 'var(--purple)' },
]

export function nameKey(id: CategoryId): `settings.cat.${CategoryId}` {
  return `settings.cat.${id}`
}

type StaticMeta = Exclude<CategoryId, 'account'>

export function metaKey(
  category: Category
): 'account.signedOut' | `settings.meta.${StaticMeta}` {
  if (category.id === 'account') return 'account.signedOut'
  return `settings.meta.${category.id}`
}

export function subKey(category: Category): `settings.sub.${CategoryId}` {
  return `settings.sub.${category.id}`
}

export function asCategory(value: string | null): CategoryId {
  const found = CATEGORIES.find((category) => category.id === value)
  return found ? found.id : 'general'
}
