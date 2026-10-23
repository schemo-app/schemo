<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useTemplateRef, watch } from 'vue'
import { useEventListener } from '@vueuse/core'
import EmptyState from './EmptyState.vue'
import Icon from './Icon.vue'
import IconButton from './IconButton.vue'
import MarkdownText from './MarkdownText.vue'
import Segment from './Segment.vue'
import TextEditor from './TextEditor.vue'
import { isCombo } from '../../lib/commands'
import { markdownHeadings } from '../../lib/markdown'
import { useMarkdownEditor, type DocCommand } from '../../composables/useMarkdownEditor'
import { usePlatform } from '../../composables/usePlatform'
import { t } from '../../i18n'

// The editor's own key, kept off the registry: it is read from the physical key like every other
// combination, so it works on any keyboard layout.
const LINK_COMBO = 'mod+K'

const { modelValue, emptyText, hint = '', startEditing = false, suggestions = [] } = defineProps<{
  modelValue: string
  emptyText: string
  hint?: string
  startEditing?: boolean
  suggestions?: { title: string; text: string }[]
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'commit'): void
  (e: 'cancel'): void
}>()

const { isMac } = usePlatform()

const rootEl = useTemplateRef<HTMLElement>('rootEl')
const visualEl = useTemplateRef<HTMLElement>('visualEl')
const scrollEl = useTemplateRef<HTMLElement>('scrollEl')

const editing = ref(false)
const mode = ref<'visual' | 'markdown'>('visual')

const modeOptions = computed(() => [
  { value: 'visual', label: t('request.docs.visual') },
  { value: 'markdown', label: t('request.docs.markdown') },
])

const empty = computed(() => modelValue.trim() === '')

const headings = computed(() => markdownHeadings(modelValue))

const shallowestLevel = computed(() => headings.value.reduce((low, head) => Math.min(low, head.level), 3))
const hasContents = computed(() => headings.value.length >= 3)
let scrollAfterOpen = false

function addSection(section: { title: string; text: string }) {
  emit('update:modelValue', `${modelValue.trimEnd()}\n\n## ${section.title}\n\n${section.text}\n`.trimStart())
  scrollAfterOpen = true
  start()
}

function scrollToHeading(index: number) {
  const box = scrollEl.value
  if (!box) return
  const found = box.querySelectorAll('h1, h2, h3')[index] as HTMLElement | undefined
  if (!found) return
  const top = found.getBoundingClientRect().top - box.getBoundingClientRect().top + box.scrollTop
  box.scrollTo({ top: top - 20, behavior: 'smooth' })
}

const visual = useMarkdownEditor((text) => emit('update:modelValue', text))
const { standing } = visual
const isBlockMenuOpen = ref(false)
const isMoreMenuOpen = ref(false)

const blockLabel = computed(() => {
  const named: Record<string, string> = {
    h1: t('request.docs.heading1'),
    h2: t('request.docs.heading2'),
    h3: t('request.docs.heading3'),
    blockquote: t('request.docs.quote'),
    pre: t('request.docs.pre'),
  }
  return named[standing.block] ?? t('request.docs.text')
})

const blockItems = computed(() => [
  { tag: 'p', label: t('request.docs.text'), style: 'font-size: var(--text-13); font-weight: 400;' },
  { tag: 'h1', label: t('request.docs.heading1'), style: 'font-size: var(--text-17); font-weight: 650;' },
  { tag: 'h2', label: t('request.docs.heading2'), style: 'font-size: var(--text-15); font-weight: 600;' },
  { tag: 'h3', label: t('request.docs.heading3'), style: 'font-size: var(--text-13-5); font-weight: 600;' },
] as const)

// The block the toolbar names: what the caret is in, with the plain text standing for the rest.
const blankBlock = computed(() => (/^(h[1-3])$/.test(standing.block) ? standing.block : 'p'))

function runCommand(command: DocCommand | 'table') {
  if (command === 'table') {
    const row = (cells: string[]) => `| ${cells.join(' | ')} |`
    visual.insert(
      [
        row([t('request.docs.tableField'), t('request.docs.tableType'), t('request.docs.tableDescription')]),
        row(['---', '---', '---']),
        row(['id', 'number', ' ']),
        '',
      ].join('\n')
    )
  } else {
    visual.run(command, t('request.docs.codeWord'))
  }
  isBlockMenuOpen.value = false
  isMoreMenuOpen.value = false
}

// What is in the document now, said as markdown: the editor tells it a moment after the last change,
// and a description that is left in that moment must not lose it.
function flushVisualText() {
  if (!editing.value || mode.value !== 'visual') return
  const text = visual.markdown()
  if (text !== modelValue) emit('update:modelValue', text)
}

// A link is asked for in a bar of its own, because a URL is typed into a field and not behind a
// dialog: the editor keeps the selection while the field has the keyboard.
const isLinkBarOpen = ref(false)
const linkUrl = ref('https://')
const linkEl = useTemplateRef<HTMLInputElement>('linkEl')

watch(isLinkBarOpen, async (open) => {
  if (!open) return
  await nextTick()
  linkEl.value?.focus()
  linkEl.value?.select()
})

function openLink() {
  if (mode.value !== 'visual') return
  linkUrl.value = visual.linkAtCaret() || 'https://'
  isLinkBarOpen.value = true
}

function applyLink() {
  const url = linkUrl.value.trim()
  isLinkBarOpen.value = false
  visual.link(url === 'https://' ? '' : url)
  visual.focus()
}

function onLinkKey(event: KeyboardEvent) {
  if (event.key === 'Enter') {
    event.preventDefault()
    applyLink()
  }
  if (event.key === 'Escape') {
    event.stopPropagation()
    isLinkBarOpen.value = false
  }
}

// The visual mode is the rendered page itself, editable where it stands; it is made when it is asked
// for and let go of when the mode or the editing ends.
watch([editing, mode], async () => {
  if (editing.value && mode.value === 'visual') {
    await nextTick()
    if (visualEl.value) await visual.open(visualEl.value, modelValue)
    if (scrollAfterOpen && scrollEl.value) {
      scrollEl.value.scrollTo({ top: scrollEl.value.scrollHeight, behavior: 'smooth' })
      scrollAfterOpen = false
    }
  } else {
    void visual.close()
  }
})

function start() {
  editing.value = true
  mode.value = 'visual'
  isLinkBarOpen.value = false
}

function finishEditing() {
  flushVisualText()
  emit('commit')
  editing.value = false
  isLinkBarOpen.value = false
}

function pickMode(value: string) {
  if (value === mode.value) return
  flushVisualText()
  mode.value = value === 'markdown' ? 'markdown' : 'visual'
  isLinkBarOpen.value = false
}

function onKeydown(event: KeyboardEvent) {
  if (isCombo(LINK_COMBO, event, isMac.value)) {
    event.preventDefault()
    event.stopPropagation()
    openLink()
  }
}

function onWindowMousedown(event: MouseEvent) {
  const root = rootEl.value
  if (!root || !(event.target instanceof Node) || !root.contains(event.target)) {
    isBlockMenuOpen.value = false
    isMoreMenuOpen.value = false
    if (!editing.value) return
    flushVisualText()
    emit('commit')
    return
  }
  const target = event.target as Element
  if (!target.closest('.doc-blk') && !target.closest('.doc-more')) {
    isBlockMenuOpen.value = false
    isMoreMenuOpen.value = false
  }
}

function onWindowKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape') return
  if (isLinkBarOpen.value) return
  if (!editing.value) return
  event.preventDefault()
  emit('cancel')
  editing.value = false
  isBlockMenuOpen.value = false
  isMoreMenuOpen.value = false
}

useEventListener(window, 'mousedown', onWindowMousedown)
useEventListener(window, 'keydown', onWindowKeydown)

onMounted(() => {
  if (startEditing) start()
})

onBeforeUnmount(() => {
  flushVisualText()
  emit('commit')
  void visual.close()
})

defineExpose({
  /** Draws other text than what stands in the visual mode: what a caller does after it cancelled. */
  setText(text: string) {
    if (editing.value && mode.value === 'visual') visual.setText(text)
  },
})
</script>

<template>
  <div ref="rootEl" class="doc">
    <!-- Nothing is written and nothing is being written: the empty state below says it all, and a
         bar whose only word is the format has nothing to hold. -->
    <div v-if="editing || !empty" class="doc-head">
      <template v-if="editing">
        <div v-if="mode === 'visual'" class="doc-tools">
          <div class="doc-blk">
            <IconButton
              variant="bare"
              size="bar"
              class="doc-tb doc-blk-btn"
              :class="{ on: isBlockMenuOpen }"
              :hint="t('request.docs.styleTitle')"
              @mousedown.prevent="isBlockMenuOpen = !isBlockMenuOpen"
            >
              <span class="cap-line">{{ blockLabel }}</span>
              <Icon name="ChevronDown" :size="8" :stroke-width="3" class="doc-blk-caret" />
            </IconButton>
            <div v-if="isBlockMenuOpen" class="doc-pop doc-blk-pop">
              <button
                v-for="item in blockItems"
                :key="item.tag"
                type="button"
                class="doc-blk-item"
                :style="item.style"
                @mousedown.prevent="runCommand(item.tag)"
              >
                <span class="cap-line">{{ item.label }}</span>
                <Icon
                  v-if="blankBlock === item.tag"
                  name="Check"
                  :size="12"
                  :stroke-width="2.8"
                  class="doc-check"
                />
              </button>
            </div>
          </div>

          <span class="doc-sep"></span>

          <IconButton
            variant="bare"
            size="bar"
            class="doc-tb"
            :class="{ on: standing.bold }"
            :hint="t('request.docs.boldTitle')"
            @mousedown.prevent="runCommand('bold')"
          >
            <span class="doc-letter doc-bold cap-line">B</span>
          </IconButton>
          <IconButton
            variant="bare"
            size="bar"
            class="doc-tb"
            :class="{ on: standing.italic }"
            :hint="t('request.docs.italicTitle')"
            @mousedown.prevent="runCommand('italic')"
          >
            <span class="doc-letter doc-italic cap-line">I</span>
          </IconButton>
          <IconButton
            variant="bare"
            size="bar"
            class="doc-tb"
            :hint="t('request.docs.codeTitle')"
            @mousedown.prevent="runCommand('code')"
          >
            <Icon name="Code" :size="12" :stroke-width="1.8" />
          </IconButton>
          <IconButton
            variant="bare"
            size="bar"
            class="doc-tb"
            :class="{ on: isLinkBarOpen }"
            :hint="t('request.docs.linkTitle')"
            @mousedown.prevent="openLink"
          >
            <Icon name="Link" :size="12" :stroke-width="1.8" />
          </IconButton>

          <span class="doc-sep"></span>

          <IconButton
            variant="bare"
            size="bar"
            class="doc-tb"
            :class="{ on: standing.ul }"
            :hint="t('request.docs.ulTitle')"
            @mousedown.prevent="runCommand('ul')"
          >
            <Icon name="List" :size="12" :stroke-width="1.8" />
          </IconButton>
          <IconButton
            variant="bare"
            size="bar"
            class="doc-tb"
            :class="{ on: standing.ol }"
            :hint="t('request.docs.olTitle')"
            @mousedown.prevent="runCommand('ol')"
          >
            <Icon name="ListOrdered" :size="12" :stroke-width="1.8" />
          </IconButton>

          <div class="doc-more">
            <IconButton
              variant="bare"
              size="bar"
              class="doc-tb"
              :class="{ on: isMoreMenuOpen }"
              :hint="t('request.docs.moreTitle')"
              @mousedown.prevent="isMoreMenuOpen = !isMoreMenuOpen"
            >
              <Icon name="Ellipsis" :size="12" :stroke-width="1.8" />
            </IconButton>
            <div v-if="isMoreMenuOpen" class="doc-pop doc-more-pop">
              <button type="button" class="doc-more-item" @mousedown.prevent="runCommand('quote')">
                <Icon name="TextQuote" :size="12" :stroke-width="1.8" />
                <span class="cap-line">{{ t('request.docs.quote') }}</span>
              </button>
              <button type="button" class="doc-more-item" @mousedown.prevent="runCommand('pre')">
                <Icon name="SquareCode" :size="12" :stroke-width="1.8" />
                <span class="cap-line">{{ t('request.docs.pre') }}</span>
              </button>
              <button type="button" class="doc-more-item" @mousedown.prevent="runCommand('table')">
                <Icon name="Table" :size="12" :stroke-width="1.8" />
                <span class="cap-line">{{ t('request.docs.table') }}</span>
              </button>
              <button type="button" class="doc-more-item" @mousedown.prevent="runCommand('hr')">
                <Icon name="Minus" :size="12" :stroke-width="1.8" />
                <span class="cap-line">{{ t('request.docs.divider') }}</span>
              </button>
            </div>
          </div>
        </div>

        <span class="doc-gap"></span>

        <Segment
          class="flex-none ml-1.5 mx-2"
          size="xs"
          :grow="false"
          :options="modeOptions"
          :value="mode"
          @pick="pickMode"
        />

        <button type="button" class="doc-done" :title="hint" @click="finishEditing">
          {{ t('request.docs.done') }}
        </button>
      </template>

      <template v-else>
        <span class="doc-tagline">{{ t('request.docs.markdown') }}</span>
        <span class="doc-gap"></span>
        <button
          v-if="!empty"
          type="button"
          class="doc-edit"
          :title="t('request.docs.editHint')"
          @click="start"
        >
          <Icon name="Pencil" :size="14" :stroke-width="1.8" />
          <span class="cap-line">{{ t('request.docs.edit') }}</span>
        </button>
      </template>
    </div>

    <div v-if="editing && isLinkBarOpen" class="doc-link-bar">
      <span class="doc-link-label">{{ t('request.docs.link') }}</span>
      <input
        ref="linkEl"
        v-model="linkUrl"
        class="doc-link-input"
        spellcheck="false"
        placeholder="https://"
        @keydown="onLinkKey"
      />
      <button type="button" class="doc-link-cancel" @click="isLinkBarOpen = false">
        {{ t('common.cancel') }}
      </button>
      <button type="button" class="doc-link-apply" @click="applyLink">
        {{ t('request.docs.apply') }}
      </button>
    </div>

    <template v-if="editing">
      <div v-if="mode === 'visual'" ref="scrollEl" class="doc-scroll doc-scroll-visual">
        <div class="doc-center">
          <div ref="visualEl" class="doc-body" @keydown="onKeydown"></div>
        </div>
      </div>
      <TextEditor
        v-else
        class="doc-src"
        language="markdown"
        :placeholder="`# ${t('request.docs.heading1')}`"
        :model-value="modelValue"
        @update:model-value="emit('update:modelValue', $event)"
      />
    </template>

    <template v-else>
      <EmptyState v-if="empty" :title="t('request.docs.emptyTitle')" :text="emptyText">
        <button type="button" class="doc-empty-start" @click="start">
          {{ t('request.docs.write') }}
        </button>
      </EmptyState>
      <div v-else ref="scrollEl" class="doc-scroll">
        <div class="doc-center">
          <div class="doc-body" @dblclick="start">
            <MarkdownText :text="modelValue" />
          </div>
          <div v-if="hasContents || suggestions.length" class="doc-toc" data-toc>
            <span class="doc-toc-title">{{ t('request.docs.onPage') }}</span>
            <button
              v-for="(head, i) in headings"
              :key="i"
              type="button"
              class="doc-toc-item"
              :class="{ lead: head.level === shallowestLevel }"
              :style="{ paddingLeft: `calc(0.5rem + ${(head.level - shallowestLevel) * 12}px)` }"
              @click="scrollToHeading(i)"
            >
              {{ head.label }}
            </button>
            <template v-if="suggestions.length">
              <span class="doc-toc-title doc-toc-add">{{ t('request.docs.addToPage') }}</span>
              <button
                v-for="section in suggestions"
                :key="section.title"
                type="button"
                class="doc-toc-item doc-toc-suggestion"
                @click="addSection(section)"
              >
                <Icon name="Plus" :size="12" />
                {{ section.title }}
              </button>
            </template>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.doc {
  @apply flex flex-col flex-1 min-h-0;
}

.doc-head {
  @apply flex flex-none items-center;
  box-sizing: border-box;
  min-height: 2.75rem;
  gap: 0.125rem;
  padding: 0.5rem 0.75rem 0.5rem 1rem;
  border-bottom: 1px solid var(--line2);
}

.doc-tools {
  @apply flex items-center min-w-0;
  gap: 0.0625rem;
}

.doc-tools :deep(.doc-tb.on) {
  @apply text-text;
  background: var(--bg-hover);
}

.doc-letter {
  font-size: var(--text-13-5);
  font-weight: 700;
}

.doc-italic {
  font-family: Georgia, serif;
  font-size: var(--text-14);
  font-weight: 500;
  font-style: italic;
}

.doc-sep {
  @apply flex-none;
  width: 1px;
  height: 1rem;
  background: var(--border);
  margin: 0 0.1875rem;
}

.doc-blk,
.doc-more {
  @apply relative flex-none;
}

.doc-tools :deep(.doc-blk-btn) {
  width: 6.5rem;
  justify-content: space-between;
  gap: 0.3rem;
  padding: 0 0.3rem 0 0.5rem;
  font-size: var(--text-12-5);
}

.doc-blk-caret {
  @apply flex-none text-text-tertiary;
}

.doc-pop {
  @apply absolute flex flex-col;
  z-index: 21;
  border-radius: 0.75rem;
  background: var(--pop);
  backdrop-filter: var(--blur-overlay);
  -webkit-backdrop-filter: var(--blur-overlay);
  box-shadow: var(--popshadow);
  padding: 0.3rem;
  gap: 0.0625rem;
}

.doc-blk-pop {
  left: 0;
  top: 2rem;
  width: 12.5rem;
}

.doc-blk-item {
  @apply flex items-center justify-between cursor-pointer border-0 bg-transparent text-text;
  height: 2.125rem;
  padding: 0 0.6rem;
  border-radius: 0.4375rem;
  font-family: inherit;
}

.doc-blk-item:hover,
.doc-more-item:hover {
  background: var(--bg-hover);
}

.doc-check {
  @apply text-accent;
}

.doc-more-pop {
  right: 0;
  top: 2rem;
  width: 12.5rem;
}

.doc-more-item {
  @apply flex items-center cursor-pointer border-0 bg-transparent text-text;
  height: 2rem;
  gap: 0.625rem;
  padding: 0 0.6rem;
  border-radius: 0.4375rem;
  font-family: inherit;
  font-size: inherit;
}

.doc-gap {
  @apply flex-1;
  min-width: 0.5rem;
}

.doc-done {
  @apply flex-none cursor-pointer border-0 font-semibold;
  height: 1.75rem;
  padding: 0 0.75rem;
  border-radius: 0.4375rem;
  background: var(--accent);
  color: var(--accent-text);
  font-family: inherit;
  font-size: var(--text-13);
}

.doc-tagline {
  @apply text-text-tertiary;
  font-size: var(--text-12);
}

.doc-edit {
  @apply flex items-center flex-none cursor-pointer border-0 text-text;
  height: 1.75rem;
  padding: 0 0.625rem 0 0.5rem;
  gap: 0.3rem;
  border-radius: 0.4375rem;
  background: var(--chip);
  font-family: inherit;
  font-size: var(--text-13);
}

.doc-edit:hover {
  background: var(--bg-hover);
}

.doc-link-bar {
  @apply flex flex-none items-center;
  height: 2.5rem;
  gap: 0.5rem;
  padding: 0 0.75rem 0 1rem;
  background: var(--raise);
  border-bottom: 1px solid var(--line2);
}

.doc-link-label {
  @apply flex-none text-text-secondary;
  font-size: var(--text-12);
}

.doc-link-input {
  @apply flex-1 min-w-0;
  height: 1.75rem;
  box-sizing: border-box;
  border: 0;
  outline: none;
  border-radius: 0.4375rem;
  background: var(--bg);
  box-shadow: inset 0 0 0 1px var(--accent), 0 0 0 3px var(--accent-soft);
  padding: 0 0.6rem;
  font-family: var(--mono);
  font-size: var(--text-12-5);
  color: var(--text);
}

.doc-link-cancel {
  @apply flex-none cursor-pointer border-0 bg-transparent text-text-secondary;
  height: 1.75rem;
  padding: 0 0.6rem;
  border-radius: 0.4375rem;
  font-family: inherit;
  font-size: var(--text-12-5);
}

.doc-link-cancel:hover {
  background: var(--bg-hover);
}

.doc-link-apply {
  @apply flex-none cursor-pointer border-0 font-semibold text-text;
  height: 1.75rem;
  padding: 0 0.75rem;
  border-radius: 0.4375rem;
  background: var(--chip);
  font-family: inherit;
  font-size: var(--text-12-5);
}

.doc-link-apply:hover {
  background: var(--bg-hover);
}

.doc-scroll {
  @apply flex-1 min-h-0 overflow-auto;
}

.doc-scroll-visual {
  cursor: text;
}

.doc-center {
  container-type: inline-size;
  display: flex;
  justify-content: center;
  gap: 2.5rem;
  padding: 1.5rem 2rem 4rem;
}

.doc-scroll-visual .doc-center {
  padding: 1.5rem 2rem 0;
}

.doc-body {
  flex: 0 1 45rem;
  min-width: 0;
}

.doc-body :deep(.ProseMirror) {
  min-height: 20rem;
  padding-bottom: 7.5rem;
  outline: none;
  white-space: pre-wrap;
  word-wrap: break-word;
}

.doc-toc {
  flex: 0 0 11.25rem;
  position: sticky;
  top: 0;
  align-self: flex-start;
  display: flex;
  flex-direction: column;
  gap: 0.0625rem;
  padding-top: 0.125rem;
}

@container (max-width: 45rem) {
  .doc-toc {
    display: none !important;
  }
}

.doc-toc-title {
  @apply text-text-tertiary;
  font-size: var(--text-11-5);
  padding: 0 0.5rem 0.3rem;
}

.doc-toc-item {
  @apply block cursor-default border-0 bg-transparent text-left text-text-secondary;
  padding: 0.25rem 0.5rem;
  border-radius: 0.3rem;
  font-family: inherit;
  font-size: var(--text-12-5);
  line-height: 1.35;
}

.doc-toc-item.lead {
  @apply text-text;
}

.doc-toc-add {
  padding-top: 1rem;
}

.doc-toc-suggestion {
  @apply flex items-center text-accent cursor-pointer;
  gap: 0.3rem;
}

.doc-toc-item:hover {
  @apply text-text;
  background: var(--bg-hover);
}

.doc-src :deep(.cm-content) {
  padding: 1.25rem 0 2.5rem;
}

.doc-src :deep(.cm-line) {
  padding: 0 2rem;
}

.doc-empty-start {
  @apply cursor-pointer border-0 font-semibold;
  height: 1.875rem;
  padding: 0 0.875rem;
  border-radius: 0.4375rem;
  background: var(--accent);
  color: var(--accent-text);
  margin-top: 0.3rem;
  font-family: inherit;
  font-size: var(--text-12-5);
}
</style>
