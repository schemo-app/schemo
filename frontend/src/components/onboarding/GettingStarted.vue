<script setup lang="ts">
import { ref } from 'vue'
import { TabKind } from '../../../bindings/schemo/internal/domain'
import { WELCOME_STEPS } from '../../lib/welcomeCards'
import { useGettingStarted } from '../../composables/useGettingStarted'
import { t } from '../../i18n'
import { useEnvironmentsStore } from '../../stores/environments'
import { useTabsStore } from '../../stores/tabs'
import { useTransferStore } from '../../stores/transfer'
import { useWorkspacesStore } from '../../stores/workspaces'
import Icon from '../ui/Icon.vue'
import GettingStartedCard from './GettingStartedCard.vue'

const tabs = useTabsStore()
const environments = useEnvironmentsStore()
const transfer = useTransferStore()
const workspaces = useWorkspacesStore()
const { walked, hidden } = useGettingStarted()

const expanded = ref(false)

function freshRequest(): void {
  if (tabs.activeKind !== TabKind.TabKindRequest) void tabs.newTab()
}

/** A row opens the place the step is walked; what the person does there is their own. A request has
 *  no place of its own yet, so those two rows lay out a fresh one. */
const places: Record<string, () => void> = {
  send: freshRequest,
  save: freshRequest,
  env: () => environments.openSheet(),
  import: () => transfer.askImport(),
  capture: () => void tabs.openCapture(),
  ws: () => workspaces.openCreate(),
}

function walk(key: string): void {
  expanded.value = false
  places[key]?.()
}

function overview(): void {
  expanded.value = false
  void tabs.openWelcome()
}
</script>

<template>
  <div v-if="!hidden" class="gs">
    <GettingStartedCard :open="expanded" @toggle="expanded = !expanded" />

    <div v-if="expanded" class="gs-list">
      <div class="gs-rows">
        <button
          v-for="step in WELCOME_STEPS"
          :key="step.key"
          type="button"
          class="gs-row"
          :class="{ done: walked(step.key) }"
          @click="walk(step.key)"
        >
          <span v-if="walked(step.key)" class="gs-tick">
            <Icon name="Check" :size="9" :stroke-width="3.6" />
          </span>
          <span v-else class="gs-ring"></span>
          <span class="gs-label">{{ t(step.label) }}</span>
        </button>
        <button type="button" class="gs-link" @click="overview()">
          {{ t('gettingStarted.link') }}
        </button>
      </div>

      <div class="gs-sep"></div>

      <GettingStartedCard open @toggle="expanded = false" />
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.gs {
  @apply flex-none relative;
  margin: 0 0.5rem 0.5rem;
}

.gs-list {
  @apply absolute inset-x-0 bottom-0 flex flex-col overflow-hidden;
  z-index: 15;
  max-height: 70vh;
  border-radius: 0.625rem;
  background: var(--bg);
  box-shadow: var(--popshadow);
}

.gs-rows {
  @apply flex-1 min-h-0 overflow-auto flex flex-col;
  gap: 1px;
  padding: 0.3125rem 0.25rem 0.125rem;
}

.gs-row {
  @apply flex items-center flex-none cursor-pointer;
  height: 1.75rem;
  padding: 0 0.5rem;
  gap: 0.5625rem;
  border: 0;
  border-radius: 0.3125rem;
  background: transparent;
  color: var(--text);
  font-family: inherit;
  font-size: inherit;
  text-align: left;
}

.gs-row:hover {
  background: var(--bg-hover);
}

.gs-row.done {
  color: var(--text-tertiary);
}

.gs-tick,
.gs-ring {
  @apply flex items-center justify-center flex-none rounded-full;
  width: 0.9375rem;
  height: 0.9375rem;
}

.gs-tick {
  background: var(--green);
  color: var(--accent-text);
}

.gs-ring {
  box-sizing: border-box;
  border: 1px solid var(--text-tertiary);
}

.gs-label {
  @apply flex-1 min-w-0 truncate;
}

.gs-link {
  @apply self-start cursor-pointer text-accent text-12;
  padding: 0.3125rem 0.5rem 0.5rem;
  border: 0;
  background: transparent;
  font-family: inherit;
  text-align: left;
}

.gs-sep {
  @apply flex-none;
  height: 1px;
  margin: 0 0.5rem;
  background: var(--border);
}
</style>
