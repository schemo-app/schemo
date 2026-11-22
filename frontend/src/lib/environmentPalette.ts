const PALETTE: Record<string, string> = {
  green: 'var(--green)',
  yellow: 'var(--orange)',
  red: 'var(--red)',
  blue: 'var(--accent)',
  pink: 'var(--tok-bool)',
  gray: 'var(--text-tertiary)',
}

export const ENVIRONMENT_COLORS = Object.keys(PALETTE)

export function dotOf(color: string): string {
  if (/^#(?:[a-f\d]{3}|[a-f\d]{6}|[a-f\d]{8})$/i.test(color)) return color
  return PALETTE[color] ?? PALETTE.green
}
