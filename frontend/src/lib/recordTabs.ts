import { t } from '../i18n'
import type { Record as RequestRecord } from '../../bindings/schemo/internal/domain'
import type { TabState } from '../../bindings/schemo/internal/transport/extension'

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
    groups.set(key, { ...group, title: tab.title || group.title, url: tab.url || group.url, tab })
  }
  return [...groups.values()].sort((a, b) => Number(!!b.tab) - Number(!!a.tab))
}

/**
 * Derives a stable key for grouping tab records.
 * Uses tabId when available, falling back to tabURL or 'unknown'; a record captured on another
 * device is prefixed, since that device numbers its tabs from its own first one — without it the
 * two devices' seventh tabs are one tab here. The Go half is `usecase/record`'s `tabKeyOf`.
 */
export function tabKeyOf(record: RequestRecord): string {
  const key = record.tabId ? String(record.tabId) : record.tabURL || 'unknown'
  return record.isRemote ? `remote:${key}` : key
}

/**
 * Groups raw network records by browser tab and sorts groups by most recent activity.
 */
export function tabGroups(records: RequestRecord[]): TabGroup[] {
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

/**
 * Resolves display label for a tab group (title, hostname, or localized fallback).
 */
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

/**
 * Generates a deterministic hue (0-350) for tab avatar fallback based on group key hash.
 */
export function groupHue(key: string): number {
  let hue = 0
  for (let i = 0; i < key.length; i++) hue = (hue * 31 + key.charCodeAt(i)) >>> 0
  return hue % 360
}

/**
 * Finds target group by key, defaulting to the newest group if unmatched.
 */
export function effectiveTab(groups: TabGroup[], key: string | null): TabGroup | null {
  if (!groups.length) return null
  return groups.find((group) => group.key === key) ?? groups[0]
}
