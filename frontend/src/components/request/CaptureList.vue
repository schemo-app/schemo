<script setup lang="ts">
import Icon from '../ui/Icon.vue'
import { IconButton } from '../ui/button'
import { CaptureSource, VarScope, type Capture } from '../../../bindings/schemo/internal/domain'
import type { RequestSource } from '../../lib/requestSource'
import { useMessages } from '../../i18n'

// The rules that keep values of the response in variables each time the request is sent. They are
// made from the inspector, where the value is, and managed here, where the request is.
const { source } = defineProps<{ source: RequestSource }>()

const { t } = useMessages()

/** Where a rule reads, in the words a person would say it. */
function from(rule: Capture): string {
  switch (rule.source) {
    case CaptureSource.CaptureStatus:
      return t('captures.from.status')
    case CaptureSource.CaptureHeader:
      return t('captures.from.header', { name: rule.path ?? '' })
    case CaptureSource.CaptureCookie:
      return t('captures.from.cookie', { name: rule.path ?? '' })
  }
  return rule.path ?? ''
}

const placeOf = (rule: Capture): string =>
  rule.scope === VarScope.ScopeGlobals
    ? t('captures.scope.globals')
    : rule.scope === VarScope.ScopeRun
      ? t('captures.scope.run')
      : t('captures.scope.environment')

const write = (list: Capture[]) => source.saveCaptures(list)
const toggle = (rule: Capture) =>
  write(source.captures.map((one) => (one.id === rule.id ? { ...one, off: !one.off } : one)))
const remove = (rule: Capture) => write(source.captures.filter((one) => one.id !== rule.id))
</script>

<template>
  <div v-if="source.captures.length" class="captures">
    <span class="title">{{ t('captures.title') }}</span>
    <div v-for="rule in source.captures" :key="rule.id" class="rule" :class="{ off: rule.off }">
      <span class="name mono">{{ rule.name }}</span>
      <span class="from mono" :title="from(rule)">← {{ from(rule) }}</span>
      <span class="place">{{ placeOf(rule) }}</span>
      <Icon v-if="rule.secret" class="Lock" name="Lock" :size="12" :stroke-width="1.9" />
      <IconButton
        variant="bare"
        size="sm"
        :hint="rule.off ? t('captures.enable') : t('captures.disable')"
        @click="toggle(rule)"
      >
        <Icon :name="rule.off ? 'Play' : 'Pause'" :size="12" :stroke-width="1.8" />
      </IconButton>
      <IconButton variant="bare" size="sm" :hint="t('captures.remove')" @click="remove(rule)">
        <Icon name="X" :size="12" :stroke-width="2" />
      </IconButton>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.captures {
  @apply flex flex-col gap-1;
}

.title {
  @apply text-13 font-semibold;
}

.rule {
  @apply flex items-center gap-2 text-12;
  min-height: 1.5rem;
}

.rule.off .name,
.rule.off .from {
  @apply opacity-50;
}

.name {
  @apply flex-none font-semibold text-text;
  font-family: var(--mono);
}

.from {
  @apply min-w-0 flex-1 truncate text-text-secondary;
  font-family: var(--mono);
}

.place {
  @apply flex-none text-text-tertiary;
}

.lock {
  @apply flex-none text-text-tertiary;
}
</style>
