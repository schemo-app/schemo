<script setup lang="ts">
import { computed, onMounted, ref, useTemplateRef, watch } from 'vue'
import EnvironmentCreatePane from './EnvironmentCreatePane.vue'
import EnvironmentDetailPane from './EnvironmentDetailPane.vue'
import EnvironmentDangerSheet from './EnvironmentDangerSheet.vue'
import EnvironmentList from './EnvironmentList.vue'
import ImportSheet from './ImportSheet.vue'
import { TwoPaneSheet } from '../ui/sheet'
import { useEnvironmentsStore } from '../../stores/environments'
import { useRecordsStore } from '../../stores/records'
import { useTabsStore } from '../../stores/tabs'
import { useSheetNotice } from '../../composables/useSheetNotice'
import { parseTokens } from '../../lib/vars'
import { EnvironmentsService } from '../../../bindings/schemo/internal/transport/wails'
import type { Entry } from '../../../bindings/schemo/internal/dotenv'
import { describeFailure, useMessages } from '../../i18n'

const emit = defineEmits<{ (e: 'close'): void }>()

const envStore = useEnvironmentsStore()
const reqStore = useRecordsStore()
const tabs = useTabsStore()
const { setNotice, clearNotice } = useSheetNotice()
const { t } = useMessages()

const creating = ref(false)
const detailRef = useTemplateRef<InstanceType<typeof EnvironmentDetailPane>>('detailRef')

// The environment menu asks for the screen that makes one; the request rides on the store.
watch(
  () => envStore.pendingCreate,
  (asked) => {
    if (!asked) return
    envStore.pendingCreate = false
    creating.value = true
  },
  { immediate: true }
)

const envId = computed(() => envStore.editedEnvId)
const env = computed(() => envStore.environments.find((e) => e.id === envId.value) ?? null)
const isGlobals = computed(() => envId.value === null)

function select(id: string | null) {
  envStore.editEnv(id)
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

  const entries = (await EnvironmentsService.ParseDotenv(await file.text())) ?? []
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

  void envStore
    .importDotenv(isGlobals.value ? null : envId.value, entries)
    .then(() => setNotice(t('environments.imported', { n: entries.length })))
    .catch(refused)
}

function refused(error: unknown) {
  setNotice(describeFailure(error))
}

async function created() {
  creating.value = false
  const made = envStore.editedEnvId
  if (pendingEntries.value && made) {
    const entries = pendingEntries.value
    pendingEntries.value = null
    try {
      await envStore.importDotenv(made, entries)
    } catch (error) {
      refused(error)
    }
  }
}

function isNameReferenced(): boolean {
  const pane = tabs.pane
  const texts: string[] = [pane?.url ?? '', pane?.body ?? '']
  for (const p of pane?.params ?? []) texts.push(p.name, p.value)
  for (const h of pane?.headers ?? []) texts.push(h.name, h.value)
  for (const r of reqStore.records) {
    texts.push(r.url)
    for (const h of r.requestHeaders ?? []) texts.push(h.name, h.value)
  }
  const names = new Set((env.value?.vars ?? []).map((v) => v.name))
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
  await envStore.clearGlobals()
}

async function remove() {
  const gone = envId.value
  if (!gone) return
  await envStore.removeEnv(gone)
  if (envStore.editedEnvId === gone) envStore.editEnv(envStore.environments[0]?.id ?? null)
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
