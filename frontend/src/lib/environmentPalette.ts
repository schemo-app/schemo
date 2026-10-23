const PALETTE = {
  green: { tint: 'var(--green-text)', dot: 'var(--green)' },
  orange: { tint: 'var(--orange-text)', dot: 'var(--orange)' },
  red: { tint: 'var(--red-text)', dot: 'var(--red)' },
  purple: { tint: 'color-mix(in srgb, var(--purple) 72%, var(--text))', dot: 'var(--purple)' },
} as const
export type EnvironmentColor = keyof typeof PALETTE

export const ENVIRONMENT_COLORS = Object.keys(PALETTE) as EnvironmentColor[]

export const ENVIRONMENT_TINTS = Object.fromEntries(
  ENVIRONMENT_COLORS.map((color) => [color, PALETTE[color].tint]),
) as Record<EnvironmentColor, string>

export const DEFAULT_ENVIRONMENT_COLOR = 'green'

export const GLOBALS_COLOR = 'purple'

export function tintOf(color: string): string {
  const tints: Record<string, string> = ENVIRONMENT_TINTS
  return tints[color] ?? ENVIRONMENT_TINTS[DEFAULT_ENVIRONMENT_COLOR]
}

export function dotOf(color: string): string {
  const held: Record<string, { dot: string }> = PALETTE
  return (held[color] ?? PALETTE[DEFAULT_ENVIRONMENT_COLOR]).dot
}
