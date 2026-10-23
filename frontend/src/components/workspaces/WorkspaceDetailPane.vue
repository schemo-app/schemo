<script setup lang="ts">
import { useColorLabels } from '../../composables/useColorLabels'
import { computed, useTemplateRef, watch } from 'vue'
import { Code } from '../../../bindings/schemo/internal/domain'
import Button from '../ui/Button.vue'
import ColorSwatches from '../ui/ColorSwatches.vue'
import Segment from '../ui/Segment.vue'
import Tooltip from '../ui/Tooltip.vue'
import WorkspaceMembers from '../cloud/WorkspaceMembers.vue'
import { useWorkspaceTeam } from '../../composables/useWorkspaceTeam'
import { WORKSPACE_COLORS, WORKSPACE_TINTS } from '../../lib/workspacePalette'
import { useWorkspacesStore } from '../../stores/workspaces'
import { useWorkspaceCounts } from '../../composables/useWorkspaceCounts'
import { useNameField } from '../../composables/useNameField'
import { useSheetNotice } from '../../composables/useSheetNotice'
import { usePlan } from '../../composables/usePlan'
import { FEATURES, featureRefusal } from '../../lib/onboardingFeatures'
import { roleLabel } from '../../lib/roles'
import { describeFailure, t, te } from '../../i18n'

const emit = defineEmits<{ (e: 'danger'): void; (e: 'leave'): void }>()

const workspaces = useWorkspacesStore()
const { contentsOf } = useWorkspaceCounts()
const { notice, setNotice, clearNotice } = useSheetNotice()
const { includes, refreshPlan } = usePlan()

const workspace = computed(() => workspaces.edited)
const personal = computed(() => workspace.value?.id === workspaces.personalId)

// What the account says about this person's place here: a role of its own is what turns the fields
// into values, because a name the server would refuse is a name this window has no business offering.
const mayEdit = computed(() => workspace.value?.access.update ?? true)
const mayEnd = computed(() => workspace.value?.access.delete ?? true)
const mayWrite = computed(() => workspace.value?.access.write ?? true)
const role = computed(() => roleLabel(t, te, workspace.value?.access.roleName ?? ''))

const colorLabels = useColorLabels('workspaces', WORKSPACE_COLORS)

const kinds = computed(() => [
  { value: 'personal', label: t('workspaces.kindPersonal') },
  { value: 'team', label: t('workspaces.kindTeam'), soon: !includes(FEATURES.sharedWorkspaces) },
])

const kind = computed(() => workspace.value?.kind ?? 'personal')

const nameInput = useTemplateRef<HTMLInputElement>('nameInput')

const { name, invalid, show, commit, revert } = useNameField({
  input: nameInput,
  target: () => workspace.value,
  save: (id, wanted) => workspaces.update(id, { name: wanted }),
})

watch([() => workspace.value?.id, () => workspace.value?.name], () => show(workspace.value?.name ?? ''), {
  immediate: true,
})

function pickColor(color: string) {
  const target = workspace.value
  if (!target) return
  void write(() => workspaces.update(target.id, { color }))
}

function pickKind(value: string) {
  const target = workspace.value
  if (!target || value === kind.value) return
  void write(() => workspaces.update(target.id, { kind: value }))
}

async function write(send: () => Promise<unknown>) {
  try {
    await send()
    clearNotice()
  } catch (error) {
    setNotice(failureText(error))
  }
}

function failureText(error: unknown): string {
  const failure = (error as { cause?: { code?: Code; args?: Record<string, string> } })?.cause
  if (failure?.code === Code.CodeFeatureRequired) {
    void refreshPlan()
    return featureRefusal(t, te, failure.args?.feature ?? '')
  }
  return describeFailure(error)
}

const team = useWorkspaceTeam(workspace, kind)
const membersLabel = team.label

const contents = computed(() =>
  workspace.value ? contentsOf(workspace.value) : []
)

const last = computed(() => personal.value)

function cancelTop(): boolean {
  if (!invalid.value) return false
  revert()
  return true
}

defineExpose({ cancelTop })
</script>

<template>
  <div class="pane">
    <div v-if="workspace" class="pane-body">
      <!-- A role the account gave that may read and not write: what this person builds here stays on
           this device, because the round will not send it and the server would refuse it. -->
      <p v-if="!mayWrite" class="read-only">{{ t('workspaces.readOnlyNote') }}</p>

      <div class="pair">

        <div v-if="personal || !mayEdit" class="field">
          <span class="field-label">{{ t('workspaces.nameLabel') }}</span>
          <span class="as-value">{{ personal ? t('workspaces.personal') : workspace.name }}</span>
        </div>
        <label v-else class="field">
          <span class="field-label">{{ t('workspaces.nameLabel') }}</span>
          <input
            ref="nameInput"
            v-model="name"
            class="input"
            :class="{ invalid }"
            maxlength="40"
            spellcheck="false"
            @keydown.enter="commit"
            @keydown.escape.prevent="revert"
            @blur="commit"
          />
        </label>

        <div class="field tight">
          <span class="field-label">{{ t('workspaces.colorLabel') }}</span>
          <ColorSwatches
            :value="workspace.color"
            :colors="WORKSPACE_COLORS"
            :tints="WORKSPACE_TINTS"
            :labels="colorLabels"
            ground="var(--glass-sheet)"
            :disabled="!mayEdit"
            @pick="pickColor"
          />
        </div>
      </div>

      <div class="field">
        <span class="field-label">{{ t('workspaces.kindLabel') }}</span>
        <span v-if="personal || !mayEdit" class="as-value">
          {{ kind === 'team' ? t('workspaces.kindTeam') : t('workspaces.kindPersonal') }}
        </span>
        <Segment
          v-else
          class="kinds"
          :options="kinds"
          :value="kind"
          :soon-hint="t('workspaces.notInPlan')"
          @pick="pickKind($event)"
        />
      </div>

      <div v-if="workspace?.access.roleName" class="field">
        <span class="field-label">{{ t('workspaces.roleLabel') }}</span>
        <span class="as-value">{{ role }}</span>
      </div>

      <div v-if="kind === 'team'" class="field">
        <span class="field-label">{{ membersLabel }}</span>

        <WorkspaceMembers :state="team" />
      </div>

      <div class="field">
        <span class="field-label">{{ t('workspaces.contentsLabel') }}</span>
        <div class="contents">
          <div v-for="entry in contents" :key="entry.label" class="count-card">
            <span class="count-value">{{ entry.value }}</span>
            <span class="count-label">{{ entry.label }}</span>
          </div>
        </div>
      </div>
    </div>

    <div class="foot">

      <span class="danger-note" :class="{ bad: !!notice }">{{ notice || t('workspaces.dangerNote') }}</span>

      <Tooltip v-if="last">
        <template #trigger>
          <span class="danger-off">{{ t('workspaces.removeAction') }}</span>
        </template>
        {{ t('workspaces.personalHint') }}
      </Tooltip>
      <Button v-else-if="mayEnd" variant="danger" size="field" @click="emit('danger')">
        {{ t('workspaces.removeAction') }}
      </Button>
      <!-- A member who may not end the workspace still has to be able to get out of it, and stepping
           out is the whole of what they may do here. -->
      <Button v-else variant="danger" size="field" @click="emit('leave')">
        {{ t('workspaces.leaveAction') }}
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
  padding: 1.125rem 1.25rem;
}

.read-only {
  @apply m-0 rounded-lg text-13;
  padding: 0.5625rem 0.6875rem;
  line-height: 1.5;
  color: var(--warn-band-ink);
  background: var(--warn-band);
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

.as-value {
  @apply flex items-center h-8.5 text-13 text-text-secondary;
}

.input {
  @apply w-full h-8.5 px-2.5 rounded-lg text-13 text-text outline-none;
  font-family: inherit;
  background: var(--bg-panel);
  border: 1px solid var(--border-strong);
}

.input:focus {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.input.invalid,
.input.invalid:focus {
  border-color: color-mix(in srgb, var(--red) 45%, transparent);
}

.input.invalid:focus {
  box-shadow: 0 0 0 3px var(--red-soft);
}

.kinds {
  max-width: 16.25rem;
}

.contents {
  @apply grid gap-2.5;
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.count-card {
  @apply border border-border rounded-xl p-3 flex flex-col gap-1;
}

.count-value {
  @apply text-19 font-semibold;
  letter-spacing: -0.01em;
}

.count-label {
  @apply text-13 text-text-tertiary;
}

.foot {
  @apply flex-none flex items-center h-12 px-5 gap-3;
  border-top: 1px solid var(--border);
}

.danger-note {
  @apply flex-1 min-w-0 text-13 text-text-tertiary;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.danger-note.bad {
  color: var(--red-text);
}

.danger-off {
  @apply inline-flex items-center flex-none h-7.5 px-3 rounded-[0.3rem] text-13 font-medium;
  font-family: inherit;
  color: var(--red-text);
  background: transparent;
  border: 1px solid var(--red-soft);
  opacity: 0.5;
  cursor: default;
}
</style>
