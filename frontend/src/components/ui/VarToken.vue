<script setup lang="ts">
import { computed, useTemplateRef } from 'vue'
import Tooltip from './Tooltip.vue'
import { useEnvironmentsStore } from '../../stores/environments'
import { useCollectionsStore } from '../../stores/collections'
import { useVarContext } from '../../composables/useVarContext'
import { findCollection } from '../../lib/collectionTree'
import { hoveredVar } from '../../lib/varPeek'
import { usePlatform } from '../../composables/usePlatform'
import { t } from '../../i18n'

// Takes no pointer, so the field keeps every click; `v-var-peek` handles the tooltip and alt-click.
const { name, text } = defineProps<{ name: string; text?: string }>()

const environments = useEnvironmentsStore()
const collections = useCollectionsStore()
const context = useVarContext()
const { isMac } = usePlatform()

const root = useTemplateRef<HTMLElement>('root')

const resolved = computed(() => environments.resolveVariable(name, context.value))
const label = computed(() => text ?? `{{${name}}}`)

// A secret's value is never drawn.
const value = computed(() => {
  if (!resolved.value) return t('varToken.unknown')
  if (resolved.value.kind === 'secret') return t('varToken.secretHidden')
  return resolved.value.value || t('varToken.empty')
})

// "Environment Local · Catalog API": which set answered.
const where = computed(() => {
  const source = resolved.value?.source
  if (!source) return ''
  const env = environments.activeEnvironment?.name ?? ''
  const collection = findCollection(collections.tree, context.value.collection)?.name ?? ''
  switch (source) {
    case 'global':
      return t('varToken.fromGlobals')
    case 'collection':
      return t('varToken.fromCollection', { collection })
    case 'env':
      return t('varToken.fromEnv', { env })
    case 'block':
      return t('varToken.fromBlock', { env, collection })
  }
  return ''
})

const modifier = computed(() => t('varToken.clickModifier', { modifier: isMac.value ? '⌥' : 'Alt' }))

const open = computed(() => root.value !== null && hoveredVar.value === root.value)
</script>

<template>
  <Tooltip :open="open">
    <template #trigger>
      <span
        ref="root"
        class="var-token"
        :class="{ unknown: !resolved }"
        :data-var="name"
        :data-source="resolved?.source ?? ''"
        :data-collection="context.collection"
      >{{ label }}</span>
    </template>

    <span class="var-tip">
      <span class="var-tip-value">{{ value }}</span>
      <span v-if="where" class="var-tip-meta">{{ where }}</span>
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
