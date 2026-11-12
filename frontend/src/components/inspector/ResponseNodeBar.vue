<script setup lang="ts">
import { computed, ref, shallowRef, useTemplateRef, watch } from 'vue'
import {
  DialogTitle,
  DropdownMenuContent,
  DropdownMenuPortal,
  DropdownMenuRoot as DropdownMenu,
  DropdownMenuTrigger,
  PopoverRoot as Popover,
} from 'reka-ui'
import { CaptureSource, NodeKind, type NodeView } from '../../../bindings/schemo/internal/domain'
import { isContainer, type Inspection } from '../../composables/useInspection'
import { useToast } from '../../composables/useToast'
import { formatNumber, t, textOf } from '../../i18n'
import { looksSecret, variableNameFor } from '../../lib/capturePolicy'
import { copyToClipboard } from '../../lib/clipboard'
import { formatReadingTime } from '../../lib/reading'
import type { RequestSource } from '../../lib/requestSource'
import Dialog from '../ui/Dialog.vue'
import DropdownMenuItem from '../ui/DropdownMenuItem.vue'
import DropdownMenuSeparator from '../ui/DropdownMenuSeparator.vue'
import Icon from '../ui/Icon.vue'
import PopoverContent from '../ui/PopoverContent.vue'
import NodeInspector from './NodeInspector.vue'
import SaveToVariable from './SaveToVariable.vue'

const { node, inspection, host, body, status, canFollow } = defineProps<{
  node: NodeView
  inspection: Inspection
  host: RequestSource | null
  body: string
  status: number
  canFollow: boolean
}>()
const emit = defineEmits<{ pick: [path: string]; follow: [url: string] }>()

const toast = useToast()
const trigger = useTemplateRef<HTMLButtonElement>('trigger')
const menu = ref(false)
const saving = ref(false)
const details = ref(false)
const adding = ref(false)
const point = shallowRef<{ getBoundingClientRect: () => DOMRect }>()
const anchor = computed(() => point.value ?? trigger.value ?? undefined)

const scalar = computed(() => !isContainer(node))
const scriptPath = computed(() => node.forms?.find(form => form.syntax === 'Postman')?.text)
const javascript = computed(() => scriptPath.value ?? node.forms?.find(form => form.syntax === 'JavaScript')?.text)
const canAddCheck = computed(() => scalar.value && !!scriptPath.value && !!host?.scriptsLevel)
const note = computed(() => {
  const time = node.readings?.find(reading => reading.kind === 'timestamp' || reading.kind === 'datetime')
  const moment = time?.rows?.find(row => row.key === 'time')
  if (moment) return formatReadingTime(moment)
  if (node.kind !== NodeKind.NodeNumber) return ''
  const value = Number(node.preview)
  return Number.isSafeInteger(value) && Math.abs(value) >= 10_000 ? formatNumber(value) : ''
})

function openActions(event?: MouseEvent) {
  if (saving.value || details.value) return
  if (event) {
    const { clientX, clientY } = event
    point.value = { getBoundingClientRect: () => new DOMRect(clientX, clientY, 0, 0) }
  } else {
    point.value = undefined
  }
  menu.value = true
}

function useToolbarAnchor() {
  if (!menu.value) point.value = undefined
}

function restoreFocus(event: Event) {
  if (saving.value || details.value) event.preventDefault()
}

async function copy(text: string | null | undefined) {
  if (text != null && await copyToClipboard(text)) toast.show(t('common.copied'))
}

function valueOf() {
  return inspection.valueOf(node)
}

async function addCheck() {
  const path = scriptPath.value
  if (!host || !path || !canAddCheck.value || adding.value) return
  const name = JSON.stringify(node.name || node.path)
  const value = body.slice(node.start, node.end)
  const statement = `pm.test(${name}, () => pm.expect(${path}).to.equal(${value}));`
  adding.value = true
  try {
    if (await host.appendPostScript(statement)) toast.show(t('inspector.checkAdded'))
  } finally {
    adding.value = false
  }
}

function showDetails() {
  details.value = true
  void inspection.diagnose(status)
}

watch(() => node.path, () => {
  menu.value = false
  saving.value = false
})

defineExpose({ openActions })
</script>

<template>
  <div class="node-bar">
    <nav class="node-crumbs mono" :aria-label="t('inspector.address')">
      <template v-for="(crumb, index) in node.crumbs ?? []" :key="crumb.path">
        <span v-if="index" class="node-separator" aria-hidden="true">›</span>
        <button
          type="button"
          class="node-crumb"
          :aria-current="crumb.path === node.path ? 'location' : undefined"
          :title="crumb.path"
          @click="emit('pick', crumb.path)"
        >
          {{ crumb.name || (index === 0 ? crumb.path : '""') }}
        </button>
      </template>
    </nav>
    <span class="node-kind mono" :data-kind="node.kind" :title="textOf(`inspector.kinds.${node.kind}`)">{{ node.kind }}</span>
    <span v-if="note" class="node-note" :title="note">{{ note }}</span>

    <DropdownMenu v-model:open="menu">
      <DropdownMenuTrigger as-child>
        <button
          ref="trigger"
          type="button"
          class="node-trigger"
          :title="t('inspector.actions')"
          :aria-label="t('inspector.actions')"
          @pointerdown="useToolbarAnchor"
          @keydown="useToolbarAnchor"
        >
          <Icon name="ChevronDown" :size="9" :stroke-width="3" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuPortal>
        <DropdownMenuContent
          class="menu node-menu"
          align="start"
          :reference="anchor"
          :side-offset="4"
          :collision-padding="8"
          @close-auto-focus="restoreFocus"
        >
          <DropdownMenuItem @select="async () => copy(await valueOf())">
            <span>{{ t('inspector.copyValue') }}</span>
            <span v-if="node.preview" class="node-menu-preview mono">{{ node.preview }}</span>
          </DropdownMenuItem>
          <DropdownMenuItem @select="copy(node.path)">
            <span>{{ t('inspector.copyPath') }}</span>
            <span class="node-menu-preview mono">{{ node.path }}</span>
          </DropdownMenuItem>
          <DropdownMenuItem v-if="javascript" @select="copy(javascript)">
            <span>{{ t('inspector.copyJavaScript') }}</span>
            <span class="node-menu-preview mono">{{ javascript }}</span>
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem v-if="scalar" @select="saving = true">
            {{ t('inspector.saveToVariable') }}
          </DropdownMenuItem>
          <DropdownMenuItem v-if="canAddCheck" :disabled="adding" @select="addCheck">
            {{ t('inspector.addCheck') }}
          </DropdownMenuItem>
          <DropdownMenuItem @select="showDetails">{{ t('inspector.details') }}</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenuPortal>
    </DropdownMenu>

    <Popover v-model:open="saving">
      <PopoverContent
        class="node-save-popover"
        :aria-label="t('inspector.saveToVariable')"
        :reference="anchor"
        :side-offset="4"
      >
        <SaveToVariable
          :key="node.path"
          :suggested="variableNameFor(node.name)"
          :secret-by-default="looksSecret(node.name) || !!node.readings?.some(reading => reading.kind === 'jwt')"
          :source="CaptureSource.CaptureBody"
          :path="node.path"
          :host
          :value="valueOf"
          compact
          @done="saving = false"
          @cancel="saving = false"
        >
          <template #heading="{ preview }">
            <span class="node-kind mono" :data-kind="node.kind">{{ node.kind }}</span>
            <span class="node-note mono">{{ preview }}</span>
          </template>
        </SaveToVariable>
      </PopoverContent>
    </Popover>

    <Dialog v-model:open="details">
      <DialogTitle class="sr-only">{{ t('inspector.details') }}</DialogTitle>
      <NodeInspector
        :inspection
        :host
        :can-follow
        @close="details = false"
        @pick="emit('pick', $event)"
        @fetch="emit('follow', $event)"
      />
    </Dialog>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.node-bar {
  @apply flex items-center flex-1 min-w-0;
  gap: 0.5rem;
}

.node-crumbs {
  @apply flex items-center min-w-0 overflow-x-auto whitespace-nowrap;
  scrollbar-width: none;
  font-size: var(--text-12);
}

.node-crumb {
  @apply border-0 bg-transparent text-text-secondary cursor-pointer flex-none;
  padding: 0.0625rem 0.25rem;
  border-radius: 0.25rem;
}

.node-crumb:hover {
  @apply bg-bg-hover text-text;
}

.node-crumb[aria-current] {
  @apply font-semibold text-text;
}

.node-separator {
  @apply text-text-tertiary;
  padding: 0 0.125rem;
}

.node-kind {
  @apply flex-none font-semibold text-text-secondary;
  padding: 0.125rem 0.4375rem;
  border-radius: 0.3125rem;
  background: var(--chip);
  font-size: var(--text-12);
}

.node-kind[data-kind='string'] {
  background: var(--red-soft);
  color: var(--tok-str);
}

.node-kind[data-kind='number'] {
  background: var(--accent-soft);
  color: var(--tok-num);
}

.node-kind[data-kind='boolean'] {
  background: var(--accent-soft);
  color: var(--purple);
}

.node-kind[data-kind='null'] {
  color: var(--purple);
}

.node-note {
  @apply text-text-tertiary min-w-0 truncate;
  font-size: var(--text-12);
}

.node-trigger {
  @apply inline-flex flex-none items-center justify-center cursor-pointer bg-transparent text-text-tertiary border-0;
  width: 1.375rem;
  height: 1.375rem;
  padding: 0;
  border-radius: 0.375rem;
}

.node-trigger:hover,
.node-trigger[data-state='open'] {
  @apply bg-bg-hover text-text;
}

:global(.node-menu) {
  width: min(17.5rem, calc(100vw - 1rem));
}

:global(.node-menu .menu-item) {
  @apply justify-between whitespace-nowrap;
  min-height: 2rem;
}

.node-menu-preview {
  @apply text-text-tertiary min-w-0 truncate;
  max-width: 6.875rem;
  font-size: var(--text-12);
}

:global(.node-save-popover) {
  width: min(18.75rem, calc(100vw - 1rem));
  padding: 0.875rem;
}

:global(.node-save-popover .node-note) {
  max-width: 5.25rem;
}
</style>
