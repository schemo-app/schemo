<script setup lang="ts">
import { computed } from 'vue'
import Block from '../ui/Block.vue'
import type { Diagnosis, NodeView, Problem } from '../../../bindings/schemo/internal/domain'
import type { RequestSource } from '../../lib/requestSource'
import { focusBodyRange } from '../../lib/intents'
import { Inspector } from '../../../bindings/schemo/internal/request'
import { textOf, t } from '../../i18n'

/** `/items/0/name` as `items[0].name`, which is how a person writes a member. */
function fieldLabel(field: string): string {
  return field
    .split('/')
    .filter(Boolean)
    .map((part) => part.replace(/~1/g, '/').replace(/~0/g, '~'))
    .reduce((out, part) => (/^\d+$/.test(part) ? `${out}[${part}]` : out ? `${out}.${part}` : part), '')
}

/** What a problem points at in the request, as it reads: a member of the body, or a name. */
const pointedAt = (problem: Problem): string =>
  problem.where === 'body' ? fieldLabel(problem.field) : problem.field

/** Whether the problem says where in the request the fault is. */
const isAboutRequest = (problem: Problem): boolean => !!problem.where && !!problem.field


const { node, diagnosis, host } = defineProps<{
  node: NodeView
  diagnosis: Diagnosis | null
  host: RequestSource
}>()


const complaint = computed(() => {
  const inside = (path: string) =>
    node.path === path || node.path.startsWith(path + '.') || node.path.startsWith(path + '[')
  return diagnosis?.problems?.find((problem) => isAboutRequest(problem) && problem.path && inside(problem.path)) ?? null
})

async function returnToField(problem: Problem) {
  if (problem.where === 'query') return host.setOpenChip('params')
  if (problem.where === 'header') return host.setOpenChip('headers')
  host.setOpenChip('body')
  const span = await Inspector.Locate('application/json', host.body, problem.field)
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
