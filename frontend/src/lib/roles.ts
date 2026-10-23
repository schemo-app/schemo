import type { MessageKey } from '../i18n'

type Say = (key: MessageKey) => string
type Has = (key: string) => key is MessageKey

/** Palette tints supported for system roles. */
export type RoleTint = 'purple' | 'accent' | 'green' | 'grey'

/** Built-in role definitions mapping role names to localization keys and UI tints. */
const SHIPPED: Record<string, { key: string; tint: RoleTint }> = {
  Owner: { key: 'owner', tint: 'purple' },
  Admin: { key: 'admin', tint: 'accent' },
  Editor: { key: 'editor', tint: 'green' },
  Viewer: { key: 'viewer', tint: 'grey' },
}

/** Mapping of role tints to CSS theme variable values. */
const TINTS: Record<RoleTint, string> = {
  purple: 'var(--purple)',
  accent: 'var(--accent)',
  green: 'var(--green)',
  grey: 'var(--text-tertiary)',
}

/** Resolves internal translation key for a built-in role name. */
function keyOf(name: string): string {
  const held = SHIPPED[name]
  return held ? `roles.shipped.${held.key}` : ''
}

/** Returns localized display name for built-in roles, falling back to raw name for custom roles. */
export function roleLabel(t: Say, te: Has, name: string): string {
  const key = keyOf(name)
  return key && te(key) ? t(key) : name
}

/** Resolves CSS color variable string corresponding to a role name. */
export function roleTint(name: string): string {
  const held = SHIPPED[name]
  const tints: { [tint: string]: string | undefined } = TINTS
  return (held?.tint ? tints[held.tint] : undefined) ?? 'var(--text-tertiary)'
}