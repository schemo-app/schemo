<script setup lang="ts">
import { ref } from 'vue'
import { TabKind } from '../../../bindings/schemo/internal/domain'
import { WELCOME_STEPS } from '../../lib/welcome'
import { useGettingStarted } from '../../composables/useGettingStarted'
import { useMessages } from '../../i18n'
import { useEnvironmentsStore } from '../../stores/environments'
import { useTabsStore } from '../../stores/tabs'
import { useTransferStore } from '../../stores/transfer'
import { useWorkspacesStore } from '../../stores/workspaces'
import Icon from '../ui/Icon.vue'

// The checklist under the tree: what is left to try, and how much of it is behind the person. What
// Go keeps is drawn here, and a press of the cross puts the card away for good — the overview is
// then the Help menu's to offer.
const { t } = useMessages()
const tabs = useTabsStore()
const environments = useEnvironmentsStore()
const transfer = useTransferStore()
const workspaces = useWorkspacesStore()
const { walked, count, percent, hidden, dismiss } = useGettingStarted()

const expanded = ref(false)

/** A row opens the place the step is walked; what the person does there is their own. A request has
 *  no place of its own yet, so those two rows lay out a fresh one. */
function walk(key: string): void {
  expanded.value = false
  if (key === 'send' || key === 'save') {
    if (tabs.activeKind !== TabKind.TabKindRequest) void tabs.newTab()
  } else if (key === 'env') {
    environments.openSheet()
  } else if (key === 'import') {
    transfer.askImport()
  } else if (key === 'capture') {
    void tabs.openCapture()
  } else if (key === 'ws') {
    workspaces.openCreate()
  }
}

function overview(): void {
  expanded.value = false
  void tabs.openWelcome()
}
</script>

<template>
  <div v-if="!hidden" class="gs">
    <div class="gs-card">
      <div class="gs-head" @click="expanded = !expanded">
        <span class="gs-title">{{ t('gettingStarted.title') }}</span>
        <span class="gs-count">
          {{ t('gettingStarted.of', { n: count, total: WELCOME_STEPS.length }) }}
        </span>
        <Icon
          name="ChevronDown"
          :size="10"
          :stroke-width="2.8"
          class="gs-chev"
          :class="{ up: !expanded }"
        />
        <button
          type="button"
          class="gs-x"
          :title="t('gettingStarted.hide')"
          @click.stop="dismiss()"
        >
          <Icon name="X" :size="9" :stroke-width="1.5" />
        </button>
      </div>
      <div class="gs-bar">
        <div class="gs-fill" :style="{ width: `${percent}%` }"></div>
      </div>
    </div>

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

      <div class="gs-card">
        <div class="gs-head" @click="expanded = false">
          <span class="gs-title">{{ t('gettingStarted.title') }}</span>
          <span class="gs-count">
            {{ t('gettingStarted.of', { n: count, total: WELCOME_STEPS.length }) }}
          </span>
          <Icon name="ChevronDown" :size="10" :stroke-width="2.8" class="gs-chev" />
          <button
            type="button"
            class="gs-x"
            :title="t('gettingStarted.hide')"
            @click.stop="dismiss()"
          >
            <Icon name="X" :size="9" :stroke-width="1.5" />
          </button>
        </div>
        <div class="gs-bar">
          <div class="gs-fill" :style="{ width: `${percent}%` }"></div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.gs {
  @apply flex-none relative;
  margin: 0 0.5rem 0.5rem;
}

.gs-card {
  @apply flex flex-col;
  border-radius: 0.625rem;
  background: var(--bg);
  box-shadow: inset 0 0 0 1px var(--border);
}

.gs-head {
  @apply flex items-center flex-none;
  height: 2.25rem;
  padding: 0 0.25rem 0 0.75rem;
  gap: 0.375rem;
}

.gs-title {
  @apply flex-1 min-w-0 truncate font-semibold;
}

.gs-count {
  @apply flex-none text-text-tertiary text-11-5;
}

.gs-chev {
  @apply flex-none text-text-tertiary;
  margin: 0 0.125rem;
  transition: transform 0.15s ease;
}

.gs-chev.up {
  transform: rotate(180deg);
}

.gs-x {
  @apply flex items-center justify-center flex-none text-text-tertiary cursor-pointer;
  width: 1.5rem;
  height: 1.5rem;
  border: 0;
  border-radius: 0.375rem;
  background: transparent;
}

.gs-x:hover {
  background: var(--bg-hover);
  color: var(--text);
}

.gs-bar {
  @apply flex-none;
  height: 0.1875rem;
  margin: 0 0.75rem 0.625rem;
  border-radius: 0.125rem;
  background: var(--chip);
}

.gs-fill {
  height: 100%;
  border-radius: 0.125rem;
  background: var(--accent);
  transition: width 0.3s ease;
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
  padding: 0.375rem 0.25rem 0.125rem;
}

.gs-row {
  @apply flex items-center flex-none cursor-pointer;
  height: 1.75rem;
  padding: 0 0.5rem;
  gap: 0.5625rem;
  border: 0;
  border-radius: 0.375rem;
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
  border: 1.5px solid var(--text-tertiary);
}

.gs-label {
  @apply flex-1 min-w-0 truncate;
}

.gs-link {
  @apply self-start cursor-pointer text-accent text-12;
  padding: 0.375rem 0.5rem 0.5rem;
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
