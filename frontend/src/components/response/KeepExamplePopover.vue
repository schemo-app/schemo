<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef, watch } from 'vue'
import { PopoverRoot, PopoverTrigger } from 'reka-ui'
import DialogButton from '../ui/DialogButton.vue'
import Icon from '../ui/Icon.vue'
import IconButton from '../ui/IconButton.vue'
import Input from '../ui/Input.vue'
import PopoverContent from '../ui/PopoverContent.vue'
import StatusBadge from '../ui/StatusBadge.vue'
import type { RecordView } from '../../lib/requestRecord'
import type { PaneSource } from '../../stores/content'
import { exampleName, exampleOf, isKeepable } from '../../lib/examples'
import { useToast } from '../../composables/useToast'
import { formatBytes, t } from '../../i18n'

const { record, pane } = defineProps<{ record: RecordView; pane: PaneSource | null }>()

const toast = useToast()
const open = ref(false)
const name = ref('')
const nameField = useTemplateRef<{ focus(): void; select(): void }>('nameField')

// Only a saved request has somewhere to keep examples, and only a response with a status is worth it.
const canKeep = computed(() => Boolean(pane?.nodeId) && isKeepable(record))

watch(open, (opened) => {
  if (opened) name.value = exampleName(record.status)
})

function focusName() {
  void nextTick(() => {
    nameField.value?.focus()
    nameField.value?.select()
  })
}

function keep() {
  const kept = name.value.trim() || exampleName(record.status) || t('request.examples.untitled')
  pane?.saveExamples([...(pane.responses ?? []), exampleOf(record, kept)])
  open.value = false
  toast.show(t('request.examples.saved', { name: kept }))
}
</script>

<template>
  <PopoverRoot v-if="canKeep" v-model:open="open">
    <PopoverTrigger as-child>
      <IconButton variant="bare" size="bar" class="row-mark" :hint="t('request.examples.toExamples')">
        <Icon name="BookmarkPlus" :size="16" :stroke-width="1.8" />
      </IconButton>
    </PopoverTrigger>
    <PopoverContent class="ex-pop" align="end" @open-auto-focus.prevent="focusName">
      <div class="keep-head">
        <span class="keep-title">{{ t('request.examples.savePopTitle') }}</span>
        <StatusBadge :status="record.status">{{ record.status }}</StatusBadge>
        <span class="keep-meta">{{ formatBytes(record.responseBytes ?? 0) }}</span>
      </div>
      <label class="keep-field">
        <span>{{ t('request.examples.saveName') }}</span>
        <Input
          ref="nameField"
          v-model="name"
          :placeholder="t('request.examples.saveNamePlaceholder')"
          spellcheck="false"
          @keydown.enter.prevent="keep"
        />
      </label>
      <span class="keep-hint">{{ t('request.examples.savePopHint') }}</span>
      <div class="keep-actions">
        <DialogButton @click="open = false">{{ t('common.cancel') }}</DialogButton>
        <DialogButton primary @click="keep">{{ t('common.save') }}</DialogButton>
      </div>
    </PopoverContent>
  </PopoverRoot>
</template>

<style scoped>
@reference "../../style.css";

.keep-head {
  @apply flex items-center;
  gap: 0.5rem;
}

.keep-title {
  @apply flex-1 font-semibold;
  font-size: var(--text-13);
}

.keep-meta {
  @apply text-text-tertiary;
  font-size: var(--text-12);
}

.keep-field {
  @apply flex flex-col;
  gap: 0.3125rem;
}

.keep-field span {
  @apply text-text-secondary;
  font-size: var(--text-12);
}

.keep-hint {
  @apply text-text-tertiary;
  font-size: var(--text-12);
  line-height: 1.45;
  text-wrap: pretty;
}

.keep-actions {
  @apply flex justify-end;
  gap: 0.5rem;
}
</style>
