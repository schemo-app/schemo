import { computed } from 'vue'
import type { RecordView } from '../../lib/requestRecord'
import { prettyJson, tryParseJson } from '../../lib/json'
import { buildJsonTree, isContainer } from '../../lib/jsonTree'
import { isJsonApi, type JsonApiDocument } from '../../lib/jsonapi'
import { viewerLanguage } from '../../lib/viewerLanguage'
import { RecordSource, TabKind } from '../../../bindings/schemo/internal/domain'
import { useRecordsStore } from '../../stores/records'
import { useTabsStore } from '../../stores/tabs'
import { useCookiesStore } from '../../stores/cookies'
import { setViewState, viewState, type PaneSource } from '../../stores/content'
import { splitAddress } from '../../lib/address'
import { focusUrlField } from '../intents'
import { useMessages } from '../../i18n'
import type { BodyView, ResponseTab } from '../../lib/responseView'

export type { BodyView, ResponseTab }

// The record, read: how its body is understood, which readings that leaves, where a person can go
// from it. `canNavigate` is false in a collection card — one response, no history, no command line.
export function useResponseView(
  record: () => RecordView,
  canNavigate: () => boolean,
  pane: () => PaneSource | null
) {
  const { t } = useMessages()
  const records = useRecordsStore()
  const jar = useCookiesStore()

  const parsed = computed(() => tryParseJson(record().responseBody))
  const isJson = computed(() => parsed.value.ok)
  const jsonValue = computed(() => parsed.value.value)
  const isJsonApiDoc = computed(() => isJson.value && isJsonApi(jsonValue.value))
  const doc = computed<JsonApiDocument | null>(() =>
    isJsonApiDoc.value ? (jsonValue.value as JsonApiDocument) : null
  )

  const prettyRaw = computed(() => (isJson.value ? prettyJson(jsonValue.value) : record().responseBody))
  const language = computed(() => viewerLanguage(record().contentType, record().responseBody, isJson.value))

  // Every node of the body, keyed by its address: what the tree walks, the map lists and the counts
  // are read from. One build per body, however many of the three ask.
  const nodes = computed(() => (isJson.value ? buildJsonTree(jsonValue.value) : new Map()))

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

  // What draws the body: the reading that was picked, and what the body is. A body that is not JSON
  // is read as the text it is, whichever reading was left selected.
  const reader = computed<'tree' | 'map' | 'text'>(() =>
    isJson.value && bodyView.value !== 'raw' ? bodyView.value : 'text'
  )

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

  const viewLabels = computed<Record<BodyView, string>>(() => ({
    tree: t('response.views.tree'),
    raw: t('response.views.raw'),
    map: t('response.views.map'),
  }))

  /** The three readings a JSON leaves; a body that is not JSON has only its text. */
  const views = computed<BodyView[]>(() => (isJson.value ? ['raw', 'tree', 'map'] : ['raw']))

  // The tab being read is kept between records; only a reading the record has no room for moves —
  // and a body that is not JSON is read as text, whatever was on screen before it.
  function settle() {
    if (!tabs.value.includes(activeTab.value)) activeTab.value = 'body'
    if (!views.value.includes(bodyView.value)) bodyView.value = 'raw'
  }

  // What the body is, in one line beside the reading: what it holds, and of how many parts.
  const bodyInfo = computed(() => {
    if (!isJson.value) return ''
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

  function openInRequest() {
    // The whole record becomes a request tab of its own — Go seeds the draft — and the reader
    // lands on it.
    const tabs = useTabsStore()
    void tabs.open({
      kind: TabKind.TabKindRequest,
      ref: record().id,
      record: record().id,
      newTab: true,
    })
    focusUrlField()
  }

  // The manager opens on the host this answer went to, not the one it was last left on.
  function openCookieManager() {
    void jar.openManager(splitAddress(record().url).host)
  }

  return {
    activeTab,
    bodyView,
    reader,
    views,
    viewLabels,
    selected,
    collapsed,
    nodes,
    jsonValue,
    bodyInfo,
    toggleNode,
    selectPath,
    expandAll,
    collapseAll,
    tabLabels,
    tabs,
    settle,
    doc,
    isJsonApiDoc,
    isJson,
    prettyRaw,
    language,
    follow,
    openInRequest,
    openCookieManager,
  }
}
