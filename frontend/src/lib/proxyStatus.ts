import { ProxyMode } from '../../bindings/schemo/internal/domain'
import type { ProxySettings } from '../../bindings/schemo/internal/domain'
import { t as message, type MessageKey } from '../i18n'

/** What the catalogue's own `t` takes besides the key. */
type Said = Parameters<typeof message>[1]

// One function for the two places the same sentence is drawn: the rail, under the section's name,
// and the page's head, in the pill.

export function proxyActive(proxy: ProxySettings | null | undefined): boolean {
  if (!proxy) return false
  if (proxy.mode === ProxyMode.ProxyOff) return false
  if (proxy.mode === ProxyMode.ProxyManual) return Boolean(proxy.host)
  return true
}

/**
 * Nothing at all while there is no setting to read: a description in the pill is not a
 * state, and naming a mode before the settings arrive would be a guess.
 */
export function proxyLine(
  t: (key: MessageKey, said?: Said) => string,
  proxy: ProxySettings | null | undefined
): string {
  if (!proxy) return ''
  switch (proxy.mode) {
    case ProxyMode.ProxyManual:
      return proxy.host
        ? t('settings.proxyStatus.manual', { host: proxy.host })
        : t('settings.proxyStatus.off')
    case ProxyMode.ProxyOff:
      return t('settings.proxyStatus.off')
    default:
      return t('settings.proxyStatus.system')
  }
}
