<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { Browser } from '@wailsio/runtime'
import Icon from '../ui/Icon.vue'
import PublicationFields from './PublicationFields.vue'
import PublicationOptions from './PublicationOptions.vue'
import PublicationPassword from './PublicationPassword.vue'
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator } from '../ui/dropdown-menu'
import type { PublicationDetails } from '../../../bindings/schemo/internal/domain'
import { PublicationsService } from '../../../bindings/schemo/internal/transport/wails'
import { hostOf, usePublicationsStore } from '../../stores/publications'
import { useCollectionsStore } from '../../stores/collections'
import { useTabsStore } from '../../stores/tabs'
import { useWorkspacesStore } from '../../stores/workspaces'
import { callOrToast } from '../../stores/calls'
import { useWorkspaceSnapshot } from '../../composables/workspace/useWorkspaceSnapshot'
import { useToast } from '../../composables/useToast'
import { copyToClipboard } from '../../lib/clipboard'
import { findCollection } from '../../lib/collectionTree'
import { changeNotes, passwordFits } from '../../lib/publications'
import { PASSWORD } from '../../lib/layout'
import { methodInkClass } from '../../lib/format'
import { formatAgo, useMessages } from '../../i18n'

const { t } = useMessages()
const store = usePublicationsStore()
const collections = useCollectionsStore()
const tabs = useTabsStore()
const toast = useToast()
const workspaces = useWorkspacesStore()
const details = useWorkspaceSnapshot<PublicationDetails | null>(null)
const restoring = ref(0)
const initializing = ref(store.pages.length === 0)
const opening = ref(true)
let closed = false
let editing: Promise<unknown> = Promise.resolve()

const selected = computed({
  get: () => store.selectedCollection,
  set: (id: string) => { store.selectedCollection = id },
})

onMounted(async () => {
  try {
    await store.load()
    if (closed) return
    await store.refresh()
    if (closed) return
    await store.markAll(store.pages.map((page) => page.collectionId))
  } finally {
    initializing.value = false
  }
})

const live = computed(() => store.pages.filter((page) => page.status === 'active' && !page.pendingUnpublish))

const current = computed(() => {
  const list = live.value
  return list.find((page) => page.collectionId === selected.value) ?? list[0] ?? null
})

const host = computed(() => hostOf(live.value[0]?.url ?? ''))
const head = computed(() =>
  host.value ? t('publications.headCount', { n: live.value.length, host: host.value }) : ''
)

// The rail, each row with what its badge says: how many requests the page behind that name is
// missing. The mark is this device's own, so the counts are drawn without asking anybody.
const rail = computed(() =>
  live.value.map((page) => {
    const mark = store.markOf(page.collectionId)
    return {
      page,
      behind: mark?.behind === true,
      missing: mark?.changes?.length ?? 0,
    }
  })
)

const mark = computed(() => (current.value ? store.markOf(current.value.collectionId) : null))
const behind = computed(() => store.collection === current.value?.collectionId ? store.behind : mark.value?.behind === true)
const notes = computed(() => behind.value ? changeNotes(store.changes) : [])
const editable = computed(() => !!current.value?.canManage && workspaces.canWrite)
const disabled = computed(() => !editable.value || store.busy || store.reading || restoring.value !== 0)
const passwordOk = computed(() => store.visibility !== 'password' || passwordFits(store.password) || (!store.password && current.value?.visibility === 'password'))
const pending = computed(() => behind.value || store.visibility !== current.value?.visibility || store.passwordChanged)
const environmentLabel = computed(() => store.environments.find(choice => choice.id === store.environment)?.name ?? current.value?.environmentName ?? t('publications.envNone'))

watch([() => current.value?.collectionId, () => current.value?.revision], async ([collectionId], [previousCollectionId]) => {
  const page = current.value
  if (!page) { store.closeShare(); return }
  const changed = collectionId !== previousCollectionId
  if (changed) {
    opening.value = true
    details.reset()
    editing = store.edit(page.collectionId)
  }
  const signal = details.signal()
  const revision = page.revision
  await Promise.all([editing, readDetails()])
  if (!signal.aborted && current.value?.collectionId === collectionId && current.value.revision === revision) opening.value = false
}, { immediate: true })
onUnmounted(() => {
  closed = true
  store.closeShare()
})

async function readDetails() {
  const page = current.value
  if (page) await callOrToast(details.take(PublicationsService.Details(page.collectionId)), 'publications.readFailed')
}

async function restore(revision: number) {
  const page = current.value
  if (!page || disabled.value) return
  restoring.value = revision
  const signal = details.signal()
  try {
    const restored = await callOrToast(PublicationsService.Restore(workspaces.activeId, page.collectionId, revision).cancelOn(signal), 'publications.restoreFailed')
    if (!restored || signal.aborted) return
    await store.markAll(Object.keys(store.marks))
    if (!signal.aborted) await store.load()
  } finally { restoring.value = 0 }
}

const name = computed(() => {
  const page = current.value
  if (!page) return ''
  return findCollection(collections.tree, page.collectionId)?.name || page.slug || ''
})
const address = computed(() => current.value?.url?.replace(/^https?:\/\//, '') ?? '')

function accessOf(visibility: string | undefined): string {
  if (visibility === 'public') return t('publications.visPublic')
  if (visibility === 'password') return t('publications.visPassword')
  return t('publications.visUnlisted')
}

function revisionOf(revision: number | undefined): string {
  return `v${revision ?? 0}`
}

function openPage() {
  const url = current.value?.url
  if (url) void Browser.OpenURL(url)
}

async function copy() {
  const url = current.value?.url
  if (url && (await copyToClipboard(url))) toast.show(t('publications.copied'))
}

function openCollection() {
  const page = current.value
  if (page) void tabs.openCollection(page.collectionId)
}

async function update() {
  if (disabled.value || !passwordOk.value) return
  if (await store.publish(true)) await readDetails()
}
</script>

<template>
  <div class="pubs-page">
    <header class="pubs-head">
      <span class="pubs-title">{{ t('publications.title') }}</span>
      <span class="pubs-count">{{ head }}</span>
    </header>

    <div v-if="(initializing || store.loading) && live.length === 0" class="pubs-empty">
      <span class="spinner spinner-lg"></span>
    </div>

    <div v-else-if="live.length === 0" class="pubs-empty">
      <div class="empty-card">
        <span class="empty-tile"><Icon name="Radio" :size="24" :stroke-width="1.5" /></span>
        <div class="empty-text">
          <span class="empty-title">{{ t('publications.emptyTitle') }}</span>
          <span class="empty-hint">{{ t('publications.emptyHint') }}</span>
        </div>
      </div>
    </div>

    <div v-else class="pubs-body">
      <div class="pubs-rail">
        <button
          v-for="row in rail"
          :key="row.page.collectionId"
          type="button"
          class="rail-row"
          :class="{ active: row.page.collectionId === current?.collectionId }"
          @click="selected = row.page.collectionId"
        >
          <span class="rail-dot" :class="{ behind: row.behind }"></span>
          <span class="rail-text">
            <span class="rail-name">
              {{ findCollection(collections.tree, row.page.collectionId)?.name || row.page.slug }}
            </span>
            <span class="rail-meta">
              {{ accessOf(row.page.visibility) }} · {{ revisionOf(row.page.revision) }}
            </span>
          </span>
          <span v-if="row.missing" class="rail-badge" :title="t('publications.changesBadge')">
            {{ row.missing }}
          </span>
        </button>
      </div>

      <div v-if="current" class="pubs-pane">
        <div v-if="opening || initializing" class="pubs-empty">
          <span class="spinner spinner-lg"></span>
        </div>
        <div v-show="!opening && !initializing" class="pane-inner">
          <div class="pane-top">
            <div class="pane-head">
              <div class="pane-title-row">
                <span class="pane-name">{{ name }}</span>
                <div class="pane-actions">
                  <button v-if="pending && editable" type="button" class="update" :disabled="disabled || !passwordOk" @click="update">
                    <Icon name="ArrowUp" :size="14" :stroke-width="2.2" />
                    {{ store.busy ? t('publications.updating') : behind ? t('publications.updateTo', { n: (current.revision ?? 0) + 1 }) : t('common.save') }}
                  </button>
                  <DropdownMenu>
                    <DropdownMenuTrigger as-child>
                      <button type="button" class="more-button" :title="t('publications.actions')" :aria-label="t('publications.actions')"><Icon name="Ellipsis" :size="16" /></button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent class="publication-actions-menu" align="end" :side-offset="6">
                      <DropdownMenuItem @select="openPage"><Icon name="ExternalLink" :size="15" />{{ t('publications.openPage') }}</DropdownMenuItem>
                      <DropdownMenuItem @select="openCollection"><Icon name="Folder" :size="15" />{{ t('publications.openCollection') }}</DropdownMenuItem>
                      <template v-if="editable">
                        <DropdownMenuSeparator />
                        <DropdownMenuItem class="bad" @select="store.askUnpublish(current.collectionId)"><Icon name="Link2Off" :size="15" />{{ t('publications.unpublish') }}</DropdownMenuItem>
                      </template>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </div>
              <span v-if="details.state.value" class="pane-meta" :title="t('publications.statisticsHint')">
                {{ t('publications.statistics', {
                  views: t('publications.views', details.state.value.views),
                  imports: t('publications.imports', details.state.value.imports),
                }) }}
              </span>
            </div>

            <div class="pane-url">
              <div class="url-text">
                <span class="url-address" :title="current.url">{{ address }}</span>
                <span
                  v-if="current.visibility === 'password'"
                  class="url-lock"
                  :title="t('publications.visPassword')"
                >
                  <Icon name="Lock" :size="12" :stroke-width="1.9" />
                </span>
                <button type="button" class="copy-btn" :title="t('publications.copyLink')" :aria-label="t('publications.copyLink')" @click="copy"><Icon name="Copy" :size="14" /></button>
              </div>
            </div>
          </div>

          <div class="pane-changes">
            <div class="changes-head">
              <span class="changes-title">{{ t('publications.changes') }}</span>
              <span v-if="notes.length" class="changes-count">{{ t('publications.unpublishedChanges', { n: notes.length }) }}</span>
            </div>
            <div v-if="!pending" class="changes-none">
              <Icon name="Check" :size="13" :stroke-width="2.6" class="mark-ok" />
              <span>{{ t('publications.noChanges') }}</span>
            </div>
            <div v-else-if="notes.length === 0" class="changes-none loose">
              <Icon name="TriangleAlert" :size="13" :stroke-width="2.2" class="mark-warn" />
              <span>{{ t('publications.changesPending') }}</span>
            </div>
            <div v-for="(row, at) in notes" :key="at" class="chg-row">
              <span class="chg-sign" :class="row.tone">{{ row.sign }}</span>
              <span v-if="row.method" class="chg-method" :class="methodInkClass(row.method)">
                {{ row.method }}
              </span>
              <span class="chg-name">{{ row.name }}</span>
              <span class="chg-what" :title="row.what">{{ row.what }}</span>
            </div>
          </div>

          <section class="pane-settings">
            <div class="changes-head"><span class="changes-title">{{ t('publications.settings') }}</span></div>
            <PublicationFields :visibility="store.visibility" :environment="store.environment"
              :environment-label="environmentLabel" :environments="store.environments" :disabled="disabled"
              @visibility="store.visibility = $event" @environment="store.choose" />
            <PublicationPassword v-if="store.visibility === 'password'" :key="current.collectionId" v-model="store.password" class="password-row" tools save :disabled="disabled"
              :placeholder="t(current.visibility === 'password' ? 'publications.passwordUnavailable' : 'publications.newPassword')"
              :save-disabled="!store.passwordChanged || !passwordFits(store.password)" @save="update" />
            <span v-if="store.password && !passwordOk" class="password-note">{{ t('publications.passwordLength', { min: PASSWORD.min, max: PASSWORD.max }) }}</span>
            <PublicationOptions v-if="store.options" :options="store.options" :disabled="disabled" @choose="store.chooseOption" />
          </section>

          <section class="pane-versions">
            <div class="changes-head"><span class="changes-title" :title="t('publications.versionsHint')">{{ t('publications.versions') }}</span></div>
            <span v-if="details.loading.value" class="spinner"></span>
            <div v-for="version in details.state.value?.versions ?? []" :key="version.revision" class="version-row">
              <span class="version-number">v{{ version.revision }}</span>
              <span class="version-summary">
                {{ formatAgo(version.publishedAt) }}
                <span class="version-note">
                  ·
                  <template v-if="version.restoredFrom">{{ t('publications.restoredFrom', { n: version.restoredFrom }) }}</template>
                  <template v-else>{{ t('publications.versionRequests', { n: version.requests }) }}<template v-if="version.environmentName"> · {{ version.environmentName }}</template></template>
                </span>
              </span>
              <span v-if="version.revision === current.revision" class="version-current">{{ t('publications.onPage') }}</span>
              <button v-else-if="editable" type="button" class="link" :disabled="disabled" @click="restore(version.revision)">
                {{ t(restoring === version.revision ? 'publications.restoring' : 'publications.restore') }}
              </button>
            </div>
            <button v-if="!details.state.value && !details.loading.value" type="button" class="link" @click="readDetails">{{ t('publications.retryDetails') }}</button>
          </section>

        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.pubs-page {
  @apply flex-1 min-h-0 flex flex-col;
  line-height: normal;
}

.pubs-head {
  @apply flex items-center flex-none;
  height: 4rem;
  padding: 0 1.5rem;
  gap: 0.875rem;
  border-bottom: 1px solid var(--border);
  box-sizing: border-box;
}

.pubs-title {
  font-size: var(--text-17);
  font-weight: 600;
  letter-spacing: -0.01em;
  flex: none;
}

.pubs-count {
  @apply min-w-0 truncate text-text-tertiary;
  font-size: var(--text-12-5);
}

.pubs-empty {
  @apply flex-1 min-h-0 overflow-auto;
  padding: 2rem 1.5rem;
}

.empty-card {
  @apply flex items-start;
  max-width: 40rem;
  gap: 1rem;
}

.empty-tile {
  @apply flex items-center justify-center flex-none text-text-secondary;
  width: 3rem;
  height: 3rem;
  border-radius: 0.75rem;
  background: var(--chip);
}

.empty-text {
  @apply flex flex-col min-w-0;
  gap: 0.375rem;
}

.empty-title {
  font-size: var(--text-20);
  font-weight: 600;
  letter-spacing: -0.015em;
}

.empty-hint {
  @apply text-text-secondary;
  font-size: var(--text-14);
  line-height: 1.5;
  text-wrap: pretty;
}

.pubs-body {
  @apply flex-1 min-h-0 flex;
}

.pubs-rail {
  @apply flex flex-col flex-none overflow-auto;
  width: 16.25rem;
  padding: 0.5rem;
  gap: 0.125rem;
  border-right: 1px solid var(--border);
}

.rail-row {
  @apply flex items-center flex-none cursor-default text-left;
  gap: 0.625rem;
  padding: 0.5625rem 0.625rem;
  border: 0;
  border-radius: 0.5rem;
  background: transparent;
  color: var(--text);
  font-family: inherit;
  font-size: var(--text-13);
}

.rail-row:hover {
  background: var(--bg-hover);
}

.rail-row.active {
  background: var(--bg-hover);
}

.rail-dot {
  @apply flex-none rounded-full;
  width: 0.4375rem;
  height: 0.4375rem;
  background: var(--green);
}

.rail-dot.behind {
  background: var(--orange);
}

.rail-text {
  @apply flex flex-col flex-1 min-w-0;
  gap: 0.125rem;
}

.rail-name {
  @apply truncate font-medium;
}

.rail-meta {
  @apply truncate text-text-tertiary;
  font-size: var(--text-12);
}

.rail-badge {
  @apply flex items-center justify-center flex-none text-orange;
  box-sizing: border-box;
  min-width: 1.125rem;
  height: 1.125rem;
  padding: 0 0.3125rem;
  border-radius: 0.5625rem;
  background: color-mix(in srgb, var(--orange) 14%, transparent);
  font-size: var(--text-11);
  font-weight: 600;
}

.pubs-pane {
  @apply flex-1 min-w-0 overflow-auto;
  padding: 1.5rem 1.75rem 2rem;
}

.pane-inner {
  @apply flex flex-col;
  max-width: 61.25rem;
  gap: 1.75rem;
}

.pane-top {
  @apply flex flex-col;
  gap: 0.75rem;
}

.pane-head {
  @apply flex flex-col;
  gap: 0.25rem;
}

.pane-title-row {
  @apply flex items-center flex-wrap;
  gap: 0.5rem 0.625rem;
}

.pane-name {
  @apply truncate;
  flex: 1 1 8.75rem;
  min-width: 7.5rem;
  font-size: var(--text-20);
  font-weight: 600;
  letter-spacing: -0.015em;
}

.pane-meta {
  @apply text-text-tertiary;
}

.link {
  @apply flex items-center flex-none cursor-pointer border-0 whitespace-nowrap bg-transparent text-accent;
  gap: 0.3125rem;
  font-family: inherit;
  font-size: var(--text-12-5);
}

.pane-url {
  @apply flex;
  gap: 0.375rem;
}

.url-text {
  @apply flex items-center flex-1 min-w-0;
  gap: 0.5rem;
  height: 2.125rem;
  padding: 0 0.75rem;
  border-radius: 0.5rem;
  background: var(--bg-inset);
  box-shadow: inset 0 0 0 1px var(--border-strong);
  font-family: var(--mono);
  font-size: var(--text-12-5);
}

.url-address {
  @apply flex-1 min-w-0 truncate;
}

.url-lock {
  @apply flex items-center flex-none text-text-tertiary;
}

.copy-btn {
  @apply flex items-center justify-center flex-none border-0 bg-transparent text-text-secondary cursor-pointer;
  width: 1.625rem;
  height: 1.625rem;
  margin-right: -0.375rem;
  padding: 0;
  border-radius: 0.375rem;
}

.copy-btn:hover {
  background: var(--bg-hover);
}

.pane-changes {
  @apply flex flex-col min-w-0;
}

.changes-head {
  @apply flex items-center;
  height: 1.875rem;
  gap: 0.625rem;
}

.changes-title {
  @apply text-text-tertiary;
  font-size: var(--text-11-5);
  font-weight: 600;
}

.update {
  @apply flex items-center flex-none cursor-pointer border-0;
  height: 1.875rem;
  padding: 0 0.875rem 0 0.6875rem;
  gap: 0.4375rem;
  border-radius: 0.5rem;
  background: var(--accent);
  color: var(--accent-text);
  font: inherit;
  font-weight: 600;
  white-space: nowrap;
}

.update:hover {
  filter: brightness(1.06);
}

.changes-none {
  @apply flex items-center text-text-secondary;
  height: 2.125rem;
  gap: 0.5rem;
  border-top: 1px solid var(--line2);
  font-size: var(--text-13);
}

.changes-none.loose {
  @apply text-text-tertiary;
  font-size: var(--text-12-5);
}

.chg-row {
  @apply flex items-center;
  min-height: 2.125rem;
  gap: 0.625rem;
  border-top: 1px solid var(--line2);
  font-size: var(--text-13);
}

.chg-sign {
  @apply flex items-center justify-center flex-none;
  width: 1rem;
  height: 1rem;
  border-radius: 0.25rem;
  font-family: var(--mono);
  font-size: var(--text-11);
  font-weight: 700;
  line-height: 1;
}

.chg-sign.add {
  background: var(--green-soft);
  color: var(--green);
}

.chg-sign.edit {
  background: var(--orange-soft);
  color: var(--orange);
}

.chg-sign.gone {
  background: var(--red-soft);
  color: var(--red);
}

.chg-method {
  @apply flex-none;
  width: 3rem;
  font-family: var(--mono);
  font-size: var(--text-10-5);
  font-weight: 700;
}

.chg-name {
  @apply min-w-0 truncate;
  flex: 1 1 5rem;
}

.chg-what {
  @apply min-w-0 truncate text-text-tertiary;
  flex: 0 1 auto;
  font-size: var(--text-12-5);
  overflow-wrap: anywhere;
}

.mark-ok {
  @apply flex-none text-green;
}

.mark-warn {
  @apply flex-none text-orange;
}

.pane-settings {
  @apply min-w-0;
  font-size: var(--text-13);
}

.pane-settings :deep(.publication-field) {
  height: 2.5rem;
  border-top: 1px solid var(--line2);
}

.password-row {
  @apply flex;
  gap: 0.375rem;
  padding: 0 0 0.5rem;
}

.password-note {
  @apply text-red text-12;
}

button:disabled {
  @apply cursor-default opacity-45;
}

.version-row {
  @apply flex items-center;
  min-height: 2.375rem;
  gap: 0.75rem;
  border-top: 1px solid var(--line2);
  font-size: var(--text-12-5);
}

.version-number {
  @apply flex-none font-semibold text-text-secondary;
  width: 1.625rem;
  font-family: var(--mono);
  font-size: var(--text-12);
}

.version-summary {
  @apply flex-1 min-w-0 truncate;
}

.version-note {
  @apply text-text-tertiary;
}

.version-current {
  @apply text-text-tertiary text-12-5;
}

.pane-actions {
  @apply flex flex-none;
  gap: 0.5rem;
}

.more-button {
  @apply flex items-center justify-center flex-none border-0 text-text cursor-pointer;
  width: 1.875rem;
  height: 1.875rem;
  padding: 0;
  border-radius: 0.5rem;
  background: var(--chip);
}

.more-button:hover {
  background: var(--bg-hover);
}

.changes-count {
  @apply text-orange text-11-5;
  margin-left: 0.5rem;
}

:global(.menu.publication-actions-menu) {
  width: 196px;
  min-width: 176px;
  padding: 0.3125rem;
  border-radius: 0.625rem;
}

:global(.publication-actions-menu .menu-item) {
  height: 2rem;
  padding: 0 0.625rem;
  gap: 0.625rem;
  border-radius: 0.4375rem;
}

:global(.publication-actions-menu .menu-item svg) {
  color: var(--text-secondary);
  flex: none;
}

:global(.publication-actions-menu .menu-item.bad),
:global(.publication-actions-menu .menu-item.bad svg) {
  color: var(--red);
}

:global(.publication-actions-menu .menu-item.bad:hover),
:global(.publication-actions-menu .menu-item.bad[data-highlighted]) {
  background: var(--red-soft);
}
</style>
