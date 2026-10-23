<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Browser } from '@wailsio/runtime'
import IconButton from '../ui/IconButton.vue'
import Icon from '../ui/Icon.vue'
import Spinner from '../ui/Spinner.vue'
import { useAccount } from '../../composables/useAccount'
import { t } from '../../i18n'

// The sign-in in a 25rem modal: a code the server hands out, a browser that opens where
// it is confirmed, and a wait. There is no password anywhere.
const { server } = defineProps<{ server: string }>()

const emit = defineEmits<{
  (e: 'begin', server: string): void
  (e: 'cancel'): void
  (e: 'close'): void
}>()

// The dialog asks; the window answers: the window holds the account's dialogs, and a component that
// called them itself would be the second place deciding what a click means.
const { state, failure, setServer } = useAccount()

// What the modal is showing. It starts where the person is, and every step after that is decided by
// what Go says: the code is out, the code was confirmed, the code came to nothing.
const step = ref<'start' | 'waiting' | 'done' | 'refused'>('start')
const customOpen = ref(false)
const address = ref(server)
const applying = ref(false)

const challenge = computed(() => state.value?.challenge ?? null)
const code = computed(() => challenge.value?.userCode ?? '')

// The wait is drawn from the state, not the click: a sign-in that stopped puts the dialog back to
// where a person chooses, rather than leaving a spinner turning over nothing.
watch(
  () => state.value?.waiting,
  (waiting) => {
    step.value = waiting ? 'waiting' : 'start'
  }
)

watch(
  () => state.value?.signedIn,
  (signedIn) => {
    if (signedIn) step.value = 'done'
  }
)

watch(failure, (said) => {
  // A refusal while the app is not waiting is the answer to what was asked: the sign-in is over,
  // and it is over unhappy.
  if (said && !state.value?.waiting) step.value = 'refused'
})

function start() {
  step.value = 'start'
  emit('begin', address.value || server)
}

// The address is applied on its own: a person who typed it wants it kept, and the sign-in that
// follows is their next click.
async function apply() {
  applying.value = true
  try {
    await setServer(address.value)
  } finally {
    applying.value = false
  }
}

function openAgain() {
  if (challenge.value) void Browser.OpenURL(challenge.value.url)
}

function giveUp() {
  emit('cancel')
}
</script>

<template>
  <!-- The dialog is dismissed by its own buttons and by nothing else: a click that lands beside the
       card is a click that missed, and it must not throw away a code somebody is reading. -->
  <div class="modal-overlay">
    <div class="modal">
      <header class="modal-head">
        <span class="modal-title">{{ t('account.dialog.title') }}</span>
        <IconButton
          variant="subtle"
          size="xl"
          class="modal-close"
          :hint="t('common.close')"
          @click="emit('close')"
        >
          <Icon name="X" :size="14" :stroke-width="2.2" />
        </IconButton>
      </header>

      <div v-if="step === 'start'" class="modal-body start">
        <span class="mark">
          <Icon name="FileText" :size="24" :stroke-width="1.7" />
        </span>
        <div class="titles">
          <span class="title">{{ t('account.dialog.startTitle') }}</span>
          <span class="text">{{ t('account.dialog.startText') }}</span>
        </div>

        <button class="wide" @click="start">
          <Icon name="AppWindow" :size="14" :stroke-width="1.9" />
          <span class="cap-line">{{ t('account.dialog.browser') }}</span>
        </button>

        <div class="links">
          <button class="link" @click="emit('close')">{{ t('account.dialog.without') }}</button>
          <span class="dot">·</span>
          <button class="link" @click="customOpen = !customOpen">
            {{ t('account.dialog.custom') }}
            <Icon name="ChevronDown" :size="10" :stroke-width="2.6" />
          </button>
        </div>

        <div v-if="customOpen" class="server">
          <span class="server-label">{{ t('account.dialog.serverLabel') }}</span>
          <div class="server-row">
            <input v-model="address" class="server-field" spellcheck="false" />
            <button class="server-apply" :disabled="applying" @click="apply">
              {{ t('account.dialog.apply') }}
            </button>
          </div>
          <span class="server-hint">{{ t('account.dialog.serverHint') }}</span>
        </div>

        <p v-if="failure" class="failure">{{ failure }}</p>
      </div>

      <div v-else-if="step === 'waiting'" class="modal-body waiting">
        <Spinner class="text-accent" />
        <div class="titles">
          <span class="wait-title">{{ t('account.dialog.waitingTitle') }}</span>
          <span class="text">{{ t('account.dialog.waitingText') }}</span>
        </div>
        <!-- The code is here as well as in the browser: a person whose browser did not open can type it
             into the page themselves, and that is the whole reason the server prints a short one. -->
        <div v-if="code" class="device-code">
          <span class="device-code-label">{{ t('account.dialog.codeLabel') }}</span>
          <span class="device-code-value">{{ code }}</span>
        </div>
        <div class="links">
          <button class="link accent" @click="openAgain">{{ t('account.dialog.openAgain') }}</button>
          <button class="link" @click="giveUp">{{ t('account.dialog.cancel') }}</button>
        </div>
      </div>

      <div v-else class="modal-body start">
        <span class="mark" :class="{ bad: step === 'refused' }">
          <Icon :name="step === 'done' ? 'Check' : 'X'" :size="24" :stroke-width="1.9" />
        </span>
        <div class="titles">
          <span class="title">
            {{ step === 'done' ? t('account.dialog.doneTitle') : t('account.dialog.refusedTitle') }}
          </span>
          <span class="text">{{ step === 'done' ? t('account.dialog.doneText') : failure }}</span>
        </div>
        <button v-if="step === 'refused'" class="wide" @click="start">
          {{ t('account.dialog.retry') }}
        </button>
        <button v-else class="wide" @click="emit('close')">{{ t('common.close') }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.modal-overlay {
  @apply fixed inset-0 z-1600 flex items-center justify-center p-7;
  background: rgba(0, 0, 0, 0.22);
}

.modal {
  @apply flex flex-col w-100 max-w-[92vw] rounded-xl overflow-hidden;
  background: var(--glass-sheet);
  backdrop-filter: var(--blur-sheet);
  box-shadow: var(--glass-sheet-shadow), 0 0 0 1px var(--glass-overlay-border);
}

.modal-head {
  @apply relative flex-none h-10 flex items-center justify-center border-b border-border;
}

.modal-title {
  @apply text-13 font-semibold text-text-secondary;
}

.modal-head :deep(.modal-close) {
  position: absolute;
  right: 0.5rem;
}

.modal-body {
  @apply flex flex-col items-center gap-5 text-center;
}

.start {
  padding: 2rem 1.875rem 1.625rem;
}

.waiting {
  padding: 2.375rem 1.875rem 2rem;
}

.mark {
  @apply inline-flex items-center justify-center w-12 h-12 rounded-xl bg-accent text-accent-text;
}

.mark.bad {
  @apply bg-bg-hover text-text-secondary;
}

.titles {
  @apply flex flex-col items-center gap-1.5;
}

.title {
  @apply text-16 font-semibold tracking-[-0.01em];
}

.wait-title {
  @apply text-14 font-semibold;
}

.text {
  @apply text-13 text-text-secondary max-w-72.5;
  line-height: 1.5;
}

.wide {
  @apply w-full h-9 inline-flex items-center justify-center gap-2 rounded-lg border-0
         bg-accent text-accent-text text-13 font-semibold cursor-pointer;
  font-family: inherit;
}

.wide:hover {
  @apply brightness-110;
}

.links {
  @apply flex items-center gap-2.5 text-13;
}

.dot {
  @apply text-border;
}

.link {
  @apply inline-flex items-center gap-1 border-0 bg-transparent p-0.5 text-13
         text-text-tertiary cursor-pointer;
  font-family: inherit;
}

.link:hover {
  @apply text-text;
}

.link.accent {
  @apply text-13 font-medium text-accent;
}

.link.accent:hover {
  @apply underline;
}

.server {
  @apply w-full flex flex-col gap-1.5 border border-border rounded-xl bg-bg-sidebar
         px-3 py-2.5 text-left;
}

.server-label {
  @apply text-11 font-semibold tracking-[0.07em] uppercase text-text-tertiary;
}

.server-row {
  @apply flex gap-1.5;
}

.server-field {
  @apply flex-1 min-w-0 h-7.5 px-2.5 border border-border-strong rounded-[0.3rem] bg-bg-panel
         text-12 text-text-secondary;
  font-family: ui-monospace, 'SF Mono', 'Cascadia Code', Menlo, monospace;
}

.server-apply {
  @apply flex-none h-7.5 px-2.5 border border-border-strong rounded-[0.3rem] bg-bg-panel
         text-text text-13 cursor-pointer;
  font-family: inherit;
}

.server-apply:hover {
  @apply bg-bg-hover;
}

.server-hint {
  @apply text-11 text-text-tertiary;
  line-height: 1.45;
}

.waiting .text {
  max-width: 16.25rem;
}

.waiting .links {
  gap: 0.875rem;
}

.waiting .link {
  @apply text-13;
}

.device-code {
  @apply flex flex-col items-center;
  gap: 0.3rem;
}

.device-code-label {
  @apply text-11 font-semibold tracking-[0.09em] uppercase text-text-tertiary;
}

.device-code-value {
  @apply text-22 font-bold tracking-[0.05em];
  font-variant-numeric: tabular-nums;
}

.failure {
  @apply m-0 text-13 text-red;
}
</style>
