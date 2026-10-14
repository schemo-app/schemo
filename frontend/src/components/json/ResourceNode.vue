<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Icon from '../ui/Icon.vue'
import { useMessages } from '../../i18n'
import { prettyJson } from '../../lib/json'
import { resourceKey, resourceLabel, type Resource, type Relationship } from '../../lib/jsonapi'
import RelationshipLink from './RelationshipLink.vue'
import { useCopyFeedback } from '../../composables/useCopyFeedback'

const { path, highlighted, resource } = defineProps<{
  resource: Resource
  resourceIndex: Map<string, Resource>
  highlighted?: boolean
  path: string
}>()

const emit = defineEmits<{
  (e: 'jump', key: string): void
  (e: 'fetch', url: string): void
  (e: 'inspect', path: string): void
}>()

const open = ref(false)

const { t } = useMessages()

// `<Transition>` is only mounted once a node has actually been opened — a resource list can run
// into the hundreds, nearly all collapsed, and giving every one of them its own Transition
// instance up front was measurably slower to mount (~70ms vs ~30ms at 500 resources) for an
// animation almost none of them will ever play.
const everOpened = ref(false)

function toggleOpen() {
  open.value = !open.value
  if (open.value) everOpened.value = true
  emit('inspect', path)
}

function relPath(name: string): string {
  return path + '.relationships.' + name
}

// A highlight from the map must expand the node, not just show its header.
watch(
  () => highlighted,
  (h) => {
    if (h) {
      open.value = true
      everOpened.value = true
    }
  }
)

const label = computed(() => resourceLabel(resource))

const attributes = computed<[string, unknown][]>(() =>
  Object.entries(resource.attributes ?? {})
)

const relationships = computed<[string, Relationship][]>(() =>
  Object.entries(resource.relationships ?? {})
)

function valueClass(v: unknown): string {
  if (v === null) return 'null'
  if (typeof v === 'string') return 'str'
  if (typeof v === 'number') return 'num'
  if (typeof v === 'boolean') return 'bool'
  return ''
}

function formatValue(v: unknown): string {
  if (v === null) return 'null'
  if (typeof v === 'string') return JSON.stringify(v)
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

const rid = computed(() => 'res-' + resourceKey(resource.type, resource.id))

// The key and the value are marked apart, each by the attribute name it was copied under.
const { copied: copiedKey, copy: copyKeyText } = useCopyFeedback(700)
const { copied: copiedVal, copy: copyValText } = useCopyFeedback(700)

function hasSelection(): boolean {
  return (window.getSelection()?.toString().length ?? 0) > 0
}

async function copyKey(k: string) {
  if (hasSelection()) return
  await copyKeyText(k, k)
}

async function copyVal(k: string, v: unknown) {
  if (hasSelection()) return
  const text = v === null ? 'null' : typeof v === 'object' ? prettyJson(v) : String(v)
  await copyValText(text, k)
}
</script>

<template>
  <div class="ja-resource" :id="rid" :class="{ highlight: highlighted }">
    <button class="ja-resource-head" @click="toggleOpen">
      <span class="ja-caret" :class="{ open }">
        <Icon name="ChevronRight" :size="10" />
      </span>
      <span class="ja-type-badge">{{ resource.type }}</span>
      <span class="ja-id">{{ resource.id }}</span>
      <span class="ja-label">{{ label }}</span>
    </button>

    <Transition v-if="everOpened" name="ja-body">
      <div v-if="open" class="ja-body">
        <div class="ja-body-inner">
          <div v-if="attributes.length" class="ja-section-title" style="padding-left: 0">attributes</div>
          <div v-for="[k, v] in attributes" :key="k" class="ja-attr">
            <span class="ja-attr-key" :title="t('json.copyKey')" :class="{ copied: copiedKey === k }" @click.stop="copyKey(k)">{{ k }}</span>
            <span
              class="ja-attr-val"
              :title="t('json.copyValue')"
              :class="[valueClass(v), { copied: copiedVal === k }]"
              @click.stop="copyVal(k, v)"
              >{{ formatValue(v) }}</span
            >
          </div>

          <template v-if="relationships.length">
            <div class="ja-section-title" style="padding-left: 0">relationships</div>
            <div v-for="[name, rel] in relationships" :key="name" class="ja-rel">
              <span class="ja-rel-name">{{ name }}</span>
              <RelationshipLink :rel :resource-index @jump="(k) => emit('jump', k)" @fetch="(u) => emit('fetch', u)" @inspect="emit('inspect', relPath(name))" />
            </div>
          </template>
        </div>
      </div>
    </Transition>
  </div>
</template>
