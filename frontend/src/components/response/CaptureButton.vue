<script setup lang="ts">
import { computed, ref } from 'vue'
import Icon from '../ui/Icon.vue'
import PopoverContent from '../ui/PopoverContent.vue'
import { PopoverRoot as Popover, PopoverTrigger } from 'reka-ui'
import SaveToVariable from '../inspector/SaveToVariable.vue'
import { CaptureSource } from '../../../bindings/schemo/internal/domain'
import { capturedAt, looksSecret, variableNameFor } from '../../lib/capturePolicy'
import type { RequestSource } from '../../lib/requestSource'
import { t } from '../../i18n'

// The button at the end of a row of the response that keeps what the row says in a variable. It is
// quiet until the row is hovered, and it stays lit for a row that is kept already.
const { host, source, path, value } = defineProps<{
  host: RequestSource
  source: CaptureSource
  /** The header's or the cookie's name: where in the response the value is. */
  path: string
  value: string
}>()

const open = ref(false)

const captured = computed(() => capturedAt(host.captures, source, path))

const stop = () => host.saveCaptures(host.captures.filter((rule) => rule.id !== captured.value?.id))

const heading = computed(() =>
  t(source === CaptureSource.CaptureCookie ? 'inspector.save.fromCookie' : 'inspector.save.fromHeader', {
    name: path,
  })
)
</script>

<template>
  <Popover v-model:open="open">
    <PopoverTrigger as-child>
      <button
        type="button"
        class="capture-btn"
        :class="{ kept: captured }"
        :title="captured ? t('inspector.captured', { name: captured.name }) : t('captures.keep')"
        :aria-label="t('captures.keep')"
        @click.stop
      >
        <Icon name="Database" :size="14" :stroke-width="1.7" />
      </button>
    </PopoverTrigger>
    <PopoverContent class="capture-pop" align="end" @click.stop>
      <div v-if="captured" class="kept-note">
        <span>{{ t('inspector.captured', { name: captured.name }) }}</span>
        <button type="button" class="stop" @click="stop">{{ t('inspector.stopCapturing') }}</button>
      </div>
      <SaveToVariable
        v-else
        :suggested="variableNameFor(path)"
        :secret-by-default="looksSecret(path)"
        :source
        :path
        :host
        :heading
        :value="async () => value"
        @done="open = false"
        @cancel="open = false"
      />
    </PopoverContent>
  </Popover>
</template>

<style scoped>
@reference "../../style.css";

.capture-btn {
  @apply flex flex-none items-center justify-center cursor-pointer border-0 bg-transparent text-text-tertiary;
  width: 1.625rem;
  height: 1.625rem;
  border-radius: 0.3rem;
  opacity: 0;
  transition: opacity 0.12s ease, background 0.12s ease, color 0.12s ease;
  --wails-draggable: no-drag;
}

.capture-btn[data-state='open'],
.capture-btn.kept {
  opacity: 1;
}

.capture-btn:hover {
  background: var(--bg-hover);
  color: var(--text);
}

.capture-btn.kept {
  @apply text-accent;
}

.kept-note {
  @apply flex items-center justify-between gap-3 text-13;
}

.stop {
  @apply cursor-pointer border-0 bg-transparent text-12 text-text-tertiary;
}
</style>
