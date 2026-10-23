<script setup lang="ts">
import { computed, ref } from 'vue'
import { DialogContent, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import { useSyncStore } from '../../stores/sync'
import { t } from '../../i18n'

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'confirm', wipe: boolean): void
}>()

const sync = useSyncStore()

const wipe = ref(false)

const options = computed(() => [
  { wipe: false, title: t('account.signOutSheetKeep'), note: t('account.signOutSheetKeepNote') },
  { wipe: true, title: t('account.signOutSheetWipe'), note: t('account.signOutSheetWipeNote') },
])

// The one line about what is still on this device, worded by which half of the choice is picked.
const pendingNote = computed(() => {
  if (!sync.waitingCount) return ''
  return wipe.value
    ? t('account.signOutSheetWipePending', { n: sync.waitingCount })
    : t('account.signOutSheetKeepPending', { n: sync.waitingCount })
})

function onOpenChange(value: boolean) {
  if (!value) emit('close')
}
</script>

<template>
  <DialogRoot :open="true" @update:open="onOpenChange">
    <DialogPortal>
      <!-- The dim starts under the titlebar, so the window keeps its own name lit. The card is centred by
      the flex box and not by a transform: a translated card lands on a half pixel whenever its height is
      odd, and the compositor resamples the whole thing, text included. -->
      <DialogOverlay class="out-overlay">
        <DialogContent class="out-card">
          <DialogTitle class="out-title">{{ t('account.signOut') }}</DialogTitle>

          <div class="out-options">
            <button
              v-for="option in options"
              :key="option.wipe ? 'wipe' : 'keep'"
              type="button"
              class="out-option"
              :class="{ picked: wipe === option.wipe }"
              @click="wipe = option.wipe"
            >
              <span class="out-radio"></span>
              <span class="out-lines">
                <span class="out-option-title">{{ option.title }}</span>
                <span class="out-option-note">{{ option.note }}</span>
              </span>
            </button>
          </div>

          <p v-if="pendingNote" class="out-pending">{{ pendingNote }}</p>

          <div class="out-foot">
            <button type="button" class="out-cancel" @click="emit('close')">
              {{ t('common.cancel') }}
            </button>
            <button type="button" class="out-confirm" @click="emit('confirm', wipe)">
              {{ wipe ? t('account.signOutSheetDelete') : t('account.signOutSheetAction') }}
            </button>
          </div>
        </DialogContent>
      </DialogOverlay>
    </DialogPortal>
  </DialogRoot>
</template>

<style scoped>
@reference "../../style.css";

.out-overlay {
  @apply fixed z-1500 flex items-center justify-center;
  inset: 3.125rem 0 0 0;
  padding: 2rem;
  background: rgba(20, 22, 30, 0.28);
}

.out-card {
  @apply flex flex-col rounded-xl overflow-y-auto;
  width: 100%;
  max-width: 28.75rem;
  max-height: 100%;
  padding: 1.25rem;
  gap: 0.875rem;
  background: var(--bg-panel);
  box-shadow:
    0 0 0 1px var(--border),
    0 24px 60px rgba(20, 24, 40, 0.28);
}

.out-title {
  @apply text-16 font-semibold;
}

.out-options {
  @apply flex flex-col gap-2;
}

.out-option {
  @apply flex items-start gap-2.5 text-left cursor-pointer rounded-[0.5625rem];
  padding: 0.6875rem 0.75rem;
  font-family: inherit;
  background: var(--bg-panel);
  border: 1px solid var(--border);
}

.out-option.picked {
  background: var(--sel-card);
  border-color: var(--accent);
}

.out-radio {
  @apply flex-none rounded-full;
  width: 1rem;
  height: 1rem;
  margin-top: 0.0625rem;
  box-sizing: border-box;
  border: 1.5px solid var(--border-strong);
  background: var(--bg-panel);
}

.out-option.picked .out-radio {
  border: 5px solid var(--accent);
}

.out-lines {
  @apply flex flex-col gap-0.75;
}

.out-option-title {
  @apply text-14 text-text;
}

.out-option-note {
  @apply text-12 text-text-tertiary;
  line-height: 1.45;
}

.out-pending {
  @apply m-0 text-13 rounded-lg;
  padding: 0.5625rem 0.6875rem;
  line-height: 1.5;
  color: var(--warn-band-ink);
  background: var(--warn-band);
}

.out-foot {
  @apply flex justify-end gap-2;
  padding-top: 0.125rem;
}

.out-cancel {
  @apply h-7.5 px-3 rounded-[0.4375rem] text-13 cursor-pointer text-text;
  font-family: inherit;
  background: var(--bg-panel);
  border: 1px solid var(--border-strong);
}

.out-cancel:hover {
  background: var(--bg-hover);
}

.out-confirm {
  @apply h-7.5 px-3.5 rounded-[0.4375rem] text-13 font-medium cursor-pointer;
  font-family: inherit;
  color: #fff;
  background: var(--red-text);
  border: 1px solid var(--red-text);
}

:root.dark .out-confirm {
  color: var(--bg-panel);
}

.out-confirm:hover {
  filter: brightness(1.06);
}
</style>
