<script setup lang="ts">
import { computed } from 'vue'
import Button from '../ui/Button.vue'
import Segment from '../ui/Segment.vue'
import CookieTable from './CookieTable.vue'
import { useCookiesStore } from '../../stores/cookies'
import { useWorkspacesStore } from '../../stores/workspaces'
import { reachLabel } from '../../lib/responseCookies'
import { formatCheckedAt, t } from '../../i18n'

// One domain of the jar: what it is called, whether responses may write to it, and the cookies it
// holds — and, at the foot, the one thing that takes it all away again.
const emit = defineEmits<{ (e: 'danger'): void }>()

const cookies = useCookiesStore()
const workspaces = useWorkspacesStore()

// A domain asked for by the response's tab may hold nothing yet, and is still the one on screen.
const host = computed(() => reachLabel(cookies.current ?? { domain: cookies.domain, cookies: [] }))

const writes = computed(() => [
  { value: 'allowed', label: t('cookies.writesAllowed') },
  { value: 'blocked', label: t('cookies.writesBlocked') },
])

function pickWrites(value: string) {
  void cookies.setBlocked(value === 'blocked')
}
</script>

<template>
  <div class="pane">
    <div class="pane-body">
      <div class="field">
        <span class="field-label">{{ t('cookies.domain') }}</span>
        <span class="as-value mono" :title="t('cookies.reachHint')">{{ host }}</span>
      </div>

      <div class="field">
        <span class="field-label">{{ t('cookies.writesLabel') }}</span>
        <Segment
          class="writes"
          :options="writes"
          :value="cookies.blocked ? 'blocked' : 'allowed'"
          :disabled="!workspaces.canWrite"
          @pick="pickWrites"
        />
        <span class="field-hint">
          {{ cookies.blocked ? t('cookies.blockedHint') : t('cookies.savingHint') }}
        </span>
      </div>

      <div class="section">
        <div class="section-head">
          <span class="section-label">{{ t('cookies.listLabel') }}</span>
          <span class="section-count mono">{{ cookies.rows.length }}</span>
          <span class="head-spacer"></span>
          <span v-if="cookies.current?.writtenAt" class="section-hint">
            {{ t('cookies.writtenAt', { at: formatCheckedAt(cookies.current.writtenAt) }) }}
          </span>
        </div>

        <CookieTable />
      </div>
    </div>

    <div class="foot">
      <span class="danger-note">{{ t('cookies.deleteDomainNote') }}</span>
      <Button
        variant="danger"
        size="field"
        :disabled="!cookies.current || !workspaces.canWrite"
        @click="emit('danger')"
      >
        {{ t('cookies.deleteDomain') }}
      </Button>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.pane {
  @apply flex-1 min-w-0 flex flex-col min-h-0;
}

.pane-body {
  @apply flex-1 min-h-0 overflow-y-auto flex flex-col gap-4.5;
  padding: 0.5rem 1.25rem 1rem;
}

.field {
  @apply flex flex-col gap-1.5 min-w-0;
}

.field-label {
  @apply section-label;
}

.field-hint {
  @apply text-12 text-text-tertiary;
}

.as-value {
  @apply flex items-center h-8.5 text-13 text-text;
  font-family: var(--mono);
}

.writes {
  max-width: 16.25rem;
}

.section {
  @apply flex flex-col gap-2;
}

.section-head {
  @apply flex items-center gap-2.5;
}

.section-label {
  @apply section-label;
}

.section-count {
  @apply text-11 text-text-tertiary;
}

.section-hint {
  @apply text-12 text-text-tertiary;
}

.foot {
  gap: 0.5rem;
  @apply flex-none flex items-center px-4 pl-5;
  height: 3.25rem;
  border-top: 1px solid var(--border);
}

.danger-note {
  @apply flex-1 min-w-0 text-12 text-text-tertiary;
}
</style>
