import { computed, ref, watch, nextTick, useTemplateRef } from 'vue'
import type { RecordView } from '../lib/requestRecord'
import { tryParseJson } from '../lib/json'
import { buildJsonTree, isContainer, type JsonNode } from '../lib/jsonTree'
import { isJsonApi } from '../lib/jsonapi'
import { viewerLanguage } from '../lib/viewerLanguage'
import { RecordSource } from '../../bindings/schemo/internal/domain'
import { useRecordsStore } from '../stores/records'
import { useCookiesStore } from '../stores/cookies'
import { setViewState, viewState, type PaneSource } from '../stores/content'
import { t } from '../i18n'
import { runOrToast } from '../lib/calls'
import { touchedJar } from '../lib/responseCookies'
import { History } from '../../bindings/schemo/internal/request'
import { exampleName, exampleOf, isKeepable } from '../lib/examples'
import { useToast } from './useToast'
import { useCopyFeedback } from './useCopyFeedback'
import type { BodyView, ResponseTab } from '../lib/responseView'


// The record, read: how its body is understood, which readings that leaves, where a person can go
// from it. `canNavigate` is false in a collection card — one response, no history, no command line.
export function useResponseView(
  record: () => RecordView,
  canNavigate: () => boolean,
  pane: () => PaneSource | null
) {
  const records = useRecordsStore()
  const cookies = useCookiesStore()

  const parsed = computed(() => tryParseJson(record().responseBody))
  const isJson = computed(() => parsed.value.ok)
  const jsonValue = computed(() => parsed.value.value)
  const structured = computed(() => jsonValue.value !== null && typeof jsonValue.value === 'object')
  const doc = computed(() =>
    isJsonApi(jsonValue.value) ? jsonValue.value : null
  )

  const language = computed(() => viewerLanguage(record().contentType, record().responseBody, isJson.value))

  const nodes = computed<ReadonlyMap<string, JsonNode>>(() =>
    structured.value ? buildJsonTree(jsonValue.value) : new Map()
  )
  const arrays = computed(() => [...nodes.value.values()].filter(node => node.kind === 'array'))

  // Which reading is on screen outlives the component: it is the tab's, like the chip a request pane
  // is open on, so switching tabs and coming back shows what was left.
  const activeTab = computed<ResponseTab>({
    get: () => viewState(pane()).tab,
    set: (tab) => setViewState(pane(), { tab }),
  })

  const bodyView = computed<BodyView>({
    get: () => viewState(pane()).bodyView,
    set: (bodyView) => setViewState(pane(), { bodyView }),
  })

  const selected = computed(() => viewState(pane()).selPath)
  const collapsed = computed(() => viewState(pane()).collapsed)

  const tabLabels = computed<Record<ResponseTab, string>>(() => ({
    body: t('response.tabs.body'),
    headers: t('response.tabs.headers'),
    cookies: t('response.tabs.cookies'),
    timings: t('response.tabs.timings'),
    scripts: t('response.tabs.scripts'),
    request: t('response.tabs.request'),
  }))

  // The one place that decides which readings exist — the tab bar and the fallback both read it.
  const tabs = computed<ResponseTab[]>(() => {
    const list: ResponseTab[] = ['body', 'headers']
    // A request this app sent always has an answer; a captured one usually has none.
    if (record().source === RecordSource.SourceManual || record().cookieWrites.length > 0) {
      list.push('cookies')
    }
    list.push('timings')
    // Only a record of this app's own can carry a report; a captured one has none.
    if (record().source === RecordSource.SourceManual) list.push('scripts')
    // A captured record has no command line above the viewer, so its request tab stays.
    if (record().source === RecordSource.SourceBrowser) list.push('request')
    return list
  })

  /** JSON arrays also have a table; a body that is not JSON has only its text. */
  const views = computed<BodyView[]>(() => {
    if (!structured.value) return ['raw']
    return arrays.value.length ? ['tree', 'raw', 'map', 'table'] : ['tree', 'raw', 'map']
  })

  const reader = computed(() =>
    views.value.includes(bodyView.value) ? bodyView.value : views.value[0]
  )
  const viewOptions = computed(() =>
    views.value.map((value) => ({ value, label: t(`response.views.${value}`) }))
  )

  // The tab being read is kept between records; only a reading the record has no room for moves —
  // and a body that is not JSON is read as text, whatever was on screen before it.
  function keepReadingAvailable() {
    if (!tabs.value.includes(activeTab.value)) activeTab.value = 'body'
    if (structured.value && bodyView.value !== reader.value) bodyView.value = reader.value
  }

  // What the body is, in one line beside the reading: what it holds, and of how many parts.
  const bodyInfo = computed(() => {
    const root = nodes.value.get('$')
    if (!root) return ''
    const held =
      root.kind === 'array'
        ? t('response.jsonItems', root.children.length)
        : t('response.jsonFields', root.children.length)
    return `JSON · ${held} · ${t('response.jsonNodes', nodes.value.size - 1)}`
  })

  function toggleNode(path: string) {
    const current = viewState(pane()).collapsed
    setViewState(pane(), { collapsed: { ...current, [path]: !current[path] } })
  }

  function selectPath(path: string) {
    setViewState(pane(), { selPath: path })
  }

  function expandAll() {
    setViewState(pane(), { collapsed: {} })
  }

  // Folding everything back leaves the root: the one node every document has.
  function collapseAll() {
    const folded: Record<string, boolean> = {}
    for (const node of nodes.value.values()) {
      if (node.path !== '$' && isContainer(node.kind)) folded[node.path] = true
    }
    setViewState(pane(), { collapsed: folded, selPath: '$' })
  }

  // A plain GET with the headers that worked last time; the record is already masked. The answer
  // lands in the tab the person is reading — the viewer is that tab's.
  function follow(url: string) {
    if (!url || !canNavigate()) return
    void records.sendSpec({ method: 'GET', url, headers: record().requestHeaders, body: '' })
  }

  // The manager opens on the address this answer went to, not on the one it was last left on.
  function openCookieManager() {
    void cookies.openManager(record().url)
  }

  return {
    activeTab,
    bodyView,
    reader,
    viewOptions,
    selected,
    collapsed,
    nodes,
    arrays,
    jsonValue,
    bodyInfo,
    toggleNode,
    selectPath,
    expandAll,
    collapseAll,
    tabLabels,
    tabs,
    keepReadingAvailable,
    doc,
    language,
    follow,
    openCookieManager,
  }
}

// Taking an answer's cookie writes back reverts the jar and takes the claim off the record, so the
// tab draws the answer the record now describes. There is nothing to take back for an answer that
// left the jar as it was.
export function useCookieUndo(record: () => RecordView) {
  const records = useRecordsStore()
  const undoing = ref(false)
  const touched = computed(() => touchedJar(record().cookieWrites))

  async function undo() {
    undoing.value = true
    const ok = await runOrToast(History.UndoCookieWrites(record().id), 'cookies.undoFailed')
    undoing.value = false
    if (ok) await records.refresh(record().id)
  }

  return { touched, undoing, undo }
}

// "To examples": the answer on screen kept beside the request it belongs to, under a name asked for
// first, since an example is found by its name more often than by its code. Only a saved request
// has somewhere to keep it (a draft's list lives in its tab and has no row to land on), and only an
// answer that answered: a refusal has no status and nothing worth keeping.
export function useKeepExample(record: () => RecordView, pane: () => PaneSource | null) {
  const toast = useToast()

  const canKeep = computed(() => Boolean(pane()?.nodeId) && isKeepable(record()))
  const isKeeping = ref(false)
  const draftName = ref('')
  const nameField = useTemplateRef<{ focus(): void; select(): void }>('exampleNameField')

  // The popover opens on the name the catalogue gives the status, ready to be overwritten.
  watch(isKeeping, (open) => {
    if (open) draftName.value = exampleName(record().status)
  })

  function focusName() {
    void nextTick(() => {
      nameField.value?.focus()
      nameField.value?.select()
    })
  }

  function keepAsExample() {
    const answer = record()
    const name =
      draftName.value.trim() || exampleName(answer.status) || t('request.examples.untitled')
    const current = pane()
    current?.saveExamples([...(current.responses ?? []), exampleOf(answer, name)])
    isKeeping.value = false
    toast.show(t('request.examples.saved', { name }))
  }

  return { canKeep, isKeeping, draftName, exampleNameField: nameField, focusName, keepAsExample }
}

// What the row's copy button copies, reading by reading. Cookies, Timings and Scripts have no
// single text, so the answer there is null and the button is not drawn.
export function useResponseCopy(opts: {
  record: () => RecordView
  tab: () => ResponseTab
  bodyText: () => string
  exportText: (format: string) => string | undefined
}) {
  const { copied, copy } = useCopyFeedback()

  const sentText = computed(() => {
    const record = opts.record()
    return [
      `${record.method} ${record.url}`,
      ...record.requestHeaders.map((requestHeader) => `${requestHeader.name}: ${requestHeader.value}`),
      ...(record.requestBody ? ['', record.requestBody] : []),
    ].join('\n')
  })

  const copyText = computed<string | null>(() => {
    switch (opts.tab()) {
      case 'body':
        return opts.bodyText()
      case 'headers':
        return opts
          .record()
          .responseHeaders.map((responseHeader) => `${responseHeader.name}: ${responseHeader.value}`)
          .join('\n')
      case 'request':
        return sentText.value
      default:
        return null
    }
  })

  async function copyExport(format: string) {
    const text = opts.exportText(format)
    if (text) await copy(text)
  }

  return { copied, copy, copyText, copyExport }
}

/** What the search is looking at. */
export type SearchKind = 'tree' | 'text' | 'map' | 'list'

/** A text viewer's steps through its matches. */
export type TextViewer = { next: () => void; prev: () => void }

// The search of the response row: one query and one field for every reading that has something to
// search. A reading with nothing to search closes the field.
export function useResponseSearch(opts: {
  kind: () => SearchKind | null
  placeholder: () => string
  /** How many the tree, the map or the list on screen found for the query — the text counts its own. */
  matches: (query: string) => number
  textViewer: () => TextViewer | null | undefined
}) {

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
        return t('response.searchFound', { n: opts.matches(query.value) })
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
