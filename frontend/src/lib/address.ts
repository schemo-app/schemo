/**
 * Strips the URI scheme (e.g. `https://`) from a URL string.
 * Uses regex instead of `URL` parser to preserve template variables like `{{host}}`.
 */
export function addressOf(url: string): string {
  return url.replace(/^[a-zA-Z][\w+.-]*:\/\//, '')
}

/**
 * Splits a URL into what stands before its path and the rest, at the first `/`, `?` or `#`
 * (RFC 3986 §3.2: the authority ends there). The host keeps its port and any user: this is for
 * drawing an address, and the domain of the jar is Go's to name. Preserves template syntax without
 * URL-encoding.
 */
export function splitAddress(url: string): { host: string; path: string } {
  const rest = addressOf(url)
  const cut = rest.search(/[/?#]/)
  return cut < 0 ? { host: rest, path: '' } : { host: rest.slice(0, cut), path: rest.slice(cut) }
}