<script setup lang="ts">
import { computed, inject, useTemplateRef } from 'vue'
import Tooltip from './Tooltip.vue'
import { useEnvironmentsStore } from '../../stores/environments'
import { hoveredVar } from '../../lib/varPeek'
import { requestVariablePreview } from '../../lib/vars'
import { t } from '../../i18n'

// Takes no pointer, so the field keeps every click; `v-var-peek` handles the tooltip.
const { name, text, envId } = defineProps<{ name: string; text?: string; envId?: string }>()

const environments = useEnvironmentsStore()

const root = useTemplateRef<HTMLElement>('root')

const requestPreview = inject(requestVariablePreview, null)
const resolved = computed(() => requestPreview ? requestPreview.value[name] ?? null : environments.resolveVariable(name, envId))
const known = computed(() => resolved.value !== null)
const environmentId = computed(() => {
  const value = resolved.value
  return value && 'environmentId' in value ? value.environmentId : undefined
})
const label = computed(() => text ?? `{{${name}}}`)

// A secret says that it is one and nothing else: no value of a secret is drawn anywhere.
const value = computed(() =>
  resolved.value?.kind === 'secret'
    ? t('varToken.secretHidden')
    : resolved.value?.value || t('varToken.empty')
)

const open = computed(() => root.value !== null && hoveredVar.value === root.value)
</script>

<template>
  <Tooltip :open="open">
    <template #trigger>
      <span ref="root" class="var-token" :class="{ unknown: !known }" :data-var="name"
        :data-var-source="resolved?.source" :data-var-environment="environmentId">{{ label }}</span>
    </template>

    <span v-if="known" class="var-tip-value">{{ value }}</span>
    <span v-else>{{ t('varToken.missing') }}</span>
  </Tooltip>
</template>

<style scoped>
@reference "../../style.css";

.var-token {
  border-radius: 0.25rem;
  background: var(--accent-soft);
  color: var(--accent);
}

.var-token.unknown {
  background: var(--red-soft);
  color: var(--red);
}

.var-tip-value {
  display: block;
  max-width: min(22rem, calc(100vw - 2.5rem));
  overflow-wrap: anywhere;
  font-family: var(--mono);
}
</style>
