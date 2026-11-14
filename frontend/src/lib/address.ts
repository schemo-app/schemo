/** Strips the scheme; a regex rather than `URL`, so `{{host}}` survives. */
export function addressOf(url: string): string {
  return url.replace(/^[a-zA-Z][\w+.-]*:\/\//, '')
}

/** Splits at the first `/`, `?` or `#` (RFC 3986 §3.2), keeping template syntax unencoded. */
export function splitAddress(url: string): { host: string; path: string } {
  const rest = addressOf(url)
  const cut = rest.search(/[/?#]/)
  return cut < 0 ? { host: rest, path: '' } : { host: rest.slice(0, cut), path: rest.slice(cut) }
}