/**
 * Strips the URI scheme (e.g. `https://`) from a URL string.
 * Uses regex instead of `URL` parser to preserve template variables like `{{host}}`.
 */
export function addressOf(url: string): string {
  return url.replace(/^[a-zA-Z][\w+.-]*:\/\//, '')
}

/**
 * Splits a URL into host and path components at the first slash.
 * Preserves template syntax without URL-encoding.
 */
export function splitAddress(url: string): { host: string; path: string } {
  const rest = addressOf(url)
  const cut = rest.indexOf('/')
  return cut < 0 ? { host: rest, path: '' } : { host: rest.slice(0, cut), path: rest.slice(cut) }
}