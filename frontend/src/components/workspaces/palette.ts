export const WORKSPACE_TINTS = {
  blue: 'var(--accent)',
  purple: 'var(--purple)',
  green: 'var(--green)',
  orange: 'var(--orange)',
  grey: 'var(--text-tertiary)',
} as const
export type WorkspaceColor = keyof typeof WORKSPACE_TINTS

export const WORKSPACE_COLORS = Object.keys(WORKSPACE_TINTS) as WorkspaceColor[]

export function tintOf(color: string): string {
  const tints: Record<string, string> = WORKSPACE_TINTS
  return tints[color] ?? WORKSPACE_TINTS.grey
}
