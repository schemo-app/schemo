<script setup lang="ts">
import { computed } from 'vue'
import { PANEL } from '../../lib/layout'
import Icon from '../ui/Icon.vue'
import IconButton from '../ui/IconButton.vue'
import Block from '../ui/Block.vue'
import ChildrenList from './ChildrenList.vue'
import CollectionMatches from './CollectionMatches.vue'
import CopyRow from '../ui/CopyRow.vue'
import FaultCard from './FaultCard.vue'
import NodeActions from './NodeActions.vue'
import ReadingCard from './ReadingCard.vue'
import { NodeKind } from '../../../bindings/schemo/internal/domain'
import { isContainer, type Inspection } from '../../composables/useInspection'
import { useCopyFeedback } from '../../composables/useCopyFeedback'
import { useResizeDrag } from '../../composables/useResizeDrag'
import { formatBytes, textOf, t } from '../../i18n'
import { rem } from '../../lib/units'
import { inspectorState, setInspectorState, type PaneSource } from '../../stores/content'

const {
  source = 'request',
  inspection,
  pane = null,
  canFollow = false,
} = defineProps<{
  inspection: Inspection
  source?: 'request' | 'browser' | 'collection'
  pane?: PaneSource | null
  canFollow?: boolean
}>()

const emit = defineEmits<{ close: []; fetch: [url: string]; pick: [path: string] }>()

const host = source === 'browser' ? null : pane

const { copied, copy } = useCopyFeedback()

const width = computed({
  get: () => inspectorState(pane).width,
  set: (v: number) => setInspectorState(pane, { width: v }),
})
const { startDrag: startResize } = useResizeDrag(width, {
  axis: 'x',
  min: PANEL.min,
  max: PANEL.max,
  side: 'right',
})

const node = computed(() => inspection.node.value)

const title = computed(() => {
  const view = node.value
  if (!view) return ''
  return view.name || (view.parent ? t('inspector.item') : t('inspector.root'))
})

const COUNTED: Partial<Record<NodeKind, string>> = {
  [NodeKind.NodeObject]: 'inspector.summary.object',
  [NodeKind.NodeArray]: 'inspector.summary.array',
  [NodeKind.NodeElement]: 'inspector.summary.element',
  [NodeKind.NodeString]: 'inspector.summary.string',
}

// What the node holds, in the unit its kind counts in: "12 keys · 3.4 KB".
const summary = computed(() => {
  const view = node.value
  if (!view) return ''
  const key = COUNTED[view.kind]
  return [key ? textOf(key, view.count) : '', formatBytes(view.bytes)].filter(Boolean).join(' · ')
})

const emptyString = computed(() => node.value?.kind === NodeKind.NodeString && node.value.count === 0)

async function copyValue() {
  const text = node.value && (await inspection.valueOf(node.value))
  if (text != null) await copy(text)
}
</script>

<template>
  <div class="inspector" :style="{ width: rem(width) }">
    <div class="inspector-resize" @mousedown.prevent="startResize"></div>

    <div class="inspector-head">
      <span class="inspector-title">{{ t('inspector.title') }}</span>
      <IconButton v-if="node?.parent" :hint="t('inspector.parent')" @click="emit('pick', node.parent)">
        <Icon name="ChevronUp" :size="14" :stroke-width="2" />
      </IconButton>
      <IconButton :hint="t('common.close')" @click="emit('close')">
        <Icon name="X" :size="14" :stroke-width="2.2" />
      </IconButton>
    </div>

    <div class="inspector-body">
      <p v-if="inspection.readable.value === false" class="note">{{ t('inspector.notDocument') }}</p>
      <p v-else-if="!node" class="note">{{ t('inspector.empty') }}</p>

      <template v-else>
        <section class="who">
          <div class="who-line">
            <span class="kind">{{ textOf(`inspector.kinds.${node.kind}`) }}</span>
            <span class="name" :title="title">{{ title }}</span>
            <span class="summary">{{ summary }}</span>
          </div>
          <div v-if="(node.crumbs?.length ?? 0) > 1" class="crumbs">
            <template v-for="(crumb, i) in node.crumbs" :key="crumb.path">
              <span v-if="i > 0" class="crumb-sep">›</span>
              <button
                type="button"
                class="crumb"
                :class="{ current: i === node.crumbs!.length - 1 }"
                @click="emit('pick', crumb.path)"
              >
                {{ crumb.name || t('inspector.root') }}
              </button>
            </template>
          </div>
        </section>

        <Block
          v-if="!isContainer(node) || (node.children?.length ?? 0) > 0"
          :title="isContainer(node) ? t('inspector.children') : t('inspector.value')"
        >
          <template #aside>
            <IconButton
              variant="bare"
              size="sm"
              :hint="copied ? t('common.copied') : t('inspector.copyValue')"
              @click="copyValue"
            >
              <Icon :name="copied ? 'Check' : 'Copy'" :size="12" :stroke-width="1.8" />
            </IconButton>
          </template>
          <pre v-if="!isContainer(node)" class="value" :class="{ quiet: emptyString }">{{
            emptyString ? t('inspector.emptyString') : node.preview
          }}</pre>
          <template v-else>
            <ChildrenList :children="node.children!" @pick="(path) => emit('pick', path)" />
            <p v-if="node.children!.length < node.count" class="more">
              {{ t('inspector.childrenMore', { n: node.count - node.children!.length }) }}
            </p>
          </template>
        </Block>

        <ReadingCard
          v-for="reading in node.readings ?? []"
          :key="reading.kind"
          :reading
          :value="node.preview"
          :can-follow="canFollow"
          @fetch="(url) => emit('fetch', url)"
        />

        <CollectionMatches :inspection :node />
        <FaultCard v-if="host" :node :diagnosis="inspection.diagnosis.value" :host />

        <Block :title="t('inspector.address')">
          <CopyRow
            v-for="form in node.forms ?? []"
            :key="form.syntax"
            :label="form.syntax"
            :text="form.text"
          />
        </Block>

        <NodeActions :inspection :node :host />
      </template>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.inspector {
  @apply relative flex-none flex flex-col h-full min-h-0;
  border-left: 1px solid var(--border);
  background: var(--bg-sidebar);
}

.inspector-resize {
  @apply absolute left-0 top-0 bottom-0 w-1 -ml-1 cursor-col-resize z-1 bg-transparent;
  transition: background 0.15s ease;
}

.inspector-resize:hover {
  background: var(--border-strong);
}

.inspector-head {
  @apply flex-none flex items-center;
  height: 2.5rem;
  padding: 0 0.3rem 0 0.875rem;
  border-bottom: 1px solid var(--border);
}

.inspector-title {
  @apply flex-1 min-w-0 text-13 font-semibold;
}

.inspector-head :deep(button.icon-btn) {
  width: 1.625rem;
  height: 1.625rem;
  border-radius: 0.3rem;
}

.inspector-body {
  @apply flex-1 min-h-0 overflow-auto p-4 flex flex-col gap-3.5;
}

.note {
  @apply text-13 text-text-secondary;
  line-height: 1.5;
}

.who {
  @apply flex flex-col gap-1;
}

.who-line {
  @apply flex items-center gap-2;
}

.kind {
  @apply flex-none rounded-[0.25rem] px-1.5 text-11 font-semibold text-accent;
  background: var(--accent-soft);
  line-height: 1.25rem;
}

.name {
  @apply min-w-0 flex-1 truncate text-15 font-semibold;
  font-family: var(--mono);
}

.summary {
  @apply flex-none text-12 text-text-tertiary;
}

.crumbs {
  @apply flex flex-wrap items-center gap-0.5 text-12;
  font-family: var(--mono);
}

.crumb-sep {
  @apply text-text-tertiary;
  padding: 0 1px;
}

.crumb {
  @apply cursor-pointer rounded-sm border-0 bg-transparent px-1 py-0.5 text-text-secondary;
  --wails-draggable: no-drag;
}

.crumb:hover {
  @apply text-text;
  background: var(--bg-hover);
}

.crumb.current {
  @apply font-semibold text-text;
}

.more {
  @apply px-1.5 text-12 text-text-tertiary;
}

.value {
  @apply overflow-auto rounded-lg text-13 text-text;
  max-height: 10rem;
  padding: 0.5rem 0.6875rem;
  font-family: var(--mono);
  background: var(--bg-inset);
  white-space: pre-wrap;
  word-break: break-all;
}

.value.quiet {
  @apply text-text-tertiary;
  font-family: inherit;
  font-style: italic;
}
</style>
