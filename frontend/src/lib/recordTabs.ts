import { t } from '../i18n'
import type { Record as RequestRecord } from '../../bindings/schemo/internal/domain'
import type { TabState } from '../../bindings/schemo/internal/agent'

/** Represents captured network traffic grouped by browser tab. */
export interface TabGroup {
  key: string
  title: string
  url: string
  favIconUrl: string
  items: RequestRecord[]
}

/** A captured tab and its saved requests, including tabs that have not sent a request yet. */
export interface CaptureSession extends TabGroup {
  tab?: TabState
}

/** Combines active capture metadata with history without creating synthetic requests. */
export function captureSessions(records: RequestRecord[], tabs: TabState[]): CaptureSession[] {
  const groups = new Map<string, CaptureSession>(tabGroups(records).map(group => [group.key, group]))
  for (const tab of tabs) {
    const key = String(tab.tabId)
    const group = groups.get(key) ?? { key, title: '', url: '', favIconUrl: '', items: [] }
    groups.set(key, {
      ...group,
      title: tab.title || group.title,
      url: tab.url || group.url,
      favIconUrl: tab.favIconUrl || group.favIconUrl,
      tab,
    })
  }
  return [...groups.values()].sort((a, b) => Number(!!b.tab) - Number(!!a.tab))
}

/**
 * Grouping key: tabId, else tabURL, else 'unknown'. Other devices' records are prefixed because each
 * device numbers its tabs on its own. Mirrors `tabKeyOf` in internal/request.
 */
export function tabKeyOf(record: RequestRecord): string {
  const key = record.tabId ? String(record.tabId) : record.tabURL || 'unknown'
  return record.isRemote ? `remote:${key}` : key
}

/** Groups records by browser tab, most recently active first. */
function tabGroups(records: RequestRecord[]): TabGroup[] {
  const map = new Map<string, TabGroup>()

  for (const record of records) {
    const key = tabKeyOf(record)
    let group = map.get(key)

    if (!group) {
      group = {
        key,
        title: record.tabTitle || '',
        url: record.tabURL || '',
        favIconUrl: record.favIconUrl || '',
        items: [],
      }
      map.set(key, group)
    }

    group.items.push(record)

    if (!group.title && record.tabTitle) group.title = record.tabTitle
    if (!group.favIconUrl && record.favIconUrl) group.favIconUrl = record.favIconUrl
    if (!group.url && record.tabURL) group.url = record.tabURL
  }

  return Array.from(map.values()).sort((a, b) => b.items[0].startedAt - a.items[0].startedAt)
}

/** A group's title, hostname or fallback label. */
export function groupLabel(group: TabGroup): string {
  return group.title || hostnameOf(group.url) || t('history.tab')
}

/** Extracts hostname from URL string safely. */
function hostnameOf(url: string): string {
  try {
    return new URL(url).hostname
  } catch {
    return ''
  }
}

/** A stable hue for a group's fallback avatar. */
export function groupHue(key: string): number {
  let hue = 0
  for (let i = 0; i < key.length; i++) hue = (hue * 31 + key.charCodeAt(i)) >>> 0
  return hue % 360
}
