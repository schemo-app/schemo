<script setup lang="ts">
import { useColorLabels } from '../../composables/useColorLabels'
import { computed, useTemplateRef, watch } from 'vue'
import { Button } from '../ui/button'
import ColorSwatches from '../ui/ColorSwatches.vue'
import Segment from '../ui/Segment.vue'
import VariablesTable from './VariablesTable.vue'
import { ENVIRONMENT_COLORS, ENVIRONMENT_TINTS } from './palette'
import { useEnvironmentsStore } from '../../stores/environments'
import { useWorkspacesStore } from '../../stores/workspaces'
import { useNameField } from '../../composables/useNameField'
import { useMessages } from '../../i18n'

const emit = defineEmits<{ (e: 'import'): void; (e: 'danger'): void }>()

const envStore = useEnvironmentsStore()
const workspaces = useWorkspacesStore()
const { t } = useMessages()

const colorLabels = useColorLabels('environments', ENVIRONMENT_COLORS)

const env = computed(() => envStore.environments.find((e) => e.id === envStore.editedEnvId) ?? null)
const isGlobals = computed(() => envStore.editedEnvId === null)

const nameInput = useTemplateRef<HTMLInputElement>('nameInput')
const { name, invalid, show, commit, revert } = useNameField({
  input: nameInput,
  target: () => env.value,
  save: (id, wanted) => envStore.renameEnv(id, wanted),
})

watch(
  [() => env.value?.id, () => env.value?.name, isGlobals],
  () => show(isGlobals.value ? t('environments.globals') : (env.value?.name ?? '')),
  { immediate: true }
)

const accessOptions = computed(() => [
  { value: 'editable', label: t('environments.accessEditable') },
  { value: 'readonly', label: t('environments.accessReadonly') },
])

const access = computed(() => (env.value?.readonly ? 'readonly' : 'editable'))

function pickAccess(value: string) {
  const target = env.value
  if (!target) return
  void envStore.setEnvReadonly(target.id, value === 'readonly')
}

function pickColor(color: string) {
  const target = env.value
  if (!target) return
  void envStore.setEnvColor(target.id, color)
}

const dangerNote = computed(() =>
  isGlobals.value ? t('environments.dangerGlobalsNote') : t('environments.dangerDeleteNote')
)

const dangerLabel = computed(() =>
  isGlobals.value ? t('environments.dangerClear') : t('environments.dangerDelete')
)

const table = useTemplateRef<InstanceType<typeof VariablesTable>>('table')

function cancelTop(): boolean {
  return table.value?.cancelTop() ?? false
}

defineExpose({ cancelTop })
</script>

<template>
  <div class="pane">
    <div class="pane-body">
      <div class="pair">
        <label class="field">
          <span class="field-label">{{ t('environments.nameLabel') }}</span>
          <input
            ref="nameInput"
            v-model="name"
            class="input"
            :class="{ invalid }"
            :readonly="isGlobals || !workspaces.canWrite"
            maxlength="40"
            spellcheck="false"
            @keydown.enter="commit"
            @keydown.esc.prevent="revert"
            @blur="commit"
          />
        </label>

        <div v-if="env && workspaces.canWrite" class="field tight">
          <span class="field-label">{{ t('environments.colorLabel') }}</span>
          <ColorSwatches
            :value="env.color ?? ''"
            :colors="ENVIRONMENT_COLORS"
            :tints="ENVIRONMENT_TINTS"
            :labels="colorLabels"
            @pick="pickColor"
          />
        </div>
      </div>

      <div v-if="env && workspaces.canWrite" class="field">
        <span class="field-label">{{ t('environments.accessLabel') }}</span>
        <Segment class="access" :options="accessOptions" :value="access" @pick="pickAccess" />
      </div>

      <VariablesTable
        ref="table"
        :locked="Boolean(env?.readonly) || !workspaces.canWrite"
        @import="emit('import')"
      />
    </div>

    <div class="foot">
      <span class="danger-note">{{ dangerNote }}</span>
      <button
        v-if="env"
        type="button"
        class="use"
        :disabled="env.id === envStore.activeId"
        @click="envStore.setActive(env.id)"
      >
        {{ t('environments.useIt') }}
      </button>
      <Button
        variant="danger"
        size="field"
        :disabled="!workspaces.canWrite || (isGlobals && envStore.globals.length === 0)"
        @click="emit('danger')"
      >
        {{ dangerLabel }}
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

.pair {
  @apply flex gap-3.5 items-end;
}

.field {
  @apply flex flex-col gap-1.5 min-w-0;
}

.pair > .field {
  @apply flex-1;
}

.pair > .field.tight {
  @apply flex-none;
}

.field-label {
  @apply section-label;
}

.input {
  @apply w-full h-8.5 px-2.5 rounded-lg text-13 text-text outline-none;
  font-family: inherit;
  background: var(--bg-inset);
  border: 1px solid var(--border-strong);
}

.input:focus,
.input.invalid {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.input.invalid {
  border-color: color-mix(in srgb, var(--red) 45%, transparent);
  box-shadow: 0 0 0 3px var(--red-soft);
}

.input[readonly] {
  @apply text-text-tertiary cursor-default;
  background: var(--bg-sidebar);
}

.access {
  max-width: 16.25rem;
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

.use {
  @apply flex-none h-7.5 px-3 rounded-[0.4375rem] cursor-pointer border-0 text-text text-13;
  font-family: inherit;
  background: var(--chip);
}

.use:hover:not(:disabled) {
  @apply bg-bg-hover;
}

.use:disabled {
  @apply opacity-40 cursor-default;
}
</style>
