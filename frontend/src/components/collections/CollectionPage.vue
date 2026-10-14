<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Icon from '../ui/Icon.vue'
import TabText from '../ui/TabText.vue'
import { Tabs, TabsList, TabsTrigger } from '../ui/tabs'
import CollectionAuth from './CollectionAuth.vue'
import CollectionDocs from './CollectionDocs.vue'
import CollectionOverview from './CollectionOverview.vue'
import CollectionRuns from './CollectionRuns.vue'
import CollectionScripts from './CollectionScripts.vue'
import CollectionVariables from './CollectionVariables.vue'
import SharePopover from '../publications/SharePopover.vue'
import { useCollectionsStore, type CollectionSection } from '../../stores/collections'
import { usePublicationsStore } from '../../stores/publications'
import { useTransferStore } from '../../stores/transfer'
import { useWorkspacesStore } from '../../stores/workspaces'
import { findCollection } from '../../lib/collectionTree'
import { useMessages } from '../../i18n'
import type { LevelRow, Scripts } from '../../../bindings/schemo/internal/domain'
import { CollectionsService, ScriptingService } from '../../../bindings/schemo/internal/transport/wails'
import { callOrToast } from '../../stores/calls'
import { useLevelRun } from '../../composables/useLevelRun'
import { useWorkspaceSnapshot } from '../../composables/workspace/useWorkspaceSnapshot'

// A collection's page: what it holds, what it authorizes with, what it keeps between requests, what
// it runs around them, and how its last run went — one section at a time, under its own name.
const { t } = useMessages()
const store = useCollectionsStore()
const publications = usePublicationsStore()
const transfer = useTransferStore()
const workspaces = useWorkspacesStore()

const section = ref<CollectionSection>('overview')

// Whether the description opens being written or being read: the overview's preview asks for the
// reader, its «add» line for the field. It is the page that holds the choice, because the section
// is its own.
const docsEdit = ref(false)

function openDocs(edit: boolean) {
  docsEdit.value = edit
  section.value = 'docs'
}

watch(section, (now) => {
  if (now !== 'docs') docsEdit.value = false
})

// The tree's menu asks for a section by name; the page is the one that holds the choice.
watch(
  () => store.pendingSection,
  (asked) => {
    if (!asked) return
    store.pendingSection = null
    section.value = asked
  },
  { flush: 'post' }
)

// The level the page is about: a collections tab names its collection, and a request tab names the
// collection that holds its node — the same answer the tree highlights by.
const level = computed(() => findCollection(store.tree, store.collectionId))
const levelId = computed(() => level.value?.id ?? '')
const title = computed(() => level.value?.name ?? '')

const contentsSnapshot = useWorkspaceSnapshot<LevelRow[]>([])
const { state: requests, loading } = contentsSnapshot
const scriptsSnapshot = useWorkspaceSnapshot<Scripts | null>(null)
const scripts = scriptsSnapshot.state

watch(
  levelId,
  (id, _, onCleanup) => {
    const reset = () => {
      contentsSnapshot.reset()
      scriptsSnapshot.reset()
    }
    onCleanup(reset)
    if (!id) return
    void callOrToast(contentsSnapshot.take(CollectionsService.Contents(id).then(rows => rows ?? [])), 'collections.contentsFailed')
    void callOrToast(scriptsSnapshot.take(ScriptingService.Scripts(id)), 'collections.readFailed')
    void publications.mark(id)
  },
  { immediate: true }
)

const { run, tableRows, resultOf } = useLevelRun(levelId, requests)

const sections = computed<{ key: CollectionSection; label: string; count: string }[]>(() => {
  const pre = scripts.value?.pre?.trim() ? 1 : 0
  const post = scripts.value?.post?.trim() ? 1 : 0
  return [
    { key: 'overview', label: t('collections.subOverview'), count: '' },
    { key: 'docs', label: t('collections.subDocs'), count: '' },
    { key: 'auth', label: t('collections.subAuth'), count: '' },
    { key: 'vars', label: t('collections.subVars'), count: String((level.value?.variables ?? []).length || '') },
    { key: 'scripts', label: t('collections.subScripts'), count: String(pre + post || '') },
    { key: 'runs', label: t('collections.subRuns'), count: run.value ? '1' : '' },
  ]
})

// The contents are the requests the page lists, folders and all: the count is what the table shows.
const countText = computed(() => t('counts.requests', requests.value.length))

// When each row's answer came: the last run's own clock, on the rows it ran.
const overviewRows = computed(() =>
  tableRows.value.map((item) => ({
    ...item,
    when: resultOf(item.row.id) ? (run.value?.finishedAt || run.value?.startedAt || 0) : 0,
  }))
)

const runLabel = computed(() => {
  const running = store.running
  return running ? t('collections.runProgress', { i: running.done + 1, n: running.total }) : t('collections.runAll')
})

async function runAll() {
  await store.run(store.runNodeId, title.value)
}

function importCollection() {
  transfer.askImport(level.value?.id ?? '')
}

function exportLevel() {
  transfer.askExport(level.value?.id ?? '')
}

async function openRow(row: LevelRow) {
  const result = resultOf(row.id)
  if (result) await store.openRunResult(result)
  else await store.select(row.id)
}

function addRequest() {
  if (!levelId.value) return
  store.pendingCreate = levelId.value
}
</script>

<template>
  <div class="col-page">
    <header class="col-head">
      <span class="col-name">{{ title }}</span>
      <span class="col-count">{{ countText }}</span>
      <span class="col-gap"></span>

      <SharePopover v-if="levelId" :collection-id="levelId" />

      <button
        type="button"
        class="col-btn"
        :disabled="store.running !== null || !workspaces.canWrite"
        @click="importCollection"
      >
        <Icon name="Download" :size="14" :stroke-width="1.8" />
        <span class="cap-line">{{ t('collections.import') }}</span>
      </button>
      <button type="button" class="col-btn" :disabled="store.running !== null" @click="exportLevel">
        <Icon name="Upload" :size="14" :stroke-width="1.8" />
        <span class="cap-line">{{ t('collections.export') }}</span>
      </button>

      <span class="col-div"></span>

      <button
        type="button"
        class="col-btn col-run"
        :disabled="requests.length === 0"
        @click="store.running ? store.stop() : runAll()"
      >
        <Icon :name="store.running ? 'Square' : 'Play'" :size="10" />
        <span class="cap-line">{{ runLabel }}</span>
      </button>
      <button type="button" class="col-btn col-new" :disabled="!workspaces.canWrite" @click="addRequest">
        <Icon name="Plus" :size="12" :stroke-width="2.4" />
        <span class="cap-line">{{ t('collections.requestShort') }}</span>
      </button>
    </header>

    <Tabs
      :model-value="section"
      class="col-subtabs"
      @update:model-value="section = $event as CollectionSection"
    >
      <TabsList class="col-strip">
        <TabsTrigger v-for="item in sections" :key="item.key" :value="item.key">
          <TabText>
            {{ item.label }}
            <span v-if="item.count" class="reading-tab-count">{{ item.count }}</span>
          </TabText>
        </TabsTrigger>
      </TabsList>
    </Tabs>

    <div class="col-body">
      <CollectionOverview
        v-if="section === 'overview'"
        :rows="overviewRows"
        :loading
        @open="openRow"
        @open-docs="openDocs"
      />
      <CollectionDocs v-else-if="section === 'docs'" :key="levelId" :id="levelId" :start-editing="docsEdit" />
      <CollectionAuth v-else-if="section === 'auth'" :level-id="levelId" />
      <CollectionVariables v-else-if="section === 'vars'" />
      <CollectionScripts
        v-else-if="section === 'scripts'"
        :level-id="levelId"
        :scripts
        @saved="scriptsSnapshot.replace($event)"
      />
      <CollectionRuns v-else :run :level-id="levelId" />
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.col-page {
  @apply flex flex-col flex-1 min-h-0 bg-bg;
}

.col-head {
  @apply flex items-center flex-none;
  height: 3.75rem;
  gap: 0.5rem;
  padding: 0 1.5rem;
}

.col-name {
  @apply min-w-0 truncate font-semibold;
  font-size: var(--text-17);
  letter-spacing: -0.01em;
}

.col-count {
  @apply flex-none whitespace-nowrap text-text-tertiary;
  margin-left: 0.375rem;
  font-size: var(--text-12-5);
}

.col-gap {
  @apply flex-1;
}

.col-btn {
  @apply flex items-center justify-center flex-none cursor-pointer border-0;
  height: 2rem;
  padding: 0 0.6875rem;
  gap: 0.4375rem;
  border-radius: 0.5rem;
  background: transparent;
  color: var(--text);
  font-family: inherit;
  font-size: var(--text-13);
}

.col-btn:hover:not(:disabled) {
  background: var(--bg-hover);
}

.col-btn:disabled {
  opacity: 0.45;
  cursor: default;
}

.col-div {
  @apply flex-none;
  width: 1px;
  height: 1.125rem;
  margin: 0 0.25rem;
  background: var(--border);
}

.col-run {
  padding: 0 0.75rem;
  min-width: 8.25rem;
  box-shadow: inset 0 0 0 1px var(--border);
}

.col-new {
  padding: 0 0.75rem;
  gap: 0.375rem;
  font-weight: 500;
  background: var(--accent);
  color: var(--accent-text);
}

.col-new:hover:not(:disabled) {
  background: var(--accent);
  opacity: 0.9;
}

.col-subtabs {
  @apply flex flex-none;
  height: 2.125rem;
  padding: 0 1rem;
  border-bottom: 1px solid var(--border);
}

.col-strip {
  @apply flex-1 items-end min-w-0;
  gap: 1.25rem;
}

.col-strip :deep(.reading-tab) {
  padding-bottom: 0.5625rem;
}

.col-body {
  @apply flex flex-col flex-1 min-h-0;
}
</style>
