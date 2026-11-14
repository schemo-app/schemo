<script setup lang="ts">
import { computed, ref } from 'vue'
import type { RequestSource } from '../../lib/requestSource'
import { isBrokenJson, tryParseJson } from '../../lib/json'
import { t } from '../../i18n'
import Segment from '../ui/Segment.vue'
import EditorPane from './EditorPane.vue'

const { source } = defineProps<{ source: RequestSource }>()
const pane = ref<'query' | 'variables'>('query')
const variables = computed(() => source.options?.variables ?? '')
const parsed = computed(() => tryParseJson(variables.value))
const variableCount = computed(() => {
  const value = parsed.value.value
  return value && typeof value === 'object' && !Array.isArray(value) ? Object.keys(value).length : null
})
const choices = computed(() => [
  { value: 'query', label: t('request.protocol.query') },
  { value: 'variables', label: t('request.protocol.variables'), count: variableCount.value ?? 0 },
])
const editor = computed(() => pane.value === 'query'
  ? {
      language: 'graphql' as const, placeholder: 'query { }', label: t('request.protocol.query'),
      value: source.body, error: '', update: source.setBody,
    }
  : {
      language: 'json' as const, placeholder: '{ "id": 1 }', label: t('request.protocol.variables'),
      value: variables.value,
      error: isBrokenJson(variables.value) || (parsed.value.ok && variableCount.value === null) ? t('request.protocol.invalidVariables') : '',
      update: (value: string) => source.setOptions({ variables: value }),
    })
</script>

<template>
  <EditorPane
    :key="pane" :language="editor.language" :placeholder="editor.placeholder" :label="editor.label"
    :error="editor.error" :model-value="editor.value" @update:model-value="editor.update"
  >
    <template #tools>
      <Segment variant="toolbar" size="xs" :grow="false" :value="pane" :options="choices" @pick="pane = $event as typeof pane" />
    </template>
  </EditorPane>
</template>
