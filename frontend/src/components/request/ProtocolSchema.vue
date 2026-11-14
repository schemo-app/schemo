<script setup lang="ts">
import { computed, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { SchemaSourceKind, type SchemaSource } from '../../../bindings/schemo/internal/domain'
import { Drafts } from '../../../bindings/schemo/internal/request'
import type { ChipName, RequestSource } from '../../lib/requestSource'
import { schemaEditor, type EditorSchema } from '../../lib/schemaEditor'
import { pickOperation } from '../../lib/intents'
import { describeFailure, t } from '../../i18n'
import GraphQLPane from './GraphQLPane.vue'
import RPCBodyPane from './RPCBodyPane.vue'
import OperationPicker from './OperationPicker.vue'
import SchemaSourcesPane from './SchemaSourcesPane.vue'

const { source, section, methodSlot } = defineProps<{
  source: RequestSource
  section: ChipName | null
  methodSlot: HTMLElement | null
}>()

const editor = computed(() => schemaEditor(source.protocol, source))
const schema = shallowRef<EditorSchema | null>(null)
const sources = computed(() => source.options?.schemaSources ?? [])
const groups = computed(() => schema.value?.groups() ?? [])
const loading = ref(false)
const error = ref('')
const pickerOpen = ref(false)

let described: { editor: typeof editor.value; descriptors: string } | undefined
let pending: AbortController | undefined
let revision = 0

function stopRead() {
  revision++
  pending?.abort()
  pending = undefined
  loading.value = false
}

async function readSchema(read: (signal: AbortSignal) => Promise<EditorSchema>, updateSources: boolean) {
  stopRead()
  const request = new AbortController()
  pending = request
  error.value = ''
  loading.value = updateSources
  try {
    const result = await read(request.signal)
    if (request.signal.aborted) return
    schema.value = result
    if (updateSources) {
      described = { editor: editor.value, descriptors: result.descriptors }
      source.setOptions({ descriptors: result.descriptors, schemaSources: result.sources })
    }
  } catch (cause) {
    if (request.signal.aborted) return
    if (!updateSources) schema.value = null
    error.value = describeFailure(cause)
  } finally {
    if (pending === request) {
      pending = undefined
      loading.value = false
    }
  }
}

watch([editor, () => source.options?.descriptors ?? ''], ([current, descriptors]) => {
  if (described?.editor === current && described?.descriptors === descriptors) return
  described = { editor: current, descriptors }
  stopRead()
  schema.value = null
  error.value = ''
  if (current && descriptors) void readSchema(signal => current.describe(descriptors, signal), false)
}, { immediate: true })

onBeforeUnmount(stopRead)
onBeforeUnmount(pickOperation.on(() => { pickerOpen.value = true }).off)

async function load(next: SchemaSource[]) {
  const current = editor.value
  const draft = source.draftSnapshot()
  if (!current || !draft) return
  if (next.length) {
    await readSchema(signal => current.load(draft, next, signal), true)
    return
  }
  stopRead()
  error.value = ''
  schema.value = null
  described = { editor: current, descriptors: '' }
  source.setOptions({ descriptors: '', schemaSources: [] })
}

function addServer() {
  const kind = SchemaSourceKind.SchemaSourceServer
  void load([...sources.value.filter(item => item.kind !== kind), { kind }])
}

async function addPath(kind: SchemaSourceKind) {
  const title = t('request.protocol.importSchema')
  const protocol = source.protocol
  const previous = revision
  try {
    const path = kind === SchemaSourceKind.SchemaSourceFolder
      ? await Drafts.ChooseSchemaFolder(title)
      : await Drafts.ChooseSchemaFile(title, protocol)
    if (path && previous === revision) {
      await load([...sources.value.filter(item => item.path !== path), { kind, path }])
    }
  } catch (cause) {
    if (previous === revision) error.value = describeFailure(cause)
  }
}
</script>

<template>
  <template v-if="editor">
    <Teleport v-if="methodSlot" :to="methodSlot">
      <OperationPicker
        v-model:open="pickerOpen"
        :label="editor.picked(source)" :groups :loaded="schema?.loaded ?? false" :disabled="source.loading"
        :unfolded="editor.unfolded" :placeholder="t(editor.placeholder)" :search-label="t(editor.search)"
        @schema="source.setOpenChip('schema')"
      />
    </Teleport>

    <GraphQLPane v-if="section === 'body' && editor.body === 'graphql'" :source />
    <RPCBodyPane v-else-if="section === 'body'" :source :input="schema?.input" />
    <SchemaSourcesPane
      v-else-if="section === 'schema'" :sources :loading :error :warnings="schema?.warnings ?? []"
      :server-label="t(editor.server)" :hint="t(editor.hint)" :file-summary="editor.fileSummary"
      :can-load-server="!!source.url.trim()" :summary="schema?.loaded ? schema.summary() : ''"
      @change="load" @server="addServer" @path="addPath"
    />
  </template>
</template>
