<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useTemplateRef, watch } from 'vue'
import {
  DropdownMenuItem, DropdownMenuItemIndicator, DropdownMenuPortal, DropdownMenuRadioGroup,
  DropdownMenuRadioItem, DropdownMenuRoot, DropdownMenuSub, DropdownMenuSubContent,
  DropdownMenuSubTrigger, DropdownMenuTrigger,
} from 'reka-ui'
import CollectionVariableRows from './CollectionVariableRows.vue'
import VariablesTable from '../environments/VariablesTable.vue'
import Button from '../ui/Button.vue'
import Icon from '../ui/Icon.vue'
import ConfirmSheet from '../ui/ConfirmSheet.vue'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import { useCollectionsStore } from '../../stores/collections'
import { useEnvironmentsStore } from '../../stores/environments'
import { useWorkspacesStore } from '../../stores/workspaces'
import { useSheetNotice } from '../../composables/useSheetNotice'
import { useColorLabels } from '../../composables/useColorLabels'
import { DEFAULT_ENVIRONMENT_COLOR, dotOf, ENVIRONMENT_COLORS } from '../../lib/environmentPalette'
import { environmentOwnerOf, trailOf } from '../../lib/collectionTree'
import { Environments } from '../../../bindings/schemo/internal/workspace'
import { t } from '../../i18n'

const collections = useCollectionsStore()
const environments = useEnvironmentsStore()
const workspaces = useWorkspacesStore()
const { notice, clearNotice, withNotice } = useSheetNotice()
const colorLabels = useColorLabels('environments', ENVIRONMENT_COLORS)

const trail = computed(() => trailOf(collections.tree, collections.collectionId))
const level = computed(() => trail.value?.collection)
const owner = computed(() => environmentOwnerOf(trail.value, environments.envState?.collectionActive))
const canCreate = computed(() => workspaces.canWrite && !!level.value && owner.value?.id === level.value.id)
const owned = computed(() => environments.collectionEnvironments.filter(env => env.collectionId === level.value?.id))
const base = computed(() => owned.value.find(env => env.base && !env.folder))
const choices = computed(() => owned.value.filter(env => !env.base || env.folder))
const scope = ref('')
const focusName = ref('')
const selected = computed(() => scope.value ? choices.value.find(env => env.id === scope.value) : base.value)
const locked = computed(() => !!selected.value?.readonly || !workspaces.canWrite)

function editScope(id: string) {
  if (scope.value !== id) environments.forgetSecrets()
  scope.value = id
  focusName.value = ''
  clearNotice()
}

watch(() => level.value?.id, () => {
  environments.forgetSecrets()
  editScope('')
}, { flush: 'sync' })
watch(choices, list => {
  if (scope.value && !list.some(env => env.id === scope.value)) editScope('')
})
watch(() => environments.collectionFocus, focus => {
  if (!focus || focus.collectionId !== level.value?.id) return
  editScope(focus.envId === base.value?.id ? '' : focus.envId)
  focusName.value = focus.varName
  environments.collectionFocus = null
}, { immediate: true })
onBeforeUnmount(() => {
  environments.forgetSecrets()
  clearNotice()
})

function labelOf(env: NonNullable<typeof selected.value>) {
  return env.folder ? t('collections.folderSecrets') : env.name
}

const pickerLabel = computed(() => selected.value && !selected.value.base ? labelOf(selected.value) : t('request.envBase'))
const showPicker = computed(() => choices.value.length || canCreate.value)

const nameMode = ref<'new' | 'copy' | 'rename' | null>(null)
const nameInput = useTemplateRef<HTMLInputElement>('nameInput')
const name = ref('')
const saving = ref(false)

function nameEnvironment(mode: NonNullable<typeof nameMode.value>) {
  nameMode.value = mode
  name.value = mode === 'rename' ? selected.value?.name ?? '' : ''
  clearNotice()
  void nextTick(() => nameInput.value?.focus())
}

function cancelName() {
  if (saving.value) return
  nameMode.value = null
  clearNotice()
}

async function saveName() {
  const mode = nameMode.value
  const current = selected.value
  const collectionId = level.value?.id
  if (!mode || !collectionId || saving.value || !name.value.trim()) return
  if (mode !== 'new' && !current) return
  if (mode === 'rename' && (current?.base || current?.folder)) return

  saving.value = true
  try {
    const saved = await withNotice(async () => {
      if (mode === 'rename') {
        if (current) await environments.renameEnv(current.id, name.value)
        return
      }
      const id = await environments.createEnv({
        collectionId,
        name: name.value,
        color: mode === 'copy' ? current?.color : DEFAULT_ENVIRONMENT_COLOR,
        startFrom: mode === 'copy' ? current?.id : undefined,
      })
      editScope(id)
    })
    if (saved) nameMode.value = null
  } finally {
    saving.value = false
  }
}

function toggleAccess() {
  const env = selected.value
  if (env) void withNotice(() => environments.setEnvReadonly(env.id, !env.readonly))
}

function setColor(color: string) {
  const env = selected.value
  if (env) void withNotice(() => environments.setEnvColor(env.id, color))
}

const fileInput = useTemplateRef<HTMLInputElement>('fileInput')

async function importVariables(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  const env = selected.value
  input.value = ''
  if (!file || !env || locked.value) return

  await withNotice(async () => {
    const entries = await Environments.ParseDotenv(await file.text())
    if (selected.value?.id !== env.id || locked.value) return
    await environments.importDotenv(env.id, entries ?? [])
  })
}

const deleting = ref<string | null>(null)

async function deleteEnvironment() {
  const id = deleting.value
  deleting.value = null
  if (id && selected.value?.id === id) await withNotice(() => environments.removeEnv(id))
}
</script>

<template>
  <div v-if="level" class="vars">
    <div class="vars-toolbar">
      <form v-if="nameMode" class="name-form" @submit.prevent="saveName">
        <input
          ref="nameInput"
          v-model="name"
          class="kv-input"
          maxlength="40"
          :aria-label="t('environments.nameLabel')"
          :placeholder="t('environments.nameLabel')"
          @keydown.esc.prevent="cancelName"
        />
        <Button variant="primary" size="md" :disabled="saving || !name.trim()" type="submit">
          {{ t(nameMode === 'rename' ? 'common.save' : 'environments.createAction') }}
        </Button>
        <Button variant="quiet" :disabled="saving" @click="cancelName">
          {{ t('common.cancel') }}
        </Button>
      </form>

      <template v-else>
        <DropdownMenuRoot v-if="showPicker">
          <DropdownMenuTrigger as-child>
            <Button variant="quiet" class="vars-action" :aria-label="t('collections.editingEnvironment', { name: pickerLabel })">
              {{ pickerLabel }}
              <Icon name="ChevronDown" :size="12" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" class="vars-scope-menu">
            <DropdownMenuRadioGroup :model-value="scope">
              <DropdownMenuRadioItem class="menu-item" value="" @select="editScope('')">
                <span class="menu-dot"></span>
                <span class="menu-label">{{ t('request.envBase') }}</span>
                <DropdownMenuItemIndicator><Icon name="Check" :size="12" /></DropdownMenuItemIndicator>
              </DropdownMenuRadioItem>
              <DropdownMenuRadioItem
                v-for="env in choices"
                :key="env.id"
                :value="env.id"
                class="menu-item"
                @select="editScope(env.id)"
              >
                <span class="menu-dot" :style="{ background: dotOf(env.color ?? '') }"></span>
                <span class="menu-label">{{ labelOf(env) }}</span>
                <DropdownMenuItemIndicator><Icon name="Check" :size="12" /></DropdownMenuItemIndicator>
              </DropdownMenuRadioItem>
            </DropdownMenuRadioGroup>
            <DropdownMenuItem v-if="canCreate" class="menu-item" @select="nameEnvironment('new')">
              <Icon name="Plus" :size="14" />
              {{ t('environments.newMenu') }}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenuRoot>
        <span v-else class="vars-label">{{ pickerLabel }}</span>

        <DropdownMenuRoot v-if="selected && workspaces.canWrite">
          <DropdownMenuTrigger as-child>
            <Button variant="quiet" class="vars-action" :title="t('common.options')" :aria-label="t('common.options')">
              <Icon name="Ellipsis" :size="16" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent>
            <DropdownMenuItem v-if="!selected.base && !selected.folder" class="menu-item" @select="nameEnvironment('rename')">
              {{ t('collections.rename') }}
            </DropdownMenuItem>
            <DropdownMenuItem v-if="canCreate && !selected.folder" class="menu-item" @select="nameEnvironment('copy')">
              {{ t('collections.duplicate') }}
            </DropdownMenuItem>
            <DropdownMenuItem class="menu-item" @select="toggleAccess">
              {{ t(selected.readonly ? 'environments.accessEditable' : 'environments.accessReadonly') }}
            </DropdownMenuItem>
            <DropdownMenuItem v-if="!locked" class="menu-item" @select="fileInput?.click()">
              {{ t('environments.importEnv') }}
            </DropdownMenuItem>
            <DropdownMenuSub v-if="!selected.folder">
              <DropdownMenuSubTrigger class="menu-item">
                <span class="menu-label">{{ t('environments.colorLabel') }}</span>
                <Icon name="ChevronRight" :size="12" />
              </DropdownMenuSubTrigger>
              <DropdownMenuPortal>
                <DropdownMenuSubContent class="menu">
                  <DropdownMenuItem v-for="color in ENVIRONMENT_COLORS" :key="color" class="menu-item" @select="setColor(color)">
                    <span class="menu-dot" :style="{ background: dotOf(color) }"></span>
                    {{ colorLabels[color] }}
                  </DropdownMenuItem>
                </DropdownMenuSubContent>
              </DropdownMenuPortal>
            </DropdownMenuSub>
            <DropdownMenuItem class="menu-item danger" @select="deleting = selected.id">
              {{ t('environments.dangerDelete') }}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenuRoot>
      </template>
    </div>

    <input ref="fileInput" type="file" accept=".env,text/plain" hidden @change="importVariables" />

    <VariablesTable v-if="selected" :key="selected.id" :environment-id="selected.id" :focus-name="focusName" :locked>
      <template v-if="selected.base && level.variables?.length" #before-rows>
        <CollectionVariableRows :key="level.id" :level embedded />
      </template>
    </VariablesTable>
    <CollectionVariableRows v-else :key="level.id" :level />
    <p v-if="notice && !selected" class="notice">{{ notice }}</p>

    <ConfirmSheet
      v-if="deleting && selected?.id === deleting"
      :title="t('environments.dangerDelete')"
      :sub="t('environments.deleteSheetSubPlain')"
      :action="t('common.delete')"
      @close="deleting = null"
      @confirm="deleteEnvironment"
    />
  </div>
</template>

<style scoped>
@reference "../../style.css";

.vars {
  @apply flex flex-col flex-1 min-h-0 overflow-y-auto;
  --collection-variable-gutter: 0.625rem;
  --collection-variable-label-inset: 0.5rem;
  --collection-variable-control-column: 1.625rem;
  --collection-variable-columns: var(--collection-variable-control-column) minmax(0, 0.8fr) minmax(0, 1.4fr) repeat(3, 1.75rem);
  padding: 1rem 1.5rem;
}

.vars :deep(.collection-variable-add) {
  @apply inline-grid items-center border-0 bg-transparent text-left text-accent cursor-pointer;
  grid-template-columns: var(--collection-variable-control-column) auto;
  height: 2rem;
  margin-top: 0.25rem;
  padding: 0 var(--collection-variable-gutter);
  border-radius: 0.375rem;
  font-family: inherit;
  font-size: var(--text-13);
  font-weight: 500;
}

.vars :deep(.collection-variable-add span) {
  padding-inline-start: var(--collection-variable-label-inset);
}

.vars :deep(.collection-variable-add:hover:not(:disabled)) {
  background: var(--accent-soft);
}

.vars :deep(.collection-variable-add:disabled) {
  @apply opacity-50 cursor-default;
}

.vars-toolbar {
  @apply flex flex-none items-center justify-between gap-2;
  min-height: 1.875rem;
  margin-bottom: 0.5rem;
}

.vars-label {
  @apply text-13 font-semibold text-text-secondary;
  padding-left: 0.5rem;
}

.vars-toolbar .vars-action {
  gap: 0.375rem;
  height: 1.75rem;
  font-size: var(--text-13);
}

.menu-dot {
  @apply flex-none rounded-full;
  width: 0.4375rem;
  height: 0.4375rem;
}

.menu-label {
  @apply flex-1 min-w-0 truncate;
}

.vars-scope-menu {
  max-width: 18rem;
  max-height: min(20rem, var(--reka-dropdown-menu-content-available-height, 100vh));
  overflow-y: auto;
}

.name-form {
  @apply flex items-center gap-2;
}

.name-form input {
  width: min(14rem, 35vw);
}

.notice {
  @apply text-12 text-red;
  padding: 0.5rem;
}
</style>
