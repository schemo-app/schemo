import { computed, nextTick, ref, useTemplateRef, watch } from 'vue'
import { useMessages } from '../../i18n'

/** What the search is looking at. */
export type SearchKind = 'tree' | 'text' | 'map' | 'list'

/** A text viewer's steps through its matches. */
export type TextViewer = { next: () => void; prev: () => void }

// The search of the response row: one query and one field for every reading that has something to
// search. A reading with nothing to search closes the field.
export function useResponseSearch(opts: {
  kind: () => SearchKind | null
  placeholder: () => string
  /** How many the tree, the map or the list on screen found — the text counts its own. */
  matches: () => number
  textViewer: () => TextViewer | null | undefined
}) {
  const { t } = useMessages()

  const query = ref('')
  const searching = ref(false)
  const searchField = useTemplateRef<HTMLInputElement>('searchField')
  // How many the text viewer found and which one is selected.
  const stats = ref({ count: 0, index: 0 })

  function setStats(next: { count: number; index: number }) {
    stats.value = next
  }

  const searchable = computed(() => opts.kind() !== null)

  // Beside the field: what the query left, or which match of the text is selected.
  const count = computed(() => {
    if (!query.value.trim()) return ''
    switch (opts.kind()) {
      case 'tree':
      case 'map':
      case 'list':
        return t('response.searchFound', { n: opts.matches() })
      case 'text':
        return stats.value.count
          ? `${stats.value.index + 1} / ${stats.value.count}`
          : t('response.text.noMatches')
      default:
        return ''
    }
  })

  function open() {
    searching.value = true
    void nextTick(() => searchField.value?.focus())
  }

  function close() {
    searching.value = false
    query.value = ''
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      close()
    } else if (e.key === 'Enter' && opts.kind() === 'text') {
      e.preventDefault()
      if (e.shiftKey) opts.textViewer()?.prev()
      else opts.textViewer()?.next()
    }
  }

  // A reading with nothing to search closes the field.
  watch(() => opts.kind(), () => {
    if (!searchable.value) close()
  })

  return { query, searching, searchField, stats, setStats, kind: opts.kind, searchable, placeholder: opts.placeholder, count, open, close, onKey }
}
