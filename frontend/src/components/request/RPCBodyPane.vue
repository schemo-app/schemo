<script setup lang="ts">
import type { RequestSource } from '../../lib/requestSource'
import { t } from '../../i18n'
import Button from '../ui/Button.vue'
import EditorPane from './EditorPane.vue'

const { source, input } = defineProps<{
  source: RequestSource
  input?: { input: string; example: string }
}>()
</script>

<template>
  <EditorPane
    language="json" placeholder="{ }" :label="t('request.chips.body')"
    :model-value="source.body" @update:model-value="source.setBody"
  >
    <template #tools>
      <span class="rpc-message truncate" :title="input?.input">JSON<span v-if="input"> · {{ input.input }}</span></span>
    </template>
    <template #actions>
      <Button size="field" :disabled="!input" @click="input && source.setBody(input.example)">
        {{ t('request.protocol.example') }}
      </Button>
    </template>
  </EditorPane>
</template>

<style scoped>
@reference "../../style.css";

.rpc-message {
  @apply font-mono text-12;
  color: var(--text-tertiary);
}
</style>
