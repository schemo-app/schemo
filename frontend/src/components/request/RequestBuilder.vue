<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, useTemplateRef, toValue, watch, type MaybeRefOrGetter, ref } from 'vue'
import { useEventListener, useResizeObserver } from '@vueuse/core'
import Icon from '../ui/Icon.vue'
import Spinner from '../ui/Spinner.vue'
import RequestEnvironment from './RequestEnvironment.vue'
import RequestPane from './RequestPane.vue'
import SaveRequestPopover from '../collections/SaveRequestPopover.vue'
import CopyRequestMenu from './CopyRequestMenu.vue'
import PopoverContent from '../ui/PopoverContent.vue'
import { PopoverRoot as Popover, PopoverTrigger, DropdownMenuRoot as DropdownMenu, DropdownMenuTrigger } from 'reka-ui'
import VarToken from '../ui/VarToken.vue'
import { vVarPeek } from '../../lib/varPeek'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import DropdownMenuItem from '../ui/DropdownMenuItem.vue'
import type { PaneSource } from '../../stores/content'
import { METHODS, methodInkClass } from '../../lib/format'
import { useRequestSave } from '../../composables/useRequestSave'
import { useRequestSend } from '../../composables/useRequestSend'
import { useCommands } from '../../composables/useCommands'
import { registerUrlField, saveRequest } from '../../lib/intents'
import { urlPieces, type UrlPiece } from '../../lib/vars'
import { Kind } from '../../../bindings/schemo/internal/convert/command'
import { useToast } from '../../composables/useToast'
import { textOf, t, describeFailure } from '../../i18n'
import type { RequestSource } from '../../lib/requestSource'
import { missingPlace } from '../../lib/missingPlace'
import { useEnvironmentsStore } from '../../stores/environments'
import { useRequestEnvironment } from '../../composables/useRequestEnvironment'
// Shell prompt prefixes (PowerShell, bash, zsh) and sudo wrapper
const PROMPT = /^(?:PS[^>\n]*>|>|\$|❯)\s+/
const SUDO = /^sudo\s+/

// Matches curl CLI execution (including curl.exe for Windows)
const COMMAND = /^curl(?:\.exe)?\b/i

/**
 * Fast synchronous check to determine if pasted text resembles a curl command.
 * Strips leading shell prompts/sudo before matching. Used in paste handlers to
 * decide whether to call event.preventDefault() for async backend parsing.
 */
function looksLikeCommand(text: string): boolean {
  let rest = text.trimStart()
  for (;;) {
    const stripped = rest.replace(PROMPT, '').replace(SUDO, '')
    if (stripped === rest) break
    rest = stripped
  }
  return COMMAND.test(rest)
}


// The pane draws the tab that is on screen, and the tab hands it its source: what is being composed
// is the strip's answer, not a store's.
const { source } = defineProps<{ source: PaneSource }>()

const { matches } = useCommands()


const toast = useToast()

function selectMethod(m: string) {
  void source.setMethod(m)
}

const {
  open: helpOpen,
  missing,
  marked,
  rows: missingRows,
  title: missingTitle,
  mayAdd,
  add,
} = useMissingVars(source)

const { label: sendLabel, key: sendKey, disabled: sendDisabled, send, onSend } = useRequestSend(source)

const sendBtn = useTemplateRef<HTMLElement>('sendBtn')
useWholeWidth(sendBtn, () => [sendLabel.value, sendKey.value, source.loading])

const urlInputRef = useTemplateRef<HTMLInputElement>('urlInputRef')

onMounted(() => onBeforeUnmount(registerUrlField(() => urlInputRef.value?.focus())))

const urlDisplayRef = useTemplateRef<HTMLElement>('urlDisplayRef')

const urlParts = computed(() => urlPieces(source.url))

const unfilledPaths = computed(
  () => new Set(source.pathParams.filter((row) => row.value === '').map((row) => row.name))
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
  const reading = await source.pasteCommand(text)
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

// A box as wide as its text is a fraction of a pixel wide, and what is laid out after it stands on
// that fraction, which blurs a glyph. The box is held to the next whole pixel through
// `--whole-width`, which its stylesheet reads as a minimum; `redo` is whatever changes its text.
function useWholeWidth(target: MaybeRefOrGetter<HTMLElement | null | undefined>, redo: () => unknown) {
  function hold() {
    const box = toValue(target)
    if (!box) return
    box.style.setProperty('--whole-width', '0px')
    const natural = box.getBoundingClientRect().width
    box.style.setProperty('--whole-width', `${Math.ceil(natural - 0.01)}px`)
  }

  useResizeObserver(target, hold)
  watch(redo, () => void nextTick(hold))
}

function useMissingVars(source: RequestSource) {
  const environments = useEnvironmentsStore()
  const toast = useToast()

  const open = ref(false)
  const missing = computed(() => source.missing)
  const marked = computed(() => missing.value.length > 0)
  const rows = computed(() =>
    missing.value.map((name) => ({
      name,
      label: `{{${name}}}`,
      place: missingPlace(source, name, t),
    }))
  )

  watch(
    () => [environments.envState, source.environmentId],
    () => void source.refreshPreview()
  )

  const { stale, scopeId, scopeReadonly } = useRequestEnvironment(source)

  const title = computed(() => {
    if (stale.value) return t('request.pinGone')
    if (scopeId.value === null) return t('request.chooseEnvFirst')
    if (scopeReadonly.value) return t('request.envReadOnly')
    return undefined
  })

  const mayAdd = computed(() => scopeId.value !== null && !scopeReadonly.value && !stale.value)

  function add(names: string[]) {
    const envId = scopeId.value
    if (envId === null) return
    if (!scopeReadonly.value) {
      for (const name of names) {
        void environments.addVar(envId, { name }).catch((error) => {
          toast.show(t('request.createFailed', { error: describeFailure(error) }), 'error')
        })
      }
    }
    open.value = false
    environments.openSheet({ envId, varName: names[0] ?? '' })
  }

  return { open, missing, marked, rows, title, mayAdd, add }
}
</script>

<template>
  <div class="request-block">
    <div class="bar-row">
      <div class="url-field">
        <DropdownMenu>
          <DropdownMenuTrigger as-child>
            <button class="method-plate method-btn" :class="methodInkClass(source.method)">
              <span class="cap-line">{{ source.method }}</span>
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
            v-var-peek="source.environmentId"
            :value="source.url"
            class="url-input mono"
            :class="{ 'url-input-veiled': showUrlDisplay }"
            :placeholder="t('request.urlPlaceholder')"
            spellcheck="false"
            @input="source.setUrl(($event.target as HTMLInputElement).value); syncUrlScroll()"
            @keydown.enter.exact="send"
            @blur="source.flush()"
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
                :env-id="source.environmentId"
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

      <template v-if="!source.blank">
        <RequestEnvironment :source="source" />

        <CopyRequestMenu :source="source" />
      </template>

      <span v-if="!source.blank" class="save-anchor">
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
          :url="source.url"
          :default-name="saveDefaultName"
          :draft-id="source.draftId"
          @saved="source.markClean()"
        />
      </span>

      <button
        ref="sendBtn"
        type="button"
        class="send-btn"
        :class="{ sending: source.loading }"
        :disabled="sendDisabled"
        @click="onSend"
      >
        <Spinner v-if="source.loading" />
        <span>{{ sendLabel }}</span>
        <span class="send-hint">{{ sendKey }}</span>
      </button>
    </div>

    <RequestPane v-if="!source.blank" :source="source" />
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

.send-hint {
  @apply font-medium;
  font-size: var(--text-11);
  opacity: 0.7;
}

.var-mark {
  @apply flex-none inline-flex items-center justify-center w-6 h-6 rounded-[0.25rem] cursor-pointer
         border-0 bg-transparent;
  margin-left: 0.3rem;
  color: var(--red-text);
  --wails-draggable: no-drag;
}

.var-mark:hover,
.var-mark.open {
  background: var(--red-soft);
}

</style>