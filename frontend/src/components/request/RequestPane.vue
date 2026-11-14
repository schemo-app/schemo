<script setup lang="ts">
import { computed, ref, useTemplateRef, watch } from 'vue'
import { REQUEST } from '../../lib/layout'
import { RowKind } from '../../../bindings/schemo/internal/domain'
import TabText from '../ui/TabText.vue'
import RowsPane from './RowsPane.vue'
import AuthPane from './AuthPane.vue'
import BodyPane from './BodyPane.vue'
import ProtocolSchema from './ProtocolSchema.vue'
import ScriptsPane from './ScriptsPane.vue'
import WebSocketComposer from './WebSocketComposer.vue'
import DescriptionEditor from '../collections/DescriptionEditor.vue'
import ExamplesPane from './ExamplesPane.vue'
import { findNode } from '../../lib/collectionTree'
import { useCollectionsStore } from '../../stores/collections'
import { useResizeDrag } from '../../composables/useResizeDrag'
import { useSlidingPill } from '../../composables/useSlidingPill'
import { useSetting, useSettings } from '../../composables/useSettings'
import type { ChipName, RequestSource } from '../../lib/requestSource'
import { t } from '../../i18n'
import { rem, unitScale } from '../../lib/units'
import { protocolOf } from '../../lib/protocol'

// Description and examples fill the workspace; other sections share it with the response.
const { source, expanded = false, methodSlot = null } = defineProps<{
  source: RequestSource
  expanded?: boolean
  methodSlot?: HTMLElement | null
}>()

const { setLayout } = useSettings()
const collections = useCollectionsStore()
const requests = useSetting('requests')
const rowsTextMode = ref(false)

// A draft has no tree row, so no description or examples.
const protocol = computed(() => protocolOf(source.protocol))
const chips = computed(() => protocol.value.chips.filter(chip => {
  if (!source.protocol && chip === 'body') return !['GET', 'HEAD', 'TRACE'].includes(source.method.toUpperCase())
  if (chip === 'docs' || chip === 'examples') return !!source.nodeId
  return true
}))

const described = computed(() =>
  source.nodeId ? (findNode(collections.tree, source.nodeId)?.description ?? '').trim() !== '' : false
)

const open = computed(() => {
  const chip = source.openChip
  return chip && chips.value.includes(chip) ? chip : null
})

function chipLabel(chip: ChipName): string {
  return t(protocol.value.chipLabels?.[chip] ?? `request.chips.${chip}`)
}

function choose(chip: ChipName) {
  source.setOpenChip(!source.protocol && open.value === chip ? null : chip)
}

// A count is how many rows go out; a section that is not a list says only whether it holds anything.
// "None" and "Inherit" carry no credentials, so the authorization is empty in those two.
function count(chip: ChipName): number {
  if (chip === 'params') return source.enabledParamsCount + source.pathParams.length
  if (chip === 'headers') {
    return source.enabledHeadersCount + source.projected.filter(row => row.target === RowKind.RowHeaders).length +
      (requests.value.showAuto ? source.auto.length : 0)
  }
  if (chip === 'examples') return source.responses.length
  return 0
}

// The rule travels to the section that was opened rather than appearing under it, and fades where
// it stands when the pane is folded.
const tabsEl = useTemplateRef<HTMLElement>('tabsEl')
const { style: markStyle, ready: markReady } = useSlidingPill(tabsEl, '.req-tab.active', () => open.value)

const stored = useSetting('requestHeight')
const height = ref(stored.value)
const defaultHeight = computed(() => !expanded && !source.protocol && height.value === 0)
const hasPane = computed(() => open.value !== null)
const hasSchema = computed(() => chips.value.includes('schema'))
const protocolEditor = computed(() => hasSchema.value && (open.value === 'body' || open.value === 'schema'))

const paneEl = useTemplateRef<HTMLElement>('paneEl')
const toolbar = useTemplateRef<HTMLElement>('toolbar')

// Start at the rendered height, which can be limited by the available workspace.
const { startDrag, dragging } = useResizeDrag(height, {
  axis: 'y',
  side: 'top',
  min: REQUEST.min,
  max: REQUEST.max,
  from: () => {
    const shown = paneEl.value?.offsetHeight
    return shown ? shown / unitScale() : height.value
  },
  onEnd: () => {
    const shown = paneEl.value?.offsetHeight
    if (height.value && shown && height.value !== stored.value) {
      height.value = Math.min(REQUEST.max, Math.max(REQUEST.min, Math.round(shown / unitScale())))
      setLayout({ requestHeight: height.value })
    }
  },
})

watch(stored, (now) => {
  if (!dragging.value && now !== height.value) height.value = now
})

function resetPaneHeight() {
  height.value = 0
  setLayout({ requestHeight: 0 })
}
</script>

<template>
  <div ref="tabsEl" class="req-tabs" :class="{ open: !!open, 'req-tabs-protocol': source.protocol }">
    <span class="slide-mark tab-mark" :class="{ ready: markReady }" :style="markStyle"></span>
    <div class="req-tab-list">
      <button
        v-for="chip in chips"
        :key="chip"
        type="button"
        class="req-tab"
        :class="{ active: open === chip }"
        :aria-pressed="open === chip"
        @click="choose(chip)"
      >
        <TabText>
          {{ chipLabel(chip) }}
          <span v-if="count(chip)" class="req-tab-count">{{ count(chip) }}</span>
          <span v-if="chip === 'docs' && described" class="req-tab-dot"></span>
        </TabText>
      </button>
    </div>
    <div ref="toolbar" class="req-toolbar"></div>
  </div>

  <div v-if="hasPane" class="req-pane-wrap" :class="{ 'default-height': defaultHeight }">
    <div
      ref="paneEl"
      class="req-pane"
      :style="!expanded && height > 0 ? { height: rem(height) } : undefined"
    >
      <ProtocolSchema v-if="hasSchema" :source :section="open" :method-slot="methodSlot" />
      <RowsPane v-if="open === 'params'" v-model:text-mode="rowsTextMode" :source :toolbar kind="params" @open-auth="choose('auth')" />
      <RowsPane v-else-if="open === 'headers'" v-model:text-mode="rowsTextMode" :source :toolbar kind="headers" @open-auth="choose('auth')" />
      <AuthPane v-else-if="open === 'auth'" :source />
      <BodyPane v-else-if="open === 'body' && !protocolEditor" :source />
      <ExamplesPane v-else-if="open === 'examples'" :source />
      <WebSocketComposer v-else-if="open === 'messages'" :source />
      <DescriptionEditor
        v-else-if="open === 'docs' && source.nodeId"
        :key="source.nodeId"
        :id="source.nodeId"
        :toolbar
        empty-key="request.docs.emptyRequest"
      />
      <div v-else-if="open === 'scripts'" class="scripts-pane">
        <ScriptsPane :source compact />
      </div>
    </div>

    <!-- The edge the pane is dragged by: it lies over the line between the pane and the answer, so
         the line itself is what is taken hold of. -->
    <div v-if="!expanded" class="req-grip" :title="t('request.pane.resize')" @mousedown.prevent="startDrag" @dblclick="resetPaneHeight"></div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.req-tabs {
  @apply relative flex flex-none items-end bg-bg;
  height: 2.5rem;
  gap: 1.25rem;
  padding: 0 1.25rem;
  border-bottom: 1px solid var(--border);
}

.req-tab-list {
  @apply flex items-end min-w-0 h-full overflow-x-auto;
  gap: 1.25rem;
  scrollbar-width: none;
}

.req-tabs-protocol .req-tab-list {
  gap: 0.875rem;
}

.req-toolbar {
  @apply flex flex-none items-center;
  margin-left: auto;
  margin-bottom: 0.4375rem;
}

.req-tab {
  @apply grid items-center justify-items-start cursor-pointer bg-transparent border-0 whitespace-nowrap;
  padding: 0 0 0.625rem;
  color: var(--text-secondary);
  font-family: inherit;
  font-size: var(--text-13);
  font-weight: 400;
  transition: color 0.15s ease;
  --wails-draggable: no-drag;
}

.req-tab:hover {
  color: var(--text);
}

.req-tab.active {
  @apply text-text font-medium;
}

.req-tab-count {
  @apply text-text-tertiary font-normal;
  font-variant-numeric: tabular-nums;
}

.req-tab-dot {
  @apply rounded-full bg-accent;
  width: 0.3125rem;
  height: 0.3125rem;
}

.req-tabs,
.req-pane {
  line-height: normal;
}

.req-pane-wrap {
  @apply relative flex flex-col flex-auto min-h-0 min-w-0;
}

.req-pane {
  @apply relative flex flex-col flex-auto min-h-0 min-w-0 max-h-full bg-bg overflow-auto;
}

.req-grip {
  @apply absolute left-0 right-0;
  bottom: -0.3125rem;
  height: 0.5rem;
  cursor: row-resize;
  z-index: 40;
}

.req-grip:hover {
  background: linear-gradient(transparent 0.1875rem, var(--accent) 0.1875rem, var(--accent) 0.3125rem, transparent 0.3125rem);
}

.scripts-pane {
  @apply flex flex-col gap-3;
  padding: 0.875rem 1.25rem 1rem;
}

</style>
