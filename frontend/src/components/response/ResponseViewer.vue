<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onBeforeUnmount, ref, useTemplateRef, watch } from 'vue'
import { useEventListener } from '@vueuse/core'
import HeaderTable from './HeaderTable.vue'
import JsonApiSteps from './JsonApiSteps.vue'
import ResponseBar from './ResponseBar.vue'
import SentRequest from './SentRequest.vue'
import Icon from '../ui/Icon.vue'
import TabText from '../ui/TabText.vue'
import IconButton from '../ui/IconButton.vue'
import Input from '../ui/Input.vue'
import PopoverContent from '../ui/PopoverContent.vue'
import { PopoverRoot as Popover, PopoverTrigger, TabsRoot as Tabs, TabsContent, DropdownMenuRoot as DropdownMenu, DropdownMenuTrigger } from 'reka-ui'
import TabsList from '../ui/TabsList.vue'
import TabsTrigger from '../ui/TabsTrigger.vue'
import Segment from '../ui/Segment.vue'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import DropdownMenuItem from '../ui/DropdownMenuItem.vue'
import type { RecordView } from '../../lib/requestRecord'
import JsonApiTree from '../json/JsonApiTree.vue'
import JsonTree from '../json/JsonTree.vue'
import JsonMap from '../json/JsonMap.vue'
import JsonTable from '../json/JsonTable.vue'
import SchemaMap from '../json/SchemaMap.vue'
import ResponseNodeBar from '../inspector/ResponseNodeBar.vue'
import CookiesTab from './CookiesTab.vue'
import TimingsTab from './TimingsTab.vue'
import ScriptsTab from './ScriptsTab.vue'
import type { PaneSource } from '../../stores/content'
import { formatBytes, t } from '../../i18n'
import { useCommands } from '../../composables/useCommands'
import { useIndent } from '../../composables/useIndent'
import { statusBadgeClass } from '../../lib/format'
import { useInspection } from '../../composables/useInspection'
import {
  useCookieUndo,
  useKeepExample,
  useResponseCopy,
  useResponseSearch,
  useResponseView,
  type SearchKind,
  type TextViewer,
} from '../../composables/useResponseView'
import { useScriptRuns } from '../../composables/useScriptRuns'
import { responseNodeActions, responseNodeSelected, responseSearchable, searchResponse } from '../../lib/intents'
import { allResources, resourceMatchesQuery } from '../../lib/jsonapi'
import { prettyJson } from '../../lib/json'
import { tabBadge, type BodyView } from '../../lib/responseView'
import { callOrToast } from '../../lib/calls'
import { formatResponse, responseParser } from '../../lib/responseFormat'

const RawViewer = defineAsyncComponent(() => import('./RawViewer.vue'))

// Where this pane is drawn: beside the command line, beside a captured request, or inside a
// collection card.
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
const { matches, hint } = useCommands()

const { canKeep, isKeeping, draftName, exampleNameField, focusName, keepAsExample } = useKeepExample(
  () => record,
  () => pane
)

// The record, read: which readings it leaves and where a person can go from it.
const {
  activeTab,
  bodyView,
  reader,
  viewOptions,
  nodes,
  selected,
  collapsed,
  bodyInfo,
  toggleNode,
  selectPath,
  expandAll,
  collapseAll,
  tabLabels,
  tabs: availableTabs,
  keepReadingAvailable,
  doc,
  jsonValue,
  arrays,
  language,
  follow,
  openCookieManager,
} = useResponseView(
  () => record,
  () => hasHistory.value,
  () => pane
)

const { indent } = useIndent()
const parser = computed(() => responseParser(record.contentType, language.value))
const canFormat = computed(() =>
  activeTab.value === 'body' && reader.value === 'raw' && !!parser.value && !!record.responseBody.trim()
)
const formattedBody = ref<string | null>(null)
const formatting = ref(false)
let formatRequest = 0
const displayedBody = computed(() =>
  reader.value === 'raw' ? formattedBody.value ?? record.responseBody : record.responseBody
)

async function toggleFormatting() {
  const selectedParser = parser.value
  if (!selectedParser || formatting.value) return
  if (formattedBody.value !== null) {
    formattedBody.value = null
    return
  }
  const request = ++formatRequest
  formatting.value = true
  const text = await callOrToast(
    formatResponse(record.responseBody, selectedParser, indent.value),
    'response.raw.formatFailed'
  )
  if (request !== formatRequest) return
  formatting.value = false
  if (text !== undefined) formattedBody.value = text
}

const { runs: scriptRuns, isLoading: runsLoading } = useScriptRuns(() => record.id)

const inspectable = computed(() => activeTab.value === 'body' && reader.value !== 'raw')
// The inspector reads the same JSON value as the tree, including how it resolves duplicate keys.
const inspectionBody = computed(() => inspectable.value ? prettyJson(jsonValue.value) : '')
const inspection = useInspection(() => inspectionBody.value)
const { inlineReadings } = inspection

const highlightKey = ref<string | null>(null)
const nodeBar = useTemplateRef<InstanceType<typeof ResponseNodeBar>>('nodeBar')
const node = computed(() => {
  if (!inspectable.value) return null
  const view = inspection.node.value
  return view && view.path !== '$' && view.path === selected.value ? view : null
})

// The view stays on the same reading when the response changes. Its selection belongs to the old body.
watch([() => record.id, () => record.responseBody, () => record.contentType], () => {
  formatRequest++
  formattedBody.value = null
  formatting.value = false
  highlightKey.value = null
  inspection.reset()
  selectPath('$')
  keepReadingAvailable()
})

function highlightResource(key: string) {
  highlightKey.value = key
}

async function pickNode(path: string, event?: MouseEvent) {
  if (!inspectable.value || !(await inspection.pickPath(path))) return
  const picked = inspection.node.value
  if (!picked) return
  selectPath(picked.path)
  if (event) {
    await nextTick()
    if (node.value === picked) nodeBar.value?.openActions(event)
  }
}

function collapseTree() {
  inspection.clearSelection()
  collapseAll()
}

watch(() => pane, inspection.reset)
watch([() => pane, inspectable], () => {
  if (!inspectable.value) return
  if (selected.value !== '$' && inspection.node.value?.path !== selected.value) {
    void pickNode(selected.value)
  }
}, { immediate: true })

const showsTree = computed(() => activeTab.value === 'body' && reader.value === 'tree')
// A pane change can reset the inspector in the same update as a response change.
watch([() => pane, () => record.id, () => record.responseBody, () => record.contentType, showsTree], () => {
  if (showsTree.value) void inspection.loadInlineReadings()
}, { immediate: true, flush: 'post' })

responseNodeActions.on(() => nodeBar.value?.openActions())
watch(() => !!node.value, (available) => {
  responseNodeSelected.value = available
}, { immediate: true })
onBeforeUnmount(() => (responseNodeSelected.value = false))

function openResourceInBody(key: string) {
  highlightKey.value = key
  activeTab.value = 'body'
  bodyView.value = 'tree'
}

// What stands beside each label: how much the section holds, and, for the scripts, how many of the
// tests passed.
const tabItems = computed(() =>
  availableTabs.value.map((tab) => ({
    tab,
    label: tabLabels.value[tab],
    ...tabBadge(tab, record, scriptRuns.value),
  }))
)

// The readers that count their own matches, asked for by the search below.
type MapExport = {
  exportFormats: readonly { id: string; label: string }[]
  exportText: (format: string) => string
  matchCount: number
}
const bodyTextViewer = useTemplateRef<TextViewer>('bodyTextViewer')
const schemaMap = useTemplateRef<MapExport>('schemaMap')
const jsonMap = useTemplateRef<{ matches: number }>('jsonMap')
const jsonTree = useTemplateRef<{ matches: number }>('jsonTree')
const jsonTable = useTemplateRef<{ matches: number }>('jsonTable')
const headerTable = useTemplateRef<{ matches: number }>('headerTable')
const cookiesTab = useTemplateRef<{ matches: number }>('cookiesTab')

// A JSON:API document is searched by its own reader: how many resources the query names.
function jsonApiMatches(query: string): number {
  if (!doc.value) return 0
  const needle = query.trim().toLowerCase()
  return allResources(doc.value).filter((resource) => resourceMatchesQuery(resource, needle)).length
}

// What the search on screen walks: a JSON:API document is counted by its own readers, and any other
// JSON by the tree or the map that is drawn.
function bodyMatches(query: string): number {
  if (reader.value === 'table') return jsonTable.value?.matches ?? 0
  if (reader.value === 'tree') return doc.value ? jsonApiMatches(query) : (jsonTree.value?.matches ?? 0)
  if (reader.value === 'map') return doc.value ? (schemaMap.value?.matchCount ?? 0) : (jsonMap.value?.matches ?? 0)
  return 0
}

// The body, the headers and the cookies have something to search; the timings and the scripts are
// numbers and a report, and the sent request is a snapshot of what went out.
const searchKind = computed<SearchKind | null>(() => {
  if (activeTab.value === 'headers' || activeTab.value === 'cookies') return 'list'
  if (activeTab.value !== 'body') return null
  if (reader.value === 'table') return 'list'
  if (reader.value === 'raw') return 'text'
  return reader.value
})

function searchMatches(query: string): number {
  if (activeTab.value === 'headers') return headerTable.value?.matches ?? 0
  if (activeTab.value === 'cookies') return cookiesTab.value?.matches ?? 0
  return bodyMatches(query)
}

// What the field says about itself: a JSON:API document is searched by its own words, and any other
// reader by a key or a value.
const searchPlaceholder = computed(() => {
  if (activeTab.value === 'body' && reader.value === 'table') return t('response.table.filter')
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
  textViewer: () => bodyTextViewer.value,
})

const searchTitle = computed(() => {
  const shortcut = hint('find')
  return shortcut ? t('response.search', { shortcut }) : t('common.search')
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
  bodyText: () => displayedBody.value,
  exportText: (format) => schemaMap.value?.exportText(format),
})

const { touched: wroteCookies, undoing, undo: undoCookies } = useCookieUndo(() => record)

const copyIsMenu = computed(() =>
  inspectable.value && (!!node.value || !!schemaMap.value?.exportFormats.length)
)

async function copySelectedValue() {
  const picked = node.value
  if (!picked) return
  const value = await inspection.valueOf(picked)
  if (value !== null) await copy(value)
}

// An answer that came back empty says so; a cancelled or failed one already has its own banner.
const hintText = computed(() => {
  if (record.cancelled || record.error || record.responseBody) return ''
  return t('response.hintEmptyBody', { status: record.status })
})

function onWindowKeydown(e: KeyboardEvent) {
  // What searching the answer and the inspector are bound to comes from the registry.
  if (matches('inspector', e)) {
    if (node.value) {
      e.preventDefault()
      if (!e.repeat) nodeBar.value?.openActions()
    }
  } else if (matches('find', e) && searchable.value) {
    e.preventDefault()
    if (!e.repeat) openSearch()
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
          :search-title="searchTitle"
          @toggle-search="searching ? closeSearch() : openSearch()"
        >
          <template #leading>
            <Popover v-if="activeTab === 'body' && canKeep" v-model:open="isKeeping">
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
                    ref="exampleNameField"
                    v-model="draftName"
                    :placeholder="t('request.examples.saveNamePlaceholder')"
                    spellcheck="false"
                    @keydown.enter.prevent="keepAsExample"
                  />
                </label>
                <span class="ex-pop-hint">{{ t('request.examples.savePopHint') }}</span>
                <div class="ex-pop-actions">
                  <button type="button" class="ex-pop-cancel" @click="isKeeping = false">
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
            <DropdownMenuContent class="copy-menu" :side-offset="4">
              <DropdownMenuItem @select="copy(copyText ?? '')">{{ t('response.copyBody') }}</DropdownMenuItem>
              <template v-if="node">
                <DropdownMenuItem @select="copySelectedValue">{{ t('response.copySelected') }}</DropdownMenuItem>
                <DropdownMenuItem @select="copy(node.path)">{{ t('inspector.copyPath') }}</DropdownMenuItem>
              </template>
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
          <IconButton
            v-if="canFormat"
            variant="bare"
            size="bar"
            class="row-mark"
            :class="{ on: formattedBody !== null }"
            :hint="t('response.raw.format')"
            :aria-label="t('response.raw.format')"
            :aria-pressed="formattedBody !== null"
            :disabled="formatting"
            @click="toggleFormatting"
          >
            <Icon name="AlignLeft" :size="16" :stroke-width="1.8" />
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
          <!-- JSON arrays also have a table; other formats are read as text. -->
          <div v-if="viewOptions.length > 1" class="body-bar">
            <Segment
              class="flex-none"
              size="xs"
              :grow="false"
              :options="viewOptions"
              :value="reader"
              @pick="bodyView = $event as BodyView"
            />
            <ResponseNodeBar
              v-if="node"
              ref="nodeBar"
              :node
              :inspection
              :host="captureHost"
              :body="inspectionBody"
              :status="record.status"
              :can-follow="hasHistory"
              @pick="pickNode"
              @follow="follow"
            />
            <span v-else-if="reader !== 'raw'" class="body-info">{{ bodyInfo }}</span>
            <template v-if="!doc && reader === 'tree'">
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
                @click="collapseTree"
              >
                <Icon name="ChevronsDownUp" :size="14" :stroke-width="1.9" />
              </IconButton>
            </template>
          </div>

          <div class="resp-content" :class="{ 'resp-viewport': reader === 'table' || (!doc && reader !== 'raw') }">
            <JsonTable
              v-if="reader === 'table'"
              ref="jsonTable"
              :arrays
              :selected
              :query
              @inspect="pickNode"
            />
            <template v-else-if="reader === 'tree'">
              <!-- A JSON:API answer keeps the steps through its pages over the tree: they are where
                   the reader goes next, not something done to the reading. -->
              <template v-if="doc">
                <JsonApiSteps :doc :url="record.url" @follow="follow" />
                <div class="resp-inner">
                  <JsonApiTree
                    :doc
                    :query
                    :highlight-key
                    :readings="inlineReadings"
                    @select="highlightResource"
                    @inspect="pickNode"
                    @fetch="follow"
                  />
                </div>
              </template>
              <JsonTree
                v-else
                ref="jsonTree"
                :nodes
                :readings="inlineReadings"
                :query
                :collapsed
                :selected
                @inspect="pickNode"
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
                :nodes
                :query
                :selected
                @inspect="pickNode"
              />
            </template>
            <RawViewer
              v-else
              ref="bodyTextViewer"
              :text="displayedBody"
              :language
              :query
              @stats="setTextStats"
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
  gap: 0.3125rem;
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
  font-size: var(--text-13);
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
  border-radius: 0.3125rem;
  background: transparent;
}

.resp-search-close:hover {
  @apply bg-bg-hover text-text;
}

.body-bar {
  @apply flex items-center flex-none gap-2.5;
  height: 2.5rem;
  padding: 0.625rem 0.8125rem 0.625rem 0.8125rem;
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
  padding: 0 0.3125rem 0 0.4375rem;
  border-radius: 0.3125rem;
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

.resp-viewport {
  @apply flex flex-col overflow-hidden;
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
  padding: 0.3125rem 1.25rem 1.25rem;
}

.resp-white {
  @apply bg-bg;
}
</style>
