<script setup lang="ts">
import { computed } from 'vue'
import Icon from '../ui/Icon.vue'
import { documentVersion, linkHref, type JsonApiDocument } from '../../lib/jsonapi'
import { useMessages } from '../../i18n'

// What a JSON:API answer offers above its tree: the four steps through the collection, the version
// the document declares, and which page this is.
const { doc, url } = defineProps<{ doc: JsonApiDocument; url: string }>()
defineEmits<{ follow: [url: string] }>()

const { t } = useMessages()

const pagination = computed(() => {
  const l = doc.links ?? {}
  return { first: linkHref(l.first), prev: linkHref(l.prev), next: linkHref(l.next), last: linkHref(l.last) }
})
const hasPagination = computed(() => Boolean(pagination.value.prev || pagination.value.next))

// Read the way the status bar and the inspector read it: one question about one document, one answer.
const version = computed(() => documentVersion(doc))

// Which page this is comes from the answer's own address, and how many there are only from `last`: an
// answer without it gets half a sentence rather than a total nobody stated.
function pageNumber(raw: string | undefined): number {
  if (!raw) return 0
  try {
    const value = Number(new URL(raw, 'https://x').searchParams.get('page[number]'))
    return Number.isFinite(value) && value > 0 ? value : 0
  } catch {
    return 0
  }
}

const pageLabel = computed(() => {
  const page = pageNumber(url)
  if (!page) return ''
  const total = pageNumber(pagination.value.last)
  return total ? t('response.pageOf', { page, total }) : t('response.page', { page })
})
</script>

<template>
  <!-- The strip stands only when it holds something: an answer without steps, a version or a page
       number would draw a bare band with a line under it above its tree. -->
  <div v-if="hasPagination || version || pageLabel" class="toolbar">
    <!-- The four steps as one control in a groove of its own: one thing the reader moves through,
         not four things to press. -->
    <div v-if="hasPagination" class="pager">
      <button type="button" class="pager-btn pager-edge" :disabled="!pagination.first || !pagination.prev" :title="t('response.first')" @click="$emit('follow', pagination.first)"><Icon name="ChevronsLeft" :size="16" :stroke-width="1.9" /></button>
      <button type="button" class="pager-btn" :disabled="!pagination.prev" :title="t('response.previous')" @click="$emit('follow', pagination.prev)"><Icon name="ChevronLeft" :size="16" :stroke-width="1.9" /></button>
      <button type="button" class="pager-btn" :disabled="!pagination.next" :title="t('response.next')" @click="$emit('follow', pagination.next)"><Icon name="ChevronRight" :size="16" :stroke-width="1.9" /></button>
      <button type="button" class="pager-btn pager-edge" :disabled="!pagination.last || !pagination.next" :title="t('response.last')" @click="$emit('follow', pagination.last)"><Icon name="ChevronsRight" :size="16" :stroke-width="1.9" /></button>
    </div>
    <span v-if="version" class="jsonapi-tag mono">jsonapi <span class="jsonapi-ver">v{{ version }}</span></span>
    <span class="head-spacer"></span>
    <span v-if="pageLabel" class="page-label">{{ pageLabel }}</span>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.pager {
  @apply flex gap-0.5 flex-none rounded-lg p-0.5 bg-bg-hover;
}

.pager-btn {
  @apply inline-flex items-center justify-center w-8 h-7 rounded-md border-0 bg-transparent
         cursor-pointer text-text-secondary;
  padding: 0;
  --wails-draggable: no-drag;
}

.pager-btn.pager-edge {
  @apply text-text-tertiary;
}

.pager-btn:hover:not(:disabled) {
  @apply bg-bg-active text-text;
}

.pager-btn:disabled {
  @apply opacity-50 cursor-default;
}

.pager-btn:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}

.jsonapi-tag {
  @apply flex-none ml-2 text-13 text-text-tertiary;
}

.jsonapi-ver {
  @apply text-text-secondary;
}

.page-label {
  @apply flex-none text-13 text-text-tertiary;
}
</style>
