<script setup lang="ts">
import { computed, useTemplateRef } from 'vue'
import Tooltip from './Tooltip.vue'
import { useEnvironmentsStore } from '../../stores/environments'
import { hoveredVar } from '../../lib/varPeek'
import { usePlatform } from '../../composables/usePlatform'
import { t } from '../../i18n'

// A variable as an input shows it: its own text on a tinted ground, no bigger than the text. It is
// a picture and takes no pointer, so the field keeps every click and drag; which token the pointer
// rests on is `v-var-peek`'s to say, and its tooltip says what stands in the variable.
const { name, text, envId } = defineProps<{ name: string; text?: string; envId?: string }>()

const environments = useEnvironmentsStore()
const { isMac } = usePlatform()

const root = useTemplateRef<HTMLElement>('root')

const resolved = computed(() => environments.resolveVariable(name, envId))
const known = computed(() => resolved.value !== null)
const label = computed(() => text ?? `{{${name}}}`)

// A secret says that it is one and nothing else: no value of a secret is drawn anywhere.
const value = computed(() =>
  resolved.value?.kind === 'secret'
    ? t('varToken.secretHidden')
    : resolved.value?.value || t('varToken.empty')
)

const modifier = computed(() =>
  t('varToken.clickModifier', { modifier: isMac.value ? '⌥' : 'Alt' })
)

const open = computed(() => root.value !== null && hoveredVar.value === root.value)
</script>

<template>
  <Tooltip :open="open">
    <template #trigger>
      <span ref="root" class="var-token" :class="{ unknown: !known }" :data-var="name">{{ label }}</span>
    </template>

    <span class="var-tip">
      <span class="var-tip-value">{{ value }}</span>
      <span class="var-tip-meta">{{ t('varToken.hint', { modifier }) }}</span>
    </span>
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

.var-tip {
  @apply flex flex-col gap-0.5;
}

.var-tip-value {
  @apply text-12 break-all;
  font-family: var(--mono);
}

.var-tip-meta {
  @apply text-12;
  color: color-mix(in srgb, var(--tip-text) 60%, transparent);
}
</style>
