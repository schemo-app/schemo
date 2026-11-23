<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef, watch } from 'vue'
import { DropdownMenuRoot, DropdownMenuTrigger } from 'reka-ui'
import Icon from '../ui/Icon.vue'
import IconButton from '../ui/IconButton.vue'
import ManagerHeader from '../ui/ManagerHeader.vue'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import DropdownMenuItem from '../ui/DropdownMenuItem.vue'
import DropdownMenuSeparator from '../ui/DropdownMenuSeparator.vue'
import DropdownMenuSubmenu from '../ui/DropdownMenuSubmenu.vue'
import VariablesTable from './VariablesTable.vue'
import { ENVIRONMENT_COLORS, dotOf } from '../../lib/environmentPalette'
import { useColorLabels } from '../../composables/useColorLabels'
import { useEnvironmentsStore } from '../../stores/environments'
import { useWorkspacesStore } from '../../stores/workspaces'
import { useNameField } from '../../composables/useNameField'
import { useSheetNotice } from '../../composables/useSheetNotice'
import { t } from '../../i18n'

const emit = defineEmits<{ (e: 'merge', into: string): void; (e: 'delete'): void; (e: 'close'): void }>()

const environments = useEnvironmentsStore()
const workspaces = useWorkspacesStore()
const { withNotice } = useSheetNotice()
const colorLabels = useColorLabels('environments', ENVIRONMENT_COLORS)

const env = computed(() => environments.envOf(environments.editedEnvId))
const isGlobals = computed(() => environments.editedEnvId === null)
const readonly = computed(() => Boolean(env.value?.readonly))
const locked = computed(() => readonly.value || !workspaces.canWrite)
const isActive = computed(() => env.value !== null && env.value.id === environments.activeId)
const mergeTargets = computed(() =>
  environments.environments.filter((one) => one.id !== env.value?.id && !one.readonly)
)

const textMode = ref(false)

const nameInput = useTemplateRef<HTMLInputElement>('nameInput')
const editingName = ref(false)
const { name, invalid, show, commit, revert } = useNameField({
  input: nameInput,
  target: () => env.value,
  save: (id, wanted) => environments.updateEnv(id, { name: wanted }),
})

watch(
  () => [env.value?.id, env.value?.name],
  () => show(env.value?.name ?? ''),
  { immediate: true }
)

watch(() => environments.editedEnvId, () => {
  editingName.value = false
})

// A new environment opens with its name being edited.
watch(
  () => environments.renaming,
  (id) => {
    if (id && id === env.value?.id) {
      environments.renaming = null
      rename()
    }
  },
  { immediate: true }
)

function rename() {
  if (locked.value || !env.value) return
  editingName.value = true
  void nextTick(() => nameInput.value?.select())
}

async function finishRename() {
  await commit()
  if (!invalid.value) editingName.value = false
}

function cancelRename() {
  revert()
  editingName.value = false
}

function use() {
  if (env.value) void withNotice(() => environments.setActive(env.value!.id))
}

function pickColor(color: string) {
  if (env.value) void withNotice(() => environments.updateEnv(env.value!.id, { color }))
}

function toggleReadonly() {
  if (env.value) void withNotice(() => environments.updateEnv(env.value!.id, { readonly: !readonly.value }))
}

function duplicate() {
  if (env.value) void withNotice(() => environments.duplicateEnv(env.value!.id))
}

const importFile = () => void withNotice(() => environments.importFile(environments.editedEnvId))
const exportFile = () => void withNotice(() => environments.exportFile(environments.editedEnvId))

const table = useTemplateRef<InstanceType<typeof VariablesTable>>('table')

function cancelTop(): boolean {
  if (editingName.value) {
    cancelRename()
    return true
  }
  return table.value?.cancelTop() ?? false
}

defineExpose({ cancelTop })
</script>

<template>
  <div class="pane">
    <ManagerHeader class="pane-head" @close="emit('close')">
      <template v-if="isGlobals">
        <span class="title">{{ t('environments.globals') }}</span>
      </template>
      <template v-else-if="env">
        <span class="dot" :style="{ background: dotOf(env.color ?? '') }"></span>
        <input
          v-if="editingName"
          ref="nameInput"
          v-model="name"
          class="name-input"
          :class="{ invalid }"
          maxlength="40"
          spellcheck="false"
          @keydown.enter.prevent="finishRename"
          @keydown.esc.stop.prevent="cancelRename"
          @blur="finishRename"
        />
        <span v-else class="title" @dblclick="rename">{{ env.name }}</span>
      </template>

      <template #actions>
        <span v-if="readonly" class="badge" :title="t('environments.readonlyHint')">
          <Icon name="Lock" :size="11" />{{ t('environments.readonly') }}
        </span>
        <span v-if="isActive" class="in-use"><Icon name="Check" :size="12" :stroke-width="2.4" />{{ t('environments.inUse') }}</span>
        <button v-else-if="env" type="button" class="use" :title="t('environments.useHint')" @click="use">
          {{ t('environments.use') }}
        </button>

        <IconButton
          size="lg"
          :class="{ on: textMode }"
          :hint="t('environments.textMode')"
          :aria-label="t('environments.textMode')"
          :aria-pressed="textMode"
          @click="textMode = !textMode"
        >
          <Icon name="AlignLeft" :size="14" />
        </IconButton>

        <DropdownMenuRoot>
          <DropdownMenuTrigger as-child>
            <IconButton size="lg" :hint="t('common.options')" :aria-label="t('common.options')"><Icon name="Ellipsis" :size="14" /></IconButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <template v-if="env">
              <DropdownMenuItem :disabled="locked" @select="rename">{{ t('environments.rename') }}</DropdownMenuItem>
              <DropdownMenuSubmenu :label="t('environments.colorLabel')" :disabled="!workspaces.canWrite">
                <template #indicator>
                  <span class="dot" :style="{ background: dotOf(env.color ?? '') }"></span>
                </template>
                <DropdownMenuItem v-for="color in ENVIRONMENT_COLORS" :key="color" @select="pickColor(color)">
                  <span class="dot" :style="{ background: dotOf(color) }"></span>
                  <span class="cap-line">{{ colorLabels[color] }}</span>
                </DropdownMenuItem>
              </DropdownMenuSubmenu>
              <DropdownMenuItem :disabled="!workspaces.canWrite" @select="duplicate">{{ t('environments.duplicate') }}</DropdownMenuItem>
              <DropdownMenuSubmenu v-if="!locked && mergeTargets.length" :label="t('environments.mergeWith')">
                <DropdownMenuItem v-for="target in mergeTargets" :key="target.id" @select="emit('merge', target.id)">
                  <span class="dot" :style="{ background: dotOf(target.color ?? '') }"></span>
                  <span class="cap-line">{{ target.name }}</span>
                </DropdownMenuItem>
              </DropdownMenuSubmenu>
              <DropdownMenuSeparator />
            </template>
            <DropdownMenuItem v-if="!locked" @select="importFile">{{ t('environments.importFile') }}</DropdownMenuItem>
            <DropdownMenuItem @select="exportFile">{{ t('environments.exportFile') }}</DropdownMenuItem>
            <template v-if="env && workspaces.canWrite">
              <DropdownMenuItem @select="toggleReadonly">
                {{ readonly ? t('environments.allowChanges') : t('environments.readonly') }}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem class="danger" @select="emit('delete')">{{ t('environments.deleteEnv') }}</DropdownMenuItem>
            </template>
          </DropdownMenuContent>
        </DropdownMenuRoot>
      </template>
    </ManagerHeader>

    <div class="pane-body">
      <VariablesTable ref="table" :env-id="environments.editedEnvId" :locked :text-mode />
    </div>

    <div class="foot">
      <span>{{ readonly ? t('environments.readonlyNote') : t('environments.secretsNote') }}</span>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.pane {
  @apply flex-1 min-w-0 flex flex-col min-h-0;
}

.title {
  @apply min-w-0 overflow-hidden text-ellipsis whitespace-nowrap font-semibold text-14;
}

.dot {
  @apply flex-none rounded-full;
  width: 0.4375rem;
  height: 0.4375rem;
  margin-right: 0.25rem;
}

.name-input {
  @apply min-w-0 h-7 px-2 rounded-md text-13 font-semibold text-text outline-none;
  width: 14rem;
  font-family: inherit;
  background: var(--bg-inset);
  border: 1px solid var(--accent);
}

.name-input.invalid {
  border-color: color-mix(in srgb, var(--red) 45%, transparent);
  box-shadow: 0 0 0 3px var(--red-soft);
}

.badge,
.in-use {
  @apply flex items-center gap-1.5 text-12 text-text-tertiary;
  padding: 0 0.25rem;
}

.in-use {
  @apply text-text-secondary;
  margin-right: 0.25rem;
}

.use {
  @apply flex-none h-7 px-3 rounded-[0.4375rem] cursor-pointer border-0 text-text text-12;
  margin-right: 0.25rem;
  font-family: inherit;
  background: var(--chip);
}

.use:hover {
  @apply bg-bg-hover;
}

.pane-head :deep(.on) {
  @apply bg-bg-hover text-text;
}

.pane-body {
  @apply flex-1 min-h-0 overflow-y-auto;
  padding: 12px 24px 24px;
}

.foot {
  @apply flex-none flex items-center gap-3 text-12 text-text-tertiary;
  min-height: 44px;
  padding: 8px 24px;
  border-top: 1px solid var(--border);
}

.danger {
  @apply text-red;
}
</style>
