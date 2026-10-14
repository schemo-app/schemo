<script setup lang="ts">
import { ref, watch } from 'vue'
import Block from '../ui/Block.vue'
import type { NodeView, Suggestion } from '../../../bindings/schemo/internal/domain'
import { isContainer, type Inspection } from '../../composables/response/useInspection'
import { useMessages } from '../../i18n'
import { trailOf } from '../../lib/collectionTree'
import { useCollectionsStore } from '../../stores/collections'
import { useTabsStore } from '../../stores/tabs'

// The requests of the collection a value looks like it belongs in: an author's identifier is what
// the request that reads an author takes.
const { inspection, node } = defineProps<{ inspection: Inspection; node: NodeView }>()

const { t } = useMessages()
const collections = useCollectionsStore()
const found = ref<Suggestion[]>([])

// Where a suggestion sits is read off the tree the window already holds — the same walk the palette
// joins its rows with.
function trail(one: Suggestion): string {
  const steps = trailOf(collections.tree, one.nodeId)?.ancestors ?? []
  return steps.map((step) => step.name).join(' › ')
}

watch(
  () => node.path,
  async () => {
    found.value = []
    if (isContainer(node)) return
    const suggested = await inspection.suggestFor(node)
    if (inspection.node.value === node) found.value = suggested
  },
  { immediate: true }
)

// Opens the request as its card with the address filled in; the card is edited, not saved.
async function open(one: Suggestion) {
  collections.reveal(one.nodeId)
  if (!(await collections.select(one.nodeId))) return
  const pane = useTabsStore().pane
  pane?.setUrl(one.filled)
  await pane?.flush()
}
</script>

<template>
  <Block v-if="found.length" :title="t('inspector.collection')">
    <button
      v-for="one in found"
      :key="one.nodeId"
      type="button"
      class="request"
      :title="one.filled"
      @click="open(one)"
    >
      <span class="method" :class="{ read: one.method === 'GET' }">{{ one.method }}</span>
      <span class="name">{{ one.name }}</span>
      <span v-if="trail(one)" class="trail">{{ trail(one) }}</span>
    </button>
  </Block>
</template>

<style scoped>
@reference "../../style.css";

.request {
  @apply flex w-full cursor-pointer items-baseline gap-2 rounded-[0.375rem] border-0 bg-transparent px-2 text-left text-13 text-text;
  min-height: 1.875rem;
  --wails-draggable: no-drag;
}

.request:hover {
  background: var(--bg-hover);
}

.method {
  @apply flex-none text-11 font-semibold text-accent;
  font-family: var(--mono);
  min-width: 2.125rem;
}

.method.read {
  color: var(--green-text);
}

.name {
  @apply min-w-0 flex-1 truncate;
}

.trail {
  @apply flex-none truncate text-12 text-text-tertiary;
  max-width: 40%;
}
</style>
