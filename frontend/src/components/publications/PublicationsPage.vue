<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Browser } from '@wailsio/runtime'
import Icon from '../ui/Icon.vue'
import { hostOf, usePublicationsStore } from '../../stores/publications'
import { useCollectionsStore } from '../../stores/collections'
import { useTabsStore } from '../../stores/tabs'
import { useToast } from '../../composables/useToast'
import { copyToClipboard } from '../../lib/clipboard'
import { findCollection } from '../../lib/collectionTree'
import { changeNotes } from '../../lib/publications'
import { methodInkClass } from '../../lib/format'
import { formatAgo, useMessages } from '../../i18n'

const { t } = useMessages()
const store = usePublicationsStore()
const collections = useCollectionsStore()
const tabs = useTabsStore()
const toast = useToast()

const selected = ref('')

onMounted(async () => {
  await store.load()
  await store.refresh()
  await store.markAll(store.pages.map((page) => page.collectionId))
})

const live = computed(() => store.pages.filter((page) => page.status === 'active'))

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
const behind = computed(() => mark.value?.behind === true)
const notes = computed(() => (behind.value ? changeNotes(mark.value?.changes ?? []) : []))

const name = computed(() => {
  const page = current.value
  if (!page) return ''
  return findCollection(collections.tree, page.collectionId)?.name || page.slug || ''
})

const meta = computed(() => {
  const page = current.value
  if (!page) return ''
  return [page.environmentName ?? '', page.updatedAt ? formatAgo(page.updatedAt) : '']
    .filter(Boolean)
    .join(' · ')
})

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
  const page = current.value
  if (page && !store.busy) await store.updatePage(page.collectionId)
}
</script>

<template>
  <div class="pubs-page">
    <header class="pubs-head">
      <span class="pubs-title">{{ t('publications.title') }}</span>
      <span class="pubs-count">{{ head }}</span>
    </header>

    <div v-if="store.loading && live.length === 0" class="pubs-empty">
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
            <span class="rail-name cap-line">
              {{ findCollection(collections.tree, row.page.collectionId)?.name || row.page.slug }}
            </span>
            <span class="rail-meta cap-line">
              {{ accessOf(row.page.visibility) }} · {{ revisionOf(row.page.revision) }}
            </span>
          </span>
          <span v-if="row.missing" class="rail-badge" :title="t('publications.changesBadge')">
            {{ row.missing }}
          </span>
        </button>
      </div>

      <div v-if="current" class="pubs-pane">
        <div class="pane-inner">
          <div class="pane-top">
            <div class="pane-head">
              <div class="pane-title-row">
                <span class="pane-name cap-line">{{ name }}</span>
                <span class="pane-gap"></span>
                <button type="button" class="link" @click="openPage">
                  {{ t('publications.openPage') }}
                  <Icon name="ArrowUpRight" :size="11" :stroke-width="2.1" />
                </button>
              </div>
              <span class="pane-meta cap-line">{{ meta }}</span>
            </div>

            <div class="pane-url">
              <div class="url-text">
                <span class="url-address">{{ current.url }}</span>
                <span
                  v-if="current.visibility === 'password'"
                  class="url-lock"
                  :title="t('publications.visPassword')"
                >
                  <Icon name="Lock" :size="12" :stroke-width="1.9" />
                </span>
              </div>
              <button type="button" class="copy-btn" @click="copy">
                {{ t('publications.copy') }}
              </button>
            </div>
          </div>

          <div class="pane-changes">
            <div class="changes-head">
              <span class="changes-title">{{ t('publications.changes') }}</span>
              <span class="pane-gap"></span>
              <button
                v-if="behind"
                type="button"
                class="update"
                :disabled="store.busy"
                @click="update"
              >
                {{
                  store.busy
                    ? t('publications.updating')
                    : t('publications.updateTo', { n: (current.revision ?? 0) + 1 })
                }}
              </button>
            </div>
            <div v-if="!behind" class="changes-none">
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
              <span class="chg-what">{{ row.what }}</span>
            </div>
          </div>

          <div class="pane-foot">
            <button type="button" class="link" @click="openCollection">
              {{ t('publications.openCollection') }}
            </button>
            <button
              v-if="current.canManage"
              type="button"
              class="link bad"
              @click="store.askUnpublish(current.collectionId)"
            >
              {{ t('publications.unpublish') }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.pubs-page {
  @apply flex-1 min-h-0 flex flex-col;
}

.pubs-head {
  @apply flex items-center flex-none;
  height: 4rem;
  padding: 0 1.5rem;
  gap: 0.875rem;
  border-bottom: 1px solid var(--border);
  box-sizing: content-box;
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
  padding: 0.85rem 0.65rem;
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
  gap: 0.6rem;
}

.rail-name {
  @apply font-medium;
}

.rail-meta {
  @apply text-text-tertiary;
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
  min-width: 30rem;
  gap: 1.625rem;
}

.pane-top {
  @apply flex flex-col;
  gap: 1rem;
}

.pane-head {
  @apply flex flex-col;
  gap: 0.6rem;
}

.pane-title-row {
  @apply flex items-center;
  gap: 0.625rem;
}

.pane-name {
  @apply min-w-0;
  font-size: var(--text-20);
  font-weight: 600;
  letter-spacing: -0.015em;
}

.pane-gap {
  @apply flex-1;
}

.pane-meta {
  @apply text-text-tertiary;
  font-size: var(--text-12-5);
}

.link {
  @apply flex items-center flex-none cursor-pointer border-0 whitespace-nowrap bg-transparent text-accent;
  gap: 0.3125rem;
  font-family: inherit;
  font-size: var(--text-12-5);
}

.link.bad {
  color: var(--red);
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
  @apply min-w-0 truncate;
}

.url-lock {
  @apply flex items-center flex-none text-text-tertiary;
}

.copy-btn {
  @apply flex-none cursor-pointer border-0 font-medium;
  height: 2.125rem;
  padding: 0 0.875rem;
  border-radius: 0.5rem;
  background: var(--accent);
  color: var(--accent-text);
  font-family: inherit;
  font-size: var(--text-13);
}

.copy-btn:hover {
  filter: brightness(1.06);
}

.pane-changes {
  @apply flex flex-col;
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
  @apply flex-none cursor-pointer border-0 font-medium;
  height: 1.625rem;
  padding: 0 0.6875rem;
  border-radius: 0.4375rem;
  background: var(--accent);
  color: var(--accent-text);
  font-family: inherit;
  font-size: var(--text-12-5);
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
  height: 2.125rem;
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
}

.mark-ok {
  @apply flex-none text-green;
}

.mark-warn {
  @apply flex-none text-orange;
}

.pane-foot {
  @apply flex;
  gap: 1.125rem;
  padding-top: 0.125rem;
}
</style>
