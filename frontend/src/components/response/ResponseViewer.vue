<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useTemplateRef, watch } from 'vue'
import { useEventListener } from '@vueuse/core'
import HeaderTable from './HeaderTable.vue'
import JsonApiSteps from './JsonApiSteps.vue'
import ResponseBar from './ResponseBar.vue'
import SentRequest from './SentRequest.vue'
import Icon from '../ui/Icon.vue'
import TabText from '../ui/TabText.vue'
import { IconButton } from '../ui/button'
import { Input } from '../ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '../ui/popover'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '../ui/tabs'
import Segment from '../ui/Segment.vue'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../ui/dropdown-menu'
import type { RecordView } from '../../lib/requestRecord'
import JsonApiTree from '../json/JsonApiTree.vue'
import JsonTree from '../json/JsonTree.vue'
import JsonMap from '../json/JsonMap.vue'
import TextViewerTab from './TextViewerTab.vue'
import SchemaMap from '../json/SchemaMap.vue'
import NodeInspector from '../inspector/NodeInspector.vue'
import CookiesTab from './CookiesTab.vue'
import TimingsTab from './TimingsTab.vue'
import ScriptsTab from './ScriptsTab.vue'
import type { ScriptRun } from '../../../bindings/schemo/internal/domain'
import { ScriptingService } from '../../../bindings/schemo/internal/transport/wails'
import { inspectorState, setInspectorState, type PaneSource } from '../../stores/content'
import { formatBytes, useMessages } from '../../i18n'
import { exampleName, exampleOf, isKeepable } from '../../lib/examples'
import { useCommands } from '../../composables/useCommands'
import { statusBadgeClass } from '../../lib/format'
import { useToast } from '../../composables/useToast'
import { useInspection } from '../../composables/response/useInspection'
import { useCookieUndo } from '../../composables/response/useCookieUndo'
import { useResponseCopy } from '../../composables/response/useResponseCopy'
import { useResponseSearch, type SearchKind, type TextViewer } from '../../composables/response/useResponseSearch'
import { responseSearchable, searchResponse } from '../../composables/intents'
import { useResponseView } from '../../composables/response/useResponseView'
import { allResources, resourceMatchesQuery } from '../../lib/jsonapi'
import type { BodyView, ResponseTab } from '../../lib/responseView'

// Where this pane is drawn: beside the command line, beside a captured request, or inside a
// collection card. The record is a prop either way; whose state it is (the inspector) is not.
const {
  source = 'request',
  record,
  pane = null,
} = defineProps<{
  record: RecordView
  source?: 'request' | 'browser' | 'collection'
  pane?: PaneSource | null
}>()

// A captured browser request is not the request of the command line, so nothing there is kept up to
// date from it: the rule would be written on a request that did not make this response.
const captureHost = computed(() => (source === 'browser' ? null : pane))

// A card shows one response: there is no history behind it to walk and no command line to hand it
// to.
const hasHistory = computed(() => source !== 'collection')
const { t } = useMessages()
const { matches, hint } = useCommands()
const toast = useToast()

// «To examples»: the answer on screen kept beside the request it belongs to, under a name asked for
// first: an example is found by its name more often than by its code. Only a saved request
// has somewhere to keep it — a draft's list lives in its tab and would have no row to land on — and
// only an answer that answered: a refusal has no status and nothing worth keeping.
const canKeep = computed(() => Boolean(pane?.nodeId) && isKeepable(record))
const exPop = ref(false)
const exName = ref('')
const exNameEl = useTemplateRef<{ focus(): void; select(): void }>('exNameEl')

watch(exPop, (open) => {
  if (open) exName.value = exampleName(record.status)
})

function focusName() {
  void nextTick(() => {
    exNameEl.value?.focus()
    exNameEl.value?.select()
  })
}

function keepAsExample() {
  const name = exName.value.trim() || exampleName(record.status) || t('request.examples.untitled')
  pane?.saveExamples([...(pane.responses ?? []), exampleOf(record, name)])
  exPop.value = false
  toast.show(t('request.examples.saved', { name }))
}

// The record, read: which readings it leaves and where a person can go from it.
const {
  activeTab,
  bodyView,
  reader,
  views,
  viewLabels,
  selected,
  collapsed,
  bodyInfo,
  toggleNode,
  selectPath,
  expandAll,
  collapseAll,
  tabLabels,
  tabs: availableTabs,
  settle,
  doc,
  isJson,
  jsonValue,
  prettyRaw,
  language,
  follow,
  openCookieManager,
} = useResponseView(
  () => record,
  () => hasHistory.value,
  () => pane
)

// The readings in the segment's own shape: the label each one is drawn under is the catalogue's.
const viewOptions = computed(() =>
  views.value.map((view) => ({ value: view, label: viewLabels.value[view] }))
)

const highlightKey = ref<string | null>(null)

// Switching between history entries keeps the tab being read; only a reading the new record has no
// room for moves.
watch(() => record.id, () => {
  highlightKey.value = null
  inspection.reset()
  settle()
})

function highlightResource(key: string) {
  highlightKey.value = key
}

// The inspector reads the text on screen, so an offset in the viewer means the same place to Go.
const inspection = useInspection(() => ({
  text: prettyRaw.value,
  contentType: record.contentType ?? '',
}))

// The panel is the body's: it is on screen while the body is, and is only then asked anything.
// Nothing is read, selected or marked while it is closed.
const inspecting = computed(() => inspectorState(pane).open && activeTab.value === 'body')

async function select(pick: () => Promise<boolean>) {
  if (inspecting.value) await pick()
}

const inspectNode = (path: string) => select(() => inspection.pickPath(path))
const inspectOffset = (offset: number) => select(() => inspection.pickOffset(offset))

// The tree's own click selects the node it names and, with the panel open, shows it.
function pickNode(path: string) {
  selectPath(path)
  void inspectNode(path)
}

function toggleInspector() {
  setInspectorState(pane, { open: !inspectorState(pane).open })
}

// The body is offered to Go and diagnosed only while the panel is open, and again when the record
// or its body changes. Closing forgets the selection.
watch(
  () => [inspecting.value, record.id, record.responseBody, record.status] as const,
  ([open]) => {
    if (!open) return inspection.reset()
    void inspection.ensure()
    void inspection.diagnose(record.status)
  },
  { immediate: true }
)

// The part of the text the panel is on, marked in the viewer.
const marked = computed(() => {
  const view = inspection.node.value
  return view && inspecting.value ? { head: view.head, start: view.start, end: view.end } : null
})

function openResourceInBody(key: string) {
  highlightKey.value = key
  activeTab.value = 'body'
  bodyView.value = 'tree'
}

const searchShortcut = computed(() => hint('find'))
const inspectorShortcut = computed(() => hint('inspector'))
const inspectorTitle = computed(() =>
  inspectorShortcut.value
    ? t('response.inspector', { shortcut: inspectorShortcut.value })
    : t('inspector.title')
)

const bodyTextViewer = useTemplateRef<TextViewer>('bodyTextViewer')
const textViewer = computed(() => bodyTextViewer.value)

type MapExport = {
  exportFormats: readonly { id: string; label: string }[]
  exportText: (format: string) => string
  matchCount: number
}
const schemaMap = useTemplateRef<MapExport>('schemaMap')
const jsonMap = useTemplateRef<{ matches: number }>('jsonMap')
const jsonTree = useTemplateRef<{ matches: number }>('jsonTree')
const headerTable = useTemplateRef<{ matches: number }>('headerTable')
const cookiesTab = useTemplateRef<{ matches: number }>('cookiesTab')

// A JSON:API document is searched by its own reader: how many resources the query names.
const jsonApiMatches = computed(() =>
  doc.value ? allResources(doc.value).filter((one) => resourceMatchesQuery(one, query.value.trim().toLowerCase())).length : 0
)

// Read here rather than in the tab: the row counts the tests the report holds, and the tab that
// shows them is one of the readings of the same record.
const scriptRuns = ref<ScriptRun[]>([])
const runsLoading = ref(true)

watch(
  () => record.id,
  async (id) => {
    runsLoading.value = true
    scriptRuns.value = (await ScriptingService.Runs(id)) ?? []
    runsLoading.value = false
  },
  { immediate: true }
)

// What the search on screen walks: a JSON:API document is counted by its own readers, and any other
// JSON by the tree or the map that is drawn.
function bodyMatches(): number {
  if (bodyView.value === 'tree') return doc.value ? jsonApiMatches.value : (jsonTree.value?.matches ?? 0)
  if (bodyView.value === 'map') return doc.value ? (schemaMap.value?.matchCount ?? 0) : (jsonMap.value?.matches ?? 0)
  return 0
}

// What stands beside each label: how much the section holds, and, for the scripts, how many of the
// tests passed.
const tabItems = computed(() =>
  availableTabs.value.map((tab) => {
    let count = ''
    let failed = false
    if (tab === 'headers') count = String(record.responseHeaders.length || '')
    if (tab === 'cookies') count = String(record.cookieWrites.length || '')
    if (tab === 'scripts') {
      const tests = scriptRuns.value.flatMap((run) => run.tests ?? [])
      failed = tests.some((one) => !one.passed)
      if (tests.length) count = `${tests.filter((one) => one.passed).length}/${tests.length}`
    }
    return { tab: tab as ResponseTab, label: tabLabels.value[tab], count, failed }
  })
)

// The body, the headers and the cookies have something to search; the timings and the scripts are
// numbers and a report, and the sent request is a snapshot of what went out.
const searchKind = computed<SearchKind | null>(() => {
  if (activeTab.value === 'headers' || activeTab.value === 'cookies') return 'list'
  if (activeTab.value !== 'body') return null
  if (bodyView.value === 'map') return 'map'
  if (bodyView.value === 'raw') return 'text'
  return 'tree'
})

function searchMatches(): number {
  if (activeTab.value === 'headers') return headerTable.value?.matches ?? 0
  if (activeTab.value === 'cookies') return cookiesTab.value?.matches ?? 0
  return bodyMatches()
}

// What the field says about itself: a JSON:API document is searched by its own words, and any other
// reader by a key or a value.
const searchPlaceholder = computed(() => {
  if (activeTab.value === 'headers') return t('response.searchHeaders')
  if (activeTab.value === 'cookies') return t('response.searchCookies')
  if (searchKind.value === 'text') return t('response.text.searchPlaceholder')
  if (!doc.value) return t('response.searchGeneric')
  return searchKind.value === 'map' ? t('json.mapSearch') : t('response.searchPlaceholder')
})

const {
  query,
  searching,
  searchField,
  setStats: setTextStats,
  searchable,
  count: searchCount,
  open: openSearch,
  close: closeSearch,
  onKey: onSearchKey,
} = useResponseSearch({
  kind: () => searchKind.value,
  placeholder: () => searchPlaceholder.value,
  matches: searchMatches,
  textViewer: () => textViewer.value,
})

// The menu's own row for this: the counter is what asks, and the flag is what the row reads — it
// stands false while no answer is on screen, so the row is never offered for nothing to search.
searchResponse.on(() => {
  if (searchable.value) openSearch()
})
watch(searchable, (on) => (responseSearchable.value = on), { immediate: true })
onBeforeUnmount(() => (responseSearchable.value = false))

// One button copies what the reading on screen shows; Cookies, Timings and Scripts have no single
// text, so none.
const { copied, copy, copyText, copyExport } = useResponseCopy({
  record: () => record,
  tab: () => activeTab.value,
  prettyRaw: () => prettyRaw.value,
  exportText: (format) => schemaMap.value?.exportText(format),
})

const { touched: wroteCookies, undoing, undo: undoCookies } = useCookieUndo(() => record)

// The map of a JSON:API document exports in other shapes as well, which is a menu rather than a
// mark: anywhere else the button copies the body as it stands.
const copyIsMenu = computed(() => activeTab.value === 'body' && reader.value === 'map' && !!doc.value)

const bodyEmpty = computed(() => !record.responseBody)
const hintText = computed(() =>
  record.cancelled || record.error
    ? ''
    : bodyEmpty.value
      ? t('response.hintEmptyBody', { status: record.status })
      : ''
)

function onWindowKeydown(e: KeyboardEvent) {
  // What searching the answer and the inspector are bound to comes from the registry.
  if (matches('inspector', e)) {
    e.preventDefault()
    if (activeTab.value === 'body') toggleInspector()
    return
  }
  if (matches('find', e) && searchable.value) {
    e.preventDefault()
    openSearch()
  } else if (e.key === 'Escape' && searching.value && document.activeElement !== searchField.value) {
    closeSearch()
  }
}

useEventListener(window, 'keydown', onWindowKeydown)
</script>

<template>
  <div class="resp">
    <div v-if="record.cancelled" class="resp-error">{{ t('response.cancelled') }}</div>
    <div v-else-if="record.error" class="resp-error">{{ t('response.error', { error: record.error }) }}</div>

    <Tabs v-model="activeTab" class="resp-tabs">
      <div class="tabs-row">
        <TabsList class="resp-strip">
          <TabsTrigger v-for="item in tabItems" :key="item.tab" :value="item.tab">
            <TabText>
              {{ item.label }}
              <span v-if="item.count" class="reading-tab-count" :class="{ failed: item.failed }">{{ item.count }}</span>
            </TabText>
          </TabsTrigger>
        </TabsList>
        <ResponseBar
          :record
          :searchable
          :searching
          :search-title="searchShortcut ? t('response.search', { shortcut: searchShortcut }) : t('common.search')"
          :inspectable="activeTab === 'body'"
          :inspector-open="inspectorState(pane).open"
          :inspector-title="inspectorTitle"
          @toggle-inspector="toggleInspector"
          @toggle-search="searching ? closeSearch() : openSearch()"
        >
          <template #leading>
            <!-- The mark acts on the answer as a whole, so it stands where the answer is read:
                 the body's tab, as the inspector beside it does. -->
            <Popover v-if="activeTab === 'body' && canKeep" v-model:open="exPop">
              <PopoverTrigger as-child>
                <IconButton
                  variant="bare"
                  size="bar"
                  class="row-mark"
                  :hint="t('request.examples.toExamples')"
                >
                  <Icon name="BookmarkPlus" :size="16" :stroke-width="1.8" />
                </IconButton>
              </PopoverTrigger>
              <PopoverContent class="ex-pop" align="end" @open-auto-focus.prevent="focusName">
                <div class="ex-pop-head">
                  <span class="ex-pop-title">{{ t('request.examples.savePopTitle') }}</span>
                  <span class="ex-badge" :class="statusBadgeClass(record.status)">
                    {{ record.status }}
                  </span>
                  <span class="ex-pop-meta">{{ formatBytes(record.responseBytes ?? 0) }}</span>
                </div>
                <label class="ex-pop-field">
                  <span>{{ t('request.examples.saveName') }}</span>
                  <Input
                    ref="exNameEl"
                    v-model="exName"
                    :placeholder="t('request.examples.saveNamePlaceholder')"
                    spellcheck="false"
                    @keydown.enter.prevent="keepAsExample"
                  />
                </label>
                <span class="ex-pop-hint">{{ t('request.examples.savePopHint') }}</span>
                <div class="ex-pop-actions">
                  <button type="button" class="ex-pop-cancel" @click="exPop = false">
                    {{ t('common.cancel') }}
                  </button>
                  <button type="button" class="ex-pop-save" @click="keepAsExample">
                    {{ t('common.save') }}
                  </button>
                </div>
              </PopoverContent>
            </Popover>
          </template>
          <template v-if="activeTab === 'cookies'">
            <IconButton
              v-if="wroteCookies"
              variant="bare"
              size="bar"
              class="row-mark"
              :disabled="undoing"
              :hint="t('cookies.undo')"
              @click="undoCookies"
            >
              <Icon name="Undo2" :size="16" />
            </IconButton>
            <IconButton
              variant="bare"
              size="bar"
              class="row-mark"
              :hint="t('response.cookies.manager')"
              @click="openCookieManager"
            >
              <Icon name="Cookie" :size="16" :stroke-width="1.8" />
            </IconButton>
          </template>
          <DropdownMenu v-else-if="copyIsMenu">
            <DropdownMenuTrigger as-child>
              <!-- The mark carries the meaning and the caret says there is more behind it: a menu
                   is the one control of the row that keeps a second mark, and it keeps no word. -->
              <button
                type="button"
                class="row-action"
                :title="t('common.copy')"
                :aria-label="t('common.copy')"
              >
                <Icon :name="copied ? 'Check' : 'Copy'" :size="16" :stroke-width="1.8" />
                <Icon name="ChevronDown" :size="8" :stroke-width="3" class="row-action-caret" />
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent :side-offset="4">
              <DropdownMenuItem v-for="f in schemaMap?.exportFormats ?? []" :key="f.id" @select="copyExport(f.id)">
                {{ f.label }}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <IconButton
            v-else-if="copyText !== null"
            variant="bare"
            size="bar"
            class="row-mark"
            :hint="copied ? t('common.copied') : t('common.copy')"
            @click="copy(copyText)"
          >
            <Icon :name="copied ? 'Check' : 'Copy'" :size="16" :stroke-width="1.8" />
          </IconButton>
        </ResponseBar>
      </div>

      <div v-if="searching" class="resp-search">
        <Icon name="Search" :size="12" :stroke-width="2" class="resp-search-mark" />
        <input
          ref="searchField"
          v-model="query"
          class="resp-search-input mono"
          :placeholder="searchPlaceholder"
          spellcheck="false"
          @keydown="onSearchKey"
        />
        <span v-if="searchCount" class="resp-search-count">{{ searchCount }}</span>
        <button type="button" class="resp-search-close" :title="t('common.close')" @click="closeSearch">
          <Icon name="X" :size="10" :stroke-width="2.8" />
        </button>
      </div>

      <div class="resp-main">
        <TabsContent class="resp-tab" value="body">
          <!-- A JSON body is read three ways; a body that is not JSON is read as the text it is. -->
          <div v-if="isJson" class="body-bar">
            <Segment
              class="flex-none"
              size="xs"
              :grow="false"
              :options="viewOptions"
              :value="bodyView"
              @pick="bodyView = $event as BodyView"
            />
            <span class="body-info">{{ bodyInfo }}</span>
            <template v-if="bodyView === 'tree'">
              <IconButton
                variant="bare"
                size="xl"
                :hint="t('response.expandAll')"
                @click="expandAll"
              >
                <Icon name="ChevronsUpDown" :size="14" :stroke-width="1.9" />
              </IconButton>
              <IconButton
                variant="bare"
                size="xl"
                :hint="t('response.collapseAll')"
                @click="collapseAll"
              >
                <Icon name="ChevronsDownUp" :size="14" :stroke-width="1.9" />
              </IconButton>
            </template>
          </div>

          <div class="resp-content">
            <template v-if="reader === 'tree'">
              <!-- A JSON:API answer keeps the steps through its pages over the tree: they are where
                   the reader goes next, not something done to the reading. -->
              <template v-if="doc">
                <JsonApiSteps :doc :url="record.url" @follow="follow" />
                <div class="resp-inner">
                  <JsonApiTree
                    :doc
                    :query
                    :highlight-key
                    @select="highlightResource"
                    @inspect="inspectNode"
                    @fetch="follow"
                  />
                </div>
              </template>
              <JsonTree
                v-else
                ref="jsonTree"
                :value="jsonValue"
                :query
                :collapsed
                :selected
                @select="pickNode"
                @toggle="toggleNode"
              />
            </template>
            <template v-else-if="reader === 'map'">
              <SchemaMap
                v-if="doc"
                ref="schemaMap"
                :doc
                :highlight-key
                :query
                @select="openResourceInBody"
                @fetch="follow"
              />
              <JsonMap
                v-else
                ref="jsonMap"
                :value="jsonValue"
                :query
                :selected
                @select="pickNode"
              />
            </template>
            <TextViewerTab
              v-else
              ref="bodyTextViewer"
              :text="prettyRaw"
              :language
              :query
              :mark="marked"
              :picking="inspecting"
              @stats="setTextStats"
              @pick="inspectOffset"
            />

            <div v-if="hintText" class="resp-hint">{{ hintText }}</div>
          </div>
        </TabsContent>

        <TabsContent class="resp-tab" value="headers">
          <div class="resp-content resp-heads">
            <HeaderTable
              ref="headerTable"
              :headers="record.responseHeaders"
              :cookies="record.cookieWrites"
              :empty="t('response.noResponseHeaders')"
              :host="captureHost"
              :query
            />
          </div>
        </TabsContent>

        <TabsContent class="resp-tab" value="cookies">
          <div class="resp-content">
            <CookiesTab ref="cookiesTab" :record :host="captureHost" :query />
          </div>
        </TabsContent>

        <TabsContent class="resp-tab" value="timings">
          <div class="resp-content">
            <TimingsTab :record />
          </div>
        </TabsContent>

        <TabsContent class="resp-tab" value="scripts">
          <div class="resp-content">
            <ScriptsTab :runs="scriptRuns" :loading="runsLoading" />
          </div>
        </TabsContent>

        <TabsContent class="resp-tab" value="request">
          <div class="resp-content resp-pad resp-white">
            <p class="request-caption">{{ t('response.substitutedCaption') }}</p>
            <SentRequest :record />
          </div>
        </TabsContent>
        <NodeInspector
          v-if="inspecting"
          :inspection
          :source
          :pane
          :can-follow="hasHistory"
          @close="setInspectorState(pane, { open: false })"
          @fetch="follow"
          @pick="inspectNode"
        />
      </div>
    </Tabs>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.resp {
  @apply flex flex-col h-full min-h-0 bg-bg;
}

.ex-pop-head {
  @apply flex items-center;
  gap: 0.5rem;
}

.ex-pop-title {
  @apply flex-1 font-semibold;
  font-size: var(--text-13);
}

.ex-badge {
  @apply font-semibold;
  font-family: var(--mono);
  font-size: var(--text-11);
  padding: 2px 6px;
  border-radius: 0.3125rem;
  white-space: nowrap;
}

.ex-pop-meta {
  @apply text-text-tertiary;
  font-size: var(--text-12);
}

.ex-pop-field {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
}

.ex-pop-field span {
  @apply text-text-secondary;
  font-size: var(--text-12);
}

.ex-pop-hint {
  @apply text-text-tertiary;
  font-size: var(--text-12);
  line-height: 1.45;
  text-wrap: pretty;
}

.ex-pop-actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.5rem;
}

.ex-pop-cancel {
  @apply cursor-pointer border-0 text-text;
  height: 1.875rem;
  padding: 0 0.75rem;
  border-radius: 0.4375rem;
  background: var(--chip);
  font-family: inherit;
  font-size: var(--text-13);
}

.ex-pop-save {
  @apply cursor-pointer border-0 font-semibold;
  height: 1.875rem;
  padding: 0 0.875rem;
  border-radius: 0.4375rem;
  background: var(--accent);
  color: var(--accent-text);
  font-family: inherit;
  font-size: var(--text-13);
}

.tabs-row {
  @apply flex items-stretch flex-none gap-0.5;
  box-sizing: content-box;
  height: 2.625rem;
  padding: 0 0.75rem;
  border-bottom: 1px solid var(--line2);
}

.tabs-row :deep(.reading-strip) {
  @apply min-w-0;
  flex: 1 1 auto;
  overflow-x: auto;
  scrollbar-width: none;
}

.resp-strip {
  mask-image: linear-gradient(to right, #000 calc(100% - 28px), transparent);
}

.request-caption {
  @apply m-0 mb-3 text-12 text-text-tertiary;
}

.resp-search {
  @apply flex items-center gap-2 flex-none;
  height: 2.375rem;
  padding: 0 0.75rem 0 1rem;
  background: var(--raise);
  border-bottom: 1px solid var(--line2);
}

.resp-search-mark {
  @apply flex-none text-text-tertiary;
}

.resp-search-input {
  @apply flex-1 min-w-0 border-0 outline-none bg-transparent;
  font-family: var(--mono);
  font-size: var(--text-12-5);
  color: var(--text);
}

.resp-search-input::placeholder {
  color: var(--text-tertiary);
}

.resp-search-count {
  @apply flex-none whitespace-nowrap text-12;
  color: var(--text-tertiary);
}

.resp-search-close {
  @apply flex flex-none items-center justify-center cursor-pointer border-0 p-0 text-text-tertiary;
  width: 1.375rem;
  height: 1.375rem;
  border-radius: 0.375rem;
  background: transparent;
}

.resp-search-close:hover {
  @apply bg-bg-hover text-text;
}

.body-bar {
  @apply flex items-center flex-none gap-2.5;
  height: 2.5rem;
  padding: 0 0.75rem 0 1.25rem;
  overflow: hidden;
}

.body-info {
  @apply flex-1 min-w-0 truncate;
  font-size: var(--text-12);
  color: var(--text-tertiary);
}

/* The one control of the row that is a menu, and the one that keeps a second mark. A plain button
   and not IconButton: it is the menu's own trigger, and a tooltip's root cannot be one. */
.row-action {
  @apply inline-flex flex-none items-center justify-center gap-1 cursor-pointer border-0 text-text-secondary;
  height: 1.75rem;
  padding: 0 0.375rem 0 0.4375rem;
  border-radius: 0.375rem;
  background: transparent;
  --wails-draggable: no-drag;
}

.row-action:hover,
.row-action[data-state='open'] {
  @apply bg-bg-hover text-text;
}

.row-action-caret {
  @apply text-text-tertiary;
}

.resp-error {
  @apply py-2.5 px-3 text-red bg-red-soft text-12;
}

.resp-tab {
  @apply flex-1 min-w-0 min-h-0 flex flex-col;
}

.resp-main {
  @apply flex flex-1 min-h-0;
}

.resp-tabs {
  @apply flex flex-col flex-1 min-h-0;
}

.resp-content {
  @apply flex-1 min-h-0 overflow-auto;
}

.resp-inner {
  @apply flex-1 min-h-0 overflow-auto;
}

.resp-hint {
  @apply text-text-tertiary;
  padding: 0.875rem 1.25rem;
}

.resp-pad {
  @apply py-4.5 px-5 m-0;
}

.resp-heads {
  padding: 0.375rem 1.25rem 1.25rem;
}

.resp-white {
  @apply bg-bg;
}
</style>
