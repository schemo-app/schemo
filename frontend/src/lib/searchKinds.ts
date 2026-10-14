import { SearchKind, SearchNoteKind, type SearchNote } from '../../bindings/schemo/internal/domain'
import { t, type MessageKey } from '../i18n'
import type { IconName } from '../components/ui/Icon.vue'

/** Icon glyph mapping for search result categories. */
const KIND_ICONS = {
  [SearchKind.SearchRequest]: 'ArrowUpRight',
  [SearchKind.SearchCollection]: 'Folder',
  [SearchKind.SearchEnvironment]: 'Globe',
  [SearchKind.SearchHistory]: 'Clock',
  [SearchKind.SearchAction]: 'Sparkles',
} satisfies Record<Exclude<`${SearchKind}`, ''>, IconName>

/** Returns icon glyph identifier for a given search item kind. */
export function kindIcon(kind: SearchKind): IconName {
  const icons: Record<string, IconName | undefined> = KIND_ICONS
  return icons[kind] ?? 'Search'
}

/** Localization key mapping for search result group headers. */
const KIND_LABELS = {
  [SearchKind.SearchRequest]: 'search.kinds.request',
  [SearchKind.SearchCollection]: 'search.kinds.collection',
  [SearchKind.SearchEnvironment]: 'search.kinds.environment',
  [SearchKind.SearchHistory]: 'search.kinds.history',
  [SearchKind.SearchAction]: 'search.kinds.action',
} satisfies Record<Exclude<`${SearchKind}`, ''>, MessageKey>

/** Returns localized heading label for a search category. */
export function kindLabel(kind: SearchKind): string {
  const labels: Record<string, MessageKey | undefined> = KIND_LABELS
  const key = labels[kind]
  return key ? t(key) : ''
}

/** Function mapping for formatting search item secondary notes. */
const NOTE_TEXT = {
  [SearchNoteKind.NoteActive]: () => t('search.notes.active'),
  [SearchNoteKind.NoteRequests]: (note: SearchNote) => t('counts.requests', note.count ?? 0),
} satisfies Record<Exclude<`${SearchNoteKind}`, ''>, (note: SearchNote) => string>

/** Formats localized secondary text string for a search result note. */
export function noteLabel(note: SearchNote | null | undefined): string {
  const said: Record<string, ((note: SearchNote) => string) | undefined> = NOTE_TEXT
  const word = note?.kind ? said[note.kind] : undefined
  return note && word ? word(note) : ''
}