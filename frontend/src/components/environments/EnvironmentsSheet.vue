<script setup lang="ts">
import { computed, onMounted, ref, useTemplateRef, watch } from 'vue'
import EnvironmentCreatePane from './EnvironmentCreatePane.vue'
import EnvironmentDetailPane from './EnvironmentDetailPane.vue'
import EnvironmentDangerSheet from './EnvironmentDangerSheet.vue'
import EnvironmentList from './EnvironmentList.vue'
import ImportSheet from './ImportSheet.vue'
import TwoPaneSheet from '../ui/TwoPaneSheet.vue'
import { useEnvironmentsStore } from '../../stores/environments'
import { useRecordsStore } from '../../stores/records'
import { useTabsStore } from '../../stores/tabs'
import { useSheetNotice } from '../../composables/useSheetNotice'
import { parseTokens } from '../../lib/vars'
import { Environments } from '../../../bindings/schemo/internal/workspace'
import type { Entry } from '../../../bindings/schemo/internal/convert/dotenv'
import { describeFailure, t } from '../../i18n'

const emit = defineEmits<{ (e: 'close'): void }>()

const environments = useEnvironmentsStore()
const records = useRecordsStore()
const tabs = useTabsStore()
const { setNotice, clearNotice } = useSheetNotice()

const creating = ref(false)
const detailRef = useTemplateRef<InstanceType<typeof EnvironmentDetailPane>>('detailRef')

// The environment menu asks for the screen that makes one; the request rides on the store.
watch(
  () => environments.pendingCreate,
  (asked) => {
    if (!asked) return
    environments.pendingCreate = false
    creating.value = true
  },
  { immediate: true }
)

const envId = computed(() => environments.editedEnvId)
const env = computed(() => environments.environments.find((environment) => environment.id === envId.value) ?? null)
const isGlobals = computed(() => envId.value === null)

function select(id: string | null) {
  environments.editEnv(id)
  clearNotice()
}

const fileInput = useTemplateRef<HTMLInputElement>('fileInput')
const importSheet = ref(false)
const pendingEntries = ref<Entry[] | null>(null)
const pendingCount = computed(() => pendingEntries.value?.length ?? 0)

function pickFile() {
  fileInput.value?.click()
}

async function onFileChosen(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return

  const entries = (await Environments.ParseDotenv(await file.text())) ?? []
  importSheet.value = false

  if (entries.length === 0) {
    setNotice(t('environments.importEmpty'))
    return
  }

  if (!isGlobals.value && env.value?.readonly) {
    setNotice(t('environments.locked'))
    return
  }

  if (creating.value) {
    pendingEntries.value = entries
    return
  }

  void environments
    .importDotenv(isGlobals.value ? null : envId.value, entries)
    .then(() => setNotice(t('environments.imported', { n: entries.length })))
    .catch(refused)
}

function refused(error: unknown) {
  setNotice(describeFailure(error))
}

async function created() {
  creating.value = false
  const made = environments.editedEnvId
  if (pendingEntries.value && made) {
    const entries = pendingEntries.value
    pendingEntries.value = null
    try {
      await environments.importDotenv(made, entries)
    } catch (error) {
      refused(error)
    }
  }
}

function isNameReferenced(): boolean {
  const pane = tabs.pane
  const texts: string[] = [pane?.url ?? '', pane?.body ?? '']
  for (const param of pane?.params ?? []) texts.push(param.name, param.value)
  for (const header of pane?.headers ?? []) texts.push(header.name, header.value)
  for (const record of records.records) {
    texts.push(record.url)
    for (const header of record.requestHeaders ?? []) texts.push(header.name, header.value)
  }
  const names = new Set((env.value?.vars ?? []).map((variable) => variable.name))
  if (names.size === 0) return false
  return texts.some((text) => text && parseTokens(text).some((tok) => names.has(tok.name)))
}

const danger = ref<'delete' | 'clear' | null>(null)

async function confirmDanger() {
  const kind = danger.value
  danger.value = null

  try {
    await (kind === 'clear' ? clear() : remove())
  } catch (error) {
    refused(error)
  }
}

async function clear() {
  await environments.clearGlobals()
}

async function remove() {
  const gone = envId.value
  if (!gone) return
  await environments.removeEnv(gone)
  if (environments.editedEnvId === gone) environments.editEnv(environments.environments[0]?.id ?? null)
}

function cancelTop(): boolean {
  if (creating.value) {
    creating.value = false
    return true
  }
  return detailRef.value?.cancelTop() ?? false
}

onMounted(() => {
  clearNotice()
})
</script>

<template>
  <TwoPaneSheet :title="t('environments.title')" :cancel-top="cancelTop" @close="emit('close')">
    <template #default>
      <EnvironmentList v-if="!creating" @select="select" @create="creating = true" />

      <EnvironmentCreatePane
        v-if="creating"
        :pending-count
        @cancel="creating = false"
        @created="created"
        @import="importSheet = true"
      />
      <EnvironmentDetailPane
        v-else
        ref="detailRef"
        @import="importSheet = true"
        @danger="danger = isGlobals ? 'clear' : 'delete'"
      />
    </template>

    <template #over>
      <input
        ref="fileInput"
        type="file"
        accept=".env,text/plain"
        class="file-input"
        @change="onFileChosen"
      />

      <EnvironmentDangerSheet
        v-if="danger"
        :kind="danger"
        :referenced="danger === 'delete' && isNameReferenced()"
        @close="danger = null"
        @confirm="confirmDanger"
      />

      <ImportSheet
        v-if="importSheet"
        @close="importSheet = false"
        @choose="pickFile"
      />
    </template>
  </TwoPaneSheet>
</template>

<style scoped>
@reference "../../style.css";

.file-input {
  @apply hidden;
}
</style>
