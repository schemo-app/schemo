<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, useTemplateRef } from 'vue'
import { useEventListener } from '@vueuse/core'
import Icon from '../ui/Icon.vue'
import BarEnvironment from './BarEnvironment.vue'
import RequestPane from './RequestPane.vue'
import SaveRequestPopover from '../collections/SaveRequestPopover.vue'
import CopyRequestMenu from './CopyRequestMenu.vue'
import { Popover, PopoverContent, PopoverTrigger } from '../ui/popover'
import VarToken from '../ui/VarToken.vue'
import { vVarPeek } from '../../composables/useVarPeek'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from '../ui/dropdown-menu'
import type { PaneSource } from '../../stores/content'
import { METHODS, methodInkClass } from '../../lib/format'
import { useMissingVars } from '../../composables/request/useMissingVars'
import { useWholeWidth } from '../../composables/useWholeWidth'
import { useRequestSave } from '../../composables/request/useRequestSave'
import { useRequestSend } from '../../composables/request/useRequestSend'
import { useCommands } from '../../composables/useCommands'
import { registerUrlField, saveRequest } from '../../composables/intents'
import { urlPieces, type UrlPiece } from '../../lib/vars'
import { looksLikeCommand } from '../../lib/commandShape'
import { Kind } from '../../../bindings/schemo/internal/command'
import { useToast } from '../../composables/useToast'
import { textOf, useMessages } from '../../i18n'

// The pane draws the tab that is on screen, and the tab hands it its source: what is being composed
// is the strip's answer, not a store's.
const { source } = defineProps<{ source: PaneSource }>()

const { t } = useMessages()
const { matches } = useCommands()

const store: PaneSource = source

const toast = useToast()

function selectMethod(m: string) {
  void store.setMethod(m)
}

const {
  open: helpOpen,
  missing,
  marked,
  rows: missingRows,
  title: missingTitle,
  mayAdd,
  add,
} = useMissingVars(store)

const { label: sendLabel, key: sendKey, disabled: sendDisabled, send, onSend } = useRequestSend(store)

const sendBtn = useTemplateRef<HTMLElement>('sendBtn')
useWholeWidth(sendBtn, () => [sendLabel.value, sendKey.value, store.loading])

const urlInputRef = useTemplateRef<HTMLInputElement>('urlInputRef')

onMounted(() => onBeforeUnmount(registerUrlField(() => urlInputRef.value?.focus())))

const urlDisplayRef = useTemplateRef<HTMLElement>('urlDisplayRef')

const urlParts = computed(() => urlPieces(store.url))

const unfilledPaths = computed(
  () => new Set(store.pathParams.filter((row) => row.value === '').map((row) => row.name))
)

function isUnfilled(piece: UrlPiece): boolean {
  return piece.kind === 'path-param' && unfilledPaths.value.has(piece.text.slice(1))
}
const showUrlDisplay = computed(() => urlParts.value.length > 0)

const {
  open: saveOpen,
  defaultName: saveDefaultName,
  bound: saveBound,
  canNow: canSaveNow,
  marked: saveMarked,
  hint: saveHint,
  save,
  saveShortcut,
} = useRequestSave(source, () => source.canSave)

saveRequest.on(() => {
  void save()
})

function syncUrlScroll() {
  const input = urlInputRef.value
  const display = urlDisplayRef.value
  if (input && display) display.scrollLeft = input.scrollLeft
}

function onWindowKeydown(e: KeyboardEvent) {
  // What save and send are bound to comes from the registry, like every other command's. A field
  // that sends on its own Enter answers to a bare one: ⌘↵ arrives here alone, and one is one send.
  if (matches('save', e)) {
    if (!saveBound.value && !canSaveNow.value) return
    e.preventDefault()
    void saveShortcut()
    return
  }
  if (matches('send', e)) {
    e.preventDefault()
    void send()
  }
}

async function onUrlPaste(e: ClipboardEvent) {
  const input = e.target as HTMLInputElement
  const text = e.clipboardData?.getData('text/plain') ?? ''
  if (!looksLikeCommand(text)) return

  e.preventDefault()
  const reading = await store.pasteCommand(text)
  if (!reading) {
    insertAtCaret(input, text)
    return
  }

  if (reading.kind === Kind.KindError) {
    toast.show(t('request.pasteFailed', { reason: textOf(`request.parseError.${reading.reason}`) }), 'error')
    return
  }
  if (reading.kind !== Kind.KindOK) {
    insertAtCaret(input, text)
    return
  }

  void nextTick(syncUrlScroll)
  toast.show(t('request.pasteRecognised'))
}

function insertAtCaret(input: HTMLInputElement, text: string) {
  const from = input.selectionStart ?? input.value.length
  const to = input.selectionEnd ?? from
  input.setRangeText(text, from, to, 'end')
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

useEventListener(window, 'keydown', onWindowKeydown)
</script>

<template>
  <div class="request-block">
    <div class="bar-row">
      <div class="url-field">
        <DropdownMenu>
          <DropdownMenuTrigger as-child>
            <button class="method-plate method-btn" :class="methodInkClass(store.method)">
              <span class="cap-line">{{ store.method }}</span>
              <Icon name="ChevronDown" :size="10" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" :side-offset="4">
            <DropdownMenuItem
              v-for="m in METHODS"
              :key="m"
              :class="methodInkClass(m)"
              @select="selectMethod(m)"
            >
              {{ m }}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>

        <div class="url-text">
          <input
            ref="urlInputRef"
            v-var-peek="store.environmentId"
            :value="store.url"
            class="url-input mono"
            :class="{ 'url-input-veiled': showUrlDisplay }"
            :placeholder="t('request.urlPlaceholder')"
            spellcheck="false"
            @input="store.setUrl(($event.target as HTMLInputElement).value); syncUrlScroll()"
            @keydown.enter.exact="send"
            @blur="store.flush()"
            @paste="onUrlPaste"
            @scroll="syncUrlScroll"
          />
          <div
            v-if="showUrlDisplay"
            ref="urlDisplayRef"
            class="url-display mono"
            aria-hidden="true"
          >
            <template v-for="(piece, i) in urlParts" :key="i">
              <VarToken
                v-if="piece.tokenName"
                :name="piece.tokenName"
                :text="piece.text"
                :env-id="store.environmentId"
              />
              <span v-else :class="[piece.kind, { unfilled: isUnfilled(piece) }]">{{ piece.text }}</span>
            </template>
          </div>
        </div>

        <Popover v-if="marked" v-model:open="helpOpen">
          <PopoverTrigger as-child>
            <button
              type="button"
              class="var-mark"
              :class="{ open: helpOpen }"
              :title="t('counts.variablesMissing', missing.length)"
            >
              <Icon name="TriangleAlert" :size="14" :stroke-width="2" />
            </button>
          </PopoverTrigger>
          <PopoverContent class="var-help" align="end" :side-offset="6">
            <span class="var-help-head">{{ t('request.missingHeader', { n: missing.length }) }}</span>

            <div v-for="one in missingRows" :key="one.name" class="var-help-row">
              <span class="var-help-who">
                <span class="var-help-name mono">{{ one.label }}</span>
                <span v-if="one.place" class="var-help-where">{{ one.place }}</span>
              </span>
              <button
                type="button"
                class="var-help-add"
                :disabled="!mayAdd"
                :title="missingTitle"
                @click="add([one.name])"
              >
                {{ t('request.createVar') }}
              </button>
            </div>

            <span class="var-help-note">{{ t('request.missingNote') }}</span>

            <template v-if="missing.length > 1">
              <span class="var-help-line"></span>
              <button
                type="button"
                class="var-help-all"
                :disabled="!mayAdd"
                :title="missingTitle"
                @click="add(missingRows.map((one) => one.name))"
              >
                {{ t('request.createVarAll', { n: missing.length }) }}
              </button>
            </template>
          </PopoverContent>
        </Popover>
      </div>

      <template v-if="!store.blank">
        <BarEnvironment :source="store" />

        <CopyRequestMenu :source="store" />
      </template>

      <span v-if="!store.blank" class="save-anchor">
        <button
          type="button"
          class="save-icon"
          :class="{ open: saveOpen, live: saveMarked }"
          :title="saveHint"
          @click="save()"
        >
          <Icon :name="saveBound ? 'Save' : 'FolderPlus'" :size="16" />
          <span v-if="saveMarked" class="unsaved-dot"></span>
        </button>
        <SaveRequestPopover
          ref="savePanel"
          v-model:open="saveOpen"
          :url="store.url"
          :default-name="saveDefaultName"
          :draft-id="source.draftId"
          @saved="source.markClean()"
        />
      </span>

      <button
        ref="sendBtn"
        type="button"
        class="send-btn"
        :class="{ sending: store.loading }"
        :disabled="sendDisabled"
        @click="onSend"
      >
        <span v-if="store.loading" class="send-spinner"></span>
        <span>{{ sendLabel }}</span>
        <span class="send-hint">{{ sendKey }}</span>
      </button>
    </div>

    <RequestPane v-if="!store.blank" :source="store" />
  </div>
</template>

<style scoped>
@reference "../../style.css";

.method-btn {
  @apply border-0 cursor-pointer outline-none;
  --wails-draggable: no-drag;
}

.method-btn:hover {
  background: var(--bg-hover);
}

.url-text {
  @apply relative flex-1 flex items-center min-w-0;
  min-width: 7.5rem;
}

.url-input {
  @apply flex-1 min-w-0 bg-transparent border-0 outline-none;
  padding: 0;
  font-family: var(--mono);
  font-size: var(--text-13);
  color: var(--text);
}

.url-input-veiled {
  color: transparent;
  caret-color: var(--text);
}

.url-display {
  @apply absolute inset-0 flex items-center overflow-hidden pointer-events-none;
  padding: 0;
  font-family: var(--mono);
  font-size: var(--text-13);
  white-space: pre;
  color: var(--text);
}

.url-display .query-key {
  color: var(--text-tertiary);
}

.url-display .query-value {
  color: var(--accent);
}

.url-display .path-param {
  color: var(--accent);
}

.url-display .path-param.unfilled {
  color: var(--red);
}

.url-input:focus,
.url-input:focus-visible {
  border: 0;
  box-shadow: none;
}

.save-anchor {
  @apply relative flex-none;
}

.save-icon {
  @apply relative flex items-center justify-center border-0 bg-transparent cursor-default;
  width: 2.375rem;
  height: 2.375rem;
  border-radius: 0.5625rem;
  color: var(--text-tertiary);
}

.save-icon.live {
  @apply cursor-pointer;
  color: var(--text);
}

.save-icon:hover,
.save-icon.open {
  background: var(--bg-hover);
}

.unsaved-dot {
  @apply absolute rounded-full bg-accent;
  width: 0.4375rem;
  height: 0.4375rem;
  top: 0.5rem;
  right: 0.5rem;
  box-shadow: 0 0 0 2px var(--bg);
}

.send-btn {
  @apply flex items-center justify-center flex-none cursor-pointer border-0;
  min-width: max(8rem, var(--whole-width, 0px));
  height: 2.375rem;
  padding: 0 1rem;
  border-radius: 0.5625rem;
  gap: 0.625rem;
  background: var(--accent);
  color: var(--accent-text);
  font: inherit;
  font-size: var(--text-13);
  font-weight: 600;
}

.send-btn:disabled {
  cursor: default;
  opacity: 0.4;
}

.send-btn.sending {
  background: var(--red);
}

.send-spinner {
  @apply flex-none w-3.5 h-3.5 rounded-full;
  border: 0.125rem solid color-mix(in srgb, #fff 35%, transparent);
  border-top-color: #fff;
  animation: spin 0.72s linear infinite;
}

.send-hint {
  @apply font-medium;
  font-size: var(--text-11);
  opacity: 0.7;
}

.var-mark {
  @apply flex-none inline-flex items-center justify-center w-6 h-6 rounded-[0.25rem] cursor-pointer
         border-0 bg-transparent;
  margin-left: 0.375rem;
  color: var(--red-text);
  --wails-draggable: no-drag;
}

.var-mark:hover,
.var-mark.open {
  background: var(--red-soft);
}

</style>