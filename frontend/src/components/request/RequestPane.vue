<script setup lang="ts">
import { computed, ref, useTemplateRef, watch } from 'vue'
import { REQUEST } from '../../lib/layout'
import { RowKind } from '../../../bindings/schemo/internal/domain'
import TabText from '../ui/TabText.vue'
import RowsPane from './RowsPane.vue'
import AuthPane from './AuthPane.vue'
import BodyPane from './BodyPane.vue'
import ScriptsPane from './ScriptsPane.vue'
import DescriptionEditor from '../collections/DescriptionEditor.vue'
import ExamplesPane from './ExamplesPane.vue'
import { findNode } from '../../lib/collectionTree'
import { useCollectionsStore } from '../../stores/collections'
import { useResizeDrag } from '../../composables/useResizeDrag'
import { useSlidingPill } from '../../composables/useSlidingPill'
import { useSetting, useSettings } from '../../composables/useSettings'
import type { ChipName, RequestSource } from '../../lib/requestSource'
import { isTabDraft } from '../../lib/strip'
import { t } from '../../i18n'
import { rem, unitScale } from '../../lib/units'

// Description and examples fill the workspace; other sections share it with the response.
const { source, expanded = false } = defineProps<{ source: RequestSource; expanded?: boolean }>()

const { setLayout } = useSettings()
const collections = useCollectionsStore()
const requests = useSetting('requests')
const rowsTextMode = ref(false)

const CHIPS: ChipName[] = ['params', 'headers', 'body', 'auth', 'scripts', 'docs', 'examples']

// A description and the saved examples both belong to a row of the tree, so a draft with nothing
// above it has neither. The examples pane carries the last answer as well, which is why its chip is
// there for every saved request and not only for ones an import brought examples to.
const chips = computed(() => CHIPS.filter(chip => {
  if (chip === 'body') return !['GET', 'HEAD', 'TRACE'].includes(source.method.toUpperCase())
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

function choose(chip: ChipName) {
  source.setOpenChip(open.value === chip ? null : chip)
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

// Where the scripts' code sits in the order, in one line. Above a tab there is no collection, so
// the sentence a card says would be untrue there.
const scriptsNote = computed(() =>
  isTabDraft(source.scriptsLevel) ? '' : t('request.scriptsNoteCollection')
)

// The rule travels to the section that was opened rather than appearing under it, and fades where
// it stands when the pane is folded.
const tabsEl = useTemplateRef<HTMLElement>('tabsEl')
const { style: markStyle, ready: markReady } = useSlidingPill(tabsEl, '.req-tab.active', () => open.value)

// The cap is rounded down to a whole pixel in the stylesheet: one that lands on a fraction puts
// the answer under it — its row, its glyphs — on a fraction too, and they blur.

// The chrome the window draws above and below the pane — the header with its tab strip, and the
// status bar. A share is taken of what is left of the window, which is the page's own column.
const CHROME_HEIGHT = 72

// Zero is not a height: it is the pane as tall as what it holds, where a double click on the edge
// puts it back. Go clamps what is stored; the pair here is what the drag stops at, and it is Go's
// own pair.
//
// The smaller of the two limits is the share the stylesheet caps the pane at: a height dragged on a
// tall window must stop there, or the answer under it disappears on a short one. The window is
// measured in screen pixels and the drag counts in layout units, hence the scale.
function tallestHeight(): number {
  return Math.min(REQUEST.max, Math.round(((window.innerHeight - CHROME_HEIGHT) / unitScale()) * 0.42))
}

const stored = useSetting('requestHeight')
const height = ref(stored.value)

const paneEl = useTemplateRef<HTMLElement>('paneEl')
const toolbar = useTemplateRef<HTMLElement>('toolbar')

// The pane is as tall as what it holds until it is dragged, and the drag counts from the height it
// has on screen rather than from the ref, which is zero until somebody has dragged it.
const { startDrag, dragging } = useResizeDrag(height, {
  axis: 'y',
  side: 'top',
  min: REQUEST.min,
  max: tallestHeight,
  from: () => {
    const shown = paneEl.value?.offsetHeight
    return shown ? shown / unitScale() : height.value
  },
  onEnd: () => setLayout({ requestHeight: Math.round(height.value) }),
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
  <div ref="tabsEl" class="req-tabs" :class="{ open: !!open }">
    <span class="slide-mark tab-mark" :class="{ ready: markReady }" :style="markStyle"></span>
    <div class="req-tab-list">
      <button
        v-for="chip in chips"
        :key="chip"
        type="button"
        class="req-tab"
        :class="{ active: open === chip }"
        @click="choose(chip)"
      >
        <TabText>
          {{ t(`request.chips.${chip}`) }}
          <span v-if="count(chip)" class="req-tab-count">{{ count(chip) }}</span>
          <span v-if="chip === 'docs' && described" class="req-tab-dot"></span>
        </TabText>
      </button>
    </div>
    <div ref="toolbar" class="req-toolbar"></div>
  </div>

  <div v-if="open" class="req-pane-wrap" :class="{ expanded }">
    <div
      ref="paneEl"
      class="req-pane"
      :style="!expanded && height > 0 ? { height: rem(height) } : undefined"
    >
      <RowsPane v-if="open === 'params'" v-model:text-mode="rowsTextMode" :source :toolbar kind="params" @open-auth="choose('auth')" />
      <RowsPane v-else-if="open === 'headers'" v-model:text-mode="rowsTextMode" :source :toolbar kind="headers" @open-auth="choose('auth')" />
      <AuthPane v-else-if="open === 'auth'" :source />
      <BodyPane v-else-if="open === 'body'" :source />
      <ExamplesPane v-else-if="open === 'examples'" :source />
      <DescriptionEditor
        v-else-if="open === 'docs' && source.nodeId"
        :key="source.nodeId"
        :id="source.nodeId"
        :toolbar
        empty-key="request.docs.emptyRequest"
      />
      <div v-else class="scripts-pane">
        <span class="scripts-title">{{ t('request.pane.scriptsTitle') }}</span>
        <ScriptsPane :source :note="scriptsNote" compact />
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

.req-pane-wrap.expanded {
  @apply flex flex-col flex-1 min-h-0;
}

.expanded .req-pane {
  @apply flex-1 min-h-0;
  max-height: none;
}

.req-pane-wrap {
  @apply relative;
}

.req-pane {
  @apply relative flex flex-col bg-bg overflow-y-auto;
  max-height: round(down, calc((100vh - 4.5rem) * 0.42), 1px);
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

.scripts-title {
  @apply text-14 font-semibold;
}
</style>
