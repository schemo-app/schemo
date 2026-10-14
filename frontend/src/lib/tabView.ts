import { TabKind, type Collection, type Record as RequestRecord, type Tab } from '../../bindings/schemo/internal/domain'
import { findNode } from './collectionTree'
import { addressOf } from './address'
import { t } from '../i18n'

/** What a tab draws: its name, the method it edits, and the mark that says what kind it is. All of
 *  it is read off what the window already holds — the strip stores none of it in Go. */
export interface TabView {
  title: string
  method: string
  icon: 'Folder' | 'Globe' | 'Clock' | 'BookOpen' | 'Radio' | null
}

export function tabView(
  tab: Tab,
  what: {
    tree: Collection[]
    records: RequestRecord[]
    draft: { method: string; url: string } | null
  }
): TabView {
  switch (tab.kind) {
    case TabKind.TabKindCollections: {
      const at = what.tree.find((c) => c.id === tab.ref)
      return {
        title: at?.name ?? t('sidebar.collections'),
        method: '',
        icon: 'Folder',
      }
    }
    case TabKind.TabKindWelcome:
      return { title: t('welcome.tabTitle'), method: '', icon: 'BookOpen' }
    case TabKind.TabKindHistory:
      return { title: t('sidebar.history'), method: '', icon: 'Clock' }
    case TabKind.TabKindCapture:
      return { title: t('sidebar.capture'), method: '', icon: 'Globe' }
    case TabKind.TabKindPubs:
      return { title: t('sidebar.publications'), method: '', icon: 'Radio' }
    default: {
      // A request that lives in a collection is drawn under the name it is saved with; the address
      // is a draft's title, where there is no name yet.
      const node = tab.ref ? findNode(what.tree, tab.ref) : null
      if (node) {
        const fallback = what.draft?.url ? addressOf(what.draft.url) : t('request.newRequestName')
        return {
          title: node.name || fallback,
          method: what.draft?.method || node.method || 'GET',
          icon: null,
        }
      }
      if (what.draft) {
        return {
          title: what.draft.url ? addressOf(what.draft.url) : t('request.newRequestName'),
          method: what.draft.method,
          icon: null,
        }
      }
      const record = tab.record ? what.records.find((r) => r.id === tab.record) : undefined
      if (record) return { title: addressOf(record.url), method: record.method, icon: null }
      return { title: t('request.newRequestName'), method: 'GET', icon: null }
    }
  }
}
