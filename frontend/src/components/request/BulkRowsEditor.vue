<script setup lang="ts">
import { computed, nextTick, onMounted, ref, useId, useTemplateRef, watch } from 'vue'
import { mergeRows, parseRows, rowsToText, toggleRows } from '../../lib/bulkRows'
import type { Row } from '../../../bindings/schemo/internal/domain'
import { usePlatform } from '../../composables/usePlatform'
import { tokenSegments } from '../../lib/vars'
import { vVarPeek } from '../../lib/varPeek'
import VarToken from '../ui/VarToken.vue'
import { rem } from '../../lib/units'
import { t } from '../../i18n'

const { rows, envId } = defineProps<{ rows: Row[]; envId?: string }>()
const emit = defineEmits<{ apply: [rows: Row[]] }>()
const { isMac, shortcut } = usePlatform()
const hintId = useId()
const editor = useTemplateRef<HTMLTextAreaElement>('editor')
const text = ref(rowsToText(rows))
let appliedText = text.value
const scrollLeft = ref(0)
const parsed = computed(() => parseRows(text.value))
const invalid = computed(() => parsed.value.errorLine !== null)
const lines = computed(() => text.value.split('\n').map(line => ({
    text: line,
    segments: tokenSegments(line),
    disabled: /^\s*(#|\/\/)/.test(line),
    invalid: parseRows(line).errorLine !== null,
})))
const lineCount = computed(() => Math.max(5, lines.value.length))

onMounted(() => editor.value?.focus())
watch(text, () => apply(false))

function apply(focus = true): boolean {
    if (invalid.value) {
        if (focus) editor.value?.focus()
        return false
    }
    if (text.value !== appliedText) {
        emit('apply', mergeRows(parsed.value.rows, rows))
        appliedText = text.value
    }
    return true
}

async function onKeydown(event: KeyboardEvent) {
    const modifier = isMac.value ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey
    if (!modifier || event.altKey || event.shiftKey || event.code !== 'Slash') return
    event.preventDefault()
    event.stopPropagation()
    const field = editor.value
    if (!field) return
    const changed = toggleRows(text.value, field.selectionStart, field.selectionEnd)
    text.value = changed.text
    await nextTick()
    field.setSelectionRange(changed.start, changed.end)
}

defineExpose({ apply })
</script>

<template>
  <div class="bulk-editor">
    <div class="bulk-field" :class="{ invalid }">
      <div class="bulk-gutter" aria-hidden="true">
        <span
          v-for="number in lineCount"
          :key="number"
          :class="{ error: lines[number - 1]?.invalid, off: lines[number - 1]?.disabled }"
        >{{ number }}</span>
      </div>
      <div class="bulk-content">
        <div class="bulk-mirror" :style="{ transform: `translateX(${-scrollLeft}px)` }" aria-hidden="true">
          <div
            v-for="(line, index) in lines"
            :key="index"
            class="bulk-line"
            :class="{ error: line.invalid, off: line.disabled }"
          ><span class="bulk-line-text"><template v-for="(segment, position) in line.segments" :key="position"><VarToken
                v-if="segment.tokenName"
                :name="segment.tokenName"
                :text="segment.text"
                :env-id="envId"
              /><span v-else>{{ segment.text }}</span></template></span><span v-if="line.invalid" class="bulk-line-error">{{ t('request.pane.bulkLineError') }}</span></div>
        </div>
        <textarea
          ref="editor"
          v-var-peek="envId"
          v-model="text"
          class="bulk-text"
          :style="{ height: rem(lineCount * 22 + 20) }"
          :aria-label="t('request.pane.bulkEdit')"
          :aria-invalid="invalid"
          :aria-describedby="hintId"
          spellcheck="false"
          autocapitalize="off"
          autocomplete="off"
          wrap="off"
          @keydown="onKeydown"
          @scroll="scrollLeft = ($event.target as HTMLTextAreaElement).scrollLeft"
          @keydown.ctrl.enter.stop.prevent="apply()"
          @keydown.meta.enter.stop.prevent="apply()"
        ></textarea>
      </div>
    </div>
    <div class="bulk-footer">
      <div :id="hintId" class="bulk-hint">{{ t('request.pane.bulkHint') }}</div>
      <span class="bulk-shortcut"><kbd>{{ shortcut('/') }}</kbd>{{ t('request.pane.toggleRow') }}</span>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.bulk-editor {
  @apply flex flex-col;
  gap: 0.5rem;
  padding: 0.125rem 0 0.375rem;
}

.bulk-field {
  @apply flex overflow-hidden;
  border-radius: 0.5625rem;
  background: var(--raise);
  box-shadow: inset 0 0 0 1px var(--line2);
}

.bulk-field:focus-within {
  box-shadow: inset 0 0 0 1px var(--accent), 0 0 0 3px var(--accent-soft);
}

.bulk-content {
  @apply relative flex-1 min-w-0 overflow-hidden;
}

.bulk-text,
.bulk-mirror {
  font-family: var(--mono);
  font-size: var(--text-13);
  line-height: 1.375rem;
  white-space: pre;
  tab-size: 2;
}

.bulk-text {
  @apply relative block w-full border-0 outline-none;
  box-sizing: border-box;
  padding: 0.625rem 0.875rem 0.625rem 0.25rem;
  resize: none;
  color: transparent;
  -webkit-text-fill-color: transparent;
  caret-color: var(--accent);
  background: transparent;
  overflow-x: auto;
  overflow-y: hidden;
  overflow-wrap: normal;
}

.bulk-text::selection {
  background: var(--accent-soft);
}

.bulk-mirror {
  @apply absolute top-0 left-0 pointer-events-none text-text;
  min-width: 100%;
  padding-top: 0.625rem;
}

.bulk-line {
  height: 1.375rem;
  padding-left: 0.25rem;
}

.bulk-line.off {
  color: var(--text-tertiary);
}

.bulk-line.error {
  background: color-mix(in srgb, var(--orange) 9%, transparent);
}

.bulk-line.error .bulk-line-text {
  text-decoration: underline wavy var(--orange);
  text-underline-offset: 0.25rem;
  text-decoration-thickness: 1px;
}

.bulk-line-error {
  padding-left: 3ch;
  color: var(--orange);
  font-size: var(--text-12);
  font-style: italic;
}

.bulk-line.off :deep(.var-token) {
  color: inherit;
  background: transparent;
}

.bulk-gutter {
  @apply flex flex-col flex-none select-none text-right text-text-tertiary;
  width: 2.375rem;
  padding: 0.625rem 0;
  font-family: var(--mono);
  font-size: var(--text-12);
  line-height: 1.375rem;
  font-variant-numeric: tabular-nums;
}

.bulk-gutter span {
  height: 1.375rem;
  padding-right: 0.625rem;
}

.bulk-gutter .off {
  color: var(--border-strong);
}

.bulk-gutter .error {
  color: var(--orange);
  background: color-mix(in srgb, var(--orange) 16%, transparent);
  font-weight: 600;
}

.bulk-footer {
  @apply flex items-center flex-wrap text-text-tertiary;
  gap: 0.5rem 1rem;
  min-height: 1.25rem;
  font-size: var(--text-12);
}

.bulk-hint {
  @apply text-12 text-text-tertiary;
  line-height: 1.5;
}

.bulk-shortcut {
  @apply flex items-center;
  gap: 0.375rem;
  margin-left: auto;
}

kbd {
  @apply flex items-center text-text-secondary;
  height: 1.125rem;
  padding: 0 0.3125rem;
  border-radius: 0.3125rem;
  background: var(--chip);
  box-shadow: inset 0 0 0 1px var(--border-strong);
  font-family: inherit;
  font-size: var(--text-11);
}
</style>
