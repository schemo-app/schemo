<script setup lang="ts">
import { computed } from 'vue'
import Block from '../ui/Block.vue'
import type { Diagnosis, NodeView, Problem } from '../../../bindings/schemo/internal/domain'
import { isAboutRequest, pointedAt } from '../../lib/problemField'
import type { RequestSource } from '../../lib/requestSource'
import { focusBodyRange } from '../../composables/intents'
import { InspectService } from '../../../bindings/schemo/internal/transport/wails'
import { textOf, useMessages } from '../../i18n'

const { node, diagnosis, host } = defineProps<{
  node: NodeView
  diagnosis: Diagnosis | null
  host: RequestSource
}>()

const { t } = useMessages()

const complaint = computed(() => {
  const inside = (path: string) =>
    node.path === path || node.path.startsWith(path + '.') || node.path.startsWith(path + '[')
  return diagnosis?.problems?.find((p) => isAboutRequest(p) && p.path && inside(p.path)) ?? null
})

async function returnToField(problem: Problem) {
  if (problem.where === 'query') return host.setOpenChip('params')
  if (problem.where === 'header') return host.setOpenChip('headers')
  host.setOpenChip('body')
  const span = await InspectService.Locate('application/json', host.body, problem.field)
  if (span) focusBodyRange({ start: span.start, end: span.end })
}
</script>

<template>
  <Block v-if="complaint" :title="t('problems.title')">
    <p class="message">{{ complaint.message }}</p>
    <button type="button" class="where" @click="returnToField(complaint)">
      {{ textOf(`problems.where.${complaint.where}`) }} ·
      <span class="mono">{{ pointedAt(complaint) }}</span>
    </button>
  </Block>
</template>

<style scoped>
@reference "../../style.css";

.message {
  @apply text-13 text-text;
  line-height: 1.5;
}

.where {
  @apply self-start cursor-pointer rounded-md border-0 px-2 text-12 text-accent;
  background: var(--accent-soft);
  line-height: 1.375rem;
}

.mono {
  font-family: var(--mono);
}
</style>
