<script setup lang="ts">
import { computed, ref } from 'vue'
import { SchemaSourceKind, type SchemaSource } from '../../../bindings/schemo/internal/domain'
import { formatClock, t, type MessageKey } from '../../i18n'
import { fileName, metaLine } from '../../lib/format'
import { DropdownMenuRoot, DropdownMenuTrigger } from 'reka-ui'
import Button from '../ui/Button.vue'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import DropdownMenuItem from '../ui/DropdownMenuItem.vue'
import Icon, { type IconName } from '../ui/Icon.vue'
import IconButton from '../ui/IconButton.vue'
import Spinner from '../ui/Spinner.vue'

const { serverLabel, hint, fileSummary, sources, loading, error, warnings, canLoadServer, summary } = defineProps<{
  serverLabel: string
  hint: string
  fileSummary?: (count: number) => string
  sources: readonly SchemaSource[]
  loading: boolean
  error: string
  warnings: readonly string[]
  canLoadServer: boolean
  summary: string
}>()
const emit = defineEmits<{
  change: [sources: SchemaSource[]]
  server: []
  path: [kind: SchemaSourceKind]
}>()

const imports: { kind: SchemaSourceKind; label: MessageKey }[] = [
  { kind: SchemaSourceKind.SchemaSourceFile, label: 'request.protocol.chooseFile' },
  { kind: SchemaSourceKind.SchemaSourceFolder, label: 'request.protocol.chooseFolder' },
]
const unfolded = ref<Record<string, boolean>>({})
const hasServer = computed(() => sources.some(item => item.kind === SchemaSourceKind.SchemaSourceServer))

type SourceRow = {
  key: string
  icon: IconName
  name: string
  path: string
  meta: string
  nested: boolean
  folder: boolean
  open: boolean
  removeHint: string
  remove: () => void
}

const rows = computed<SourceRow[]>(() => sources.flatMap((item, index) => {
  const folder = item.kind === SchemaSourceKind.SchemaSourceFolder
  const open = folder && !!unfolded.value[item.path ?? '']
  const head: SourceRow = {
    key: `${item.kind}:${item.path ?? ''}`,
    icon: folder ? 'Folder' : item.kind === SchemaSourceKind.SchemaSourceFile ? 'FileText' : 'Globe',
    name: item.kind === SchemaSourceKind.SchemaSourceServer ? t('request.protocol.sourceServer')
      : folder ? `${fileName(item.path ?? '')}/` : fileName(item.path ?? ''),
    path: item.path ?? '',
    meta: metaLine(...describeSource(item), item.loadedAt ? formatClock(item.loadedAt) : ''),
    nested: false,
    folder,
    open,
    removeHint: t('request.protocol.removeSource'),
    remove: () => emit('change', sources.filter((_, i) => i !== index)),
  }
  if (!open) return [head]
  return [head, ...(item.files ?? []).map(file => ({
    key: `${head.key}/${file.name}`,
    icon: 'FileText' as const,
    name: file.name,
    path: file.name,
    meta: fileSummary?.(file.methods ?? 0) ?? '',
    nested: true,
    folder: false,
    open: false,
    removeHint: t('request.protocol.removeFile'),
    remove: () => skipFile(index, file.name),
  }))]
}))

function describeSource(item: SchemaSource) {
  switch (item.kind) {
    case SchemaSourceKind.SchemaSourceServer:
      return [serverLabel]
    case SchemaSourceKind.SchemaSourceFolder:
      return [t('request.protocol.sourceFolder'), t('request.protocol.filesCount', item.files?.length ?? 0)]
    default:
      return [t('request.protocol.sourceFile'), fileSummary?.(item.methods ?? 0) ?? '']
  }
}

// A file taken out of a folder stays out when the folder is read again; the last one takes the
// folder with it.
function skipFile(index: number, name: string) {
  const folder = sources[index]
  const left = (folder.files ?? []).filter(file => file.name !== name)
  emit('change', left.length
    ? sources.map((item, i) => i === index ? { ...item, skipped: [...(item.skipped ?? []), name] } : item)
    : sources.filter((_, i) => i !== index))
}

function toggle(row: SourceRow) {
  if (row.folder) unfolded.value = { ...unfolded.value, [row.path]: !row.open }
}
</script>

<template>
  <section v-if="!sources.length" class="schema-empty">
    <p>{{ hint }}</p>
    <div class="schema-actions">
      <Button size="field" :disabled="loading || !canLoadServer" @click="emit('server')">
        <Spinner v-if="loading" />
        {{ t('request.protocol.loadServer') }}
      </Button>
      <DropdownMenuRoot>
        <DropdownMenuTrigger as-child>
          <Button size="field" :disabled="loading">
            {{ t('request.protocol.importSchema') }}
            <Icon name="ChevronDown" :size="10" :stroke-width="2.6" class="text-text-tertiary" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          <DropdownMenuItem v-for="item in imports" :key="item.kind" @select="emit('path', item.kind)">{{ t(item.label) }}</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenuRoot>
    </div>
    <p v-if="error" class="schema-error" role="alert">{{ error }}</p>
  </section>

  <section v-else class="schema-pane">
    <div class="schema-head">
      <h4>{{ t('request.protocol.sources') }} <span>· {{ sources.length }}</span></h4>
      <span class="schema-meta">{{ summary }}</span>
      <!-- Reading every source again keeps a file edited on disk as current as the server. -->
      <IconButton
        variant="bare" size="bar" :title="t('request.protocol.reloadSources')"
        :aria-label="t('request.protocol.reloadSources')" :disabled="loading" @click="emit('change', [...sources])"
      >
        <Spinner v-if="loading" />
        <Icon v-else name="RefreshCw" :size="14" />
      </IconButton>
      <DropdownMenuRoot>
        <DropdownMenuTrigger as-child>
          <IconButton
            variant="bare" size="bar" :title="t('request.protocol.importSchema')"
            :aria-label="t('request.protocol.importSchema')" :disabled="loading"
          >
            <Icon name="FolderInput" :size="14" />
          </IconButton>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem v-for="item in imports" :key="item.kind" @select="emit('path', item.kind)">{{ t(item.label) }}</DropdownMenuItem>
          <DropdownMenuItem v-if="!hasServer" :disabled="!canLoadServer" @select="emit('server')">
            {{ t('request.protocol.loadServer') }}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenuRoot>
    </div>
    <ul class="schema-sources">
      <li
        v-for="row in rows" :key="row.key" class="schema-source"
        :class="{ nested: row.nested, folder: row.folder }" @click="toggle(row)"
      >
        <span class="schema-source-caret">
          <Icon v-if="row.folder" name="ChevronRight" :size="10" :stroke-width="2.8" :class="{ open: row.open }" />
        </span>
        <Icon :name="row.icon" :size="15" class="schema-source-icon" />
        <button
          v-if="row.folder" type="button" class="schema-source-name" :title="row.path" :aria-expanded="row.open"
          @click.stop="toggle(row)"
        >
          {{ row.name }}
        </button>
        <span v-else class="schema-source-name" :title="row.path || undefined">{{ row.name }}</span>
        <span class="schema-source-meta" :title="row.meta">{{ row.meta }}</span>
        <IconButton
          variant="danger" :title="row.removeHint" :aria-label="`${row.removeHint} ${row.name}`"
          :disabled="loading" @click.stop="row.remove()"
        >
          <Icon name="X" :size="12" :stroke-width="2.4" />
        </IconButton>
      </li>
    </ul>
    <p v-if="error" class="schema-error" role="alert">{{ error }}</p>
    <ul v-if="warnings.length" class="schema-warnings" :aria-label="t('request.protocol.unresolvedServices')">
      <li>{{ t('request.protocol.unresolvedServices') }}</li>
      <li v-for="warning in warnings" :key="warning" class="truncate" :title="warning">{{ warning }}</li>
    </ul>
  </section>
</template>

<style scoped>
@reference "../../style.css";

.schema-empty {
  @apply flex flex-col text-text-secondary;
  max-width: 40rem;
  min-height: 9.375rem;
  gap: 0.625rem;
  padding: 1rem 1.25rem;
}

.schema-actions {
  @apply flex flex-wrap items-center;
  gap: 0.5rem;
}

.schema-pane {
  @apply flex flex-col min-w-0;
  min-height: 8.125rem;
  padding: 0.5rem 1.25rem 0.75rem;
}

.schema-head {
  @apply flex items-center min-w-0;
  height: 2.125rem;
  gap: 0.25rem;
}

.schema-head h4 {
  @apply flex flex-1 items-center min-w-0 whitespace-nowrap text-12 text-text-secondary font-semibold;
  gap: 0.25rem;
}

.schema-head h4 span {
  @apply font-normal text-text-tertiary;
}

.schema-meta {
  @apply truncate text-12 text-text-tertiary;
  margin-right: 0.375rem;
}

.schema-source {
  @apply flex items-center min-w-0;
  height: 2rem;
  gap: 0.625rem;
  margin: 0 -0.625rem;
  padding: 0 0.375rem;
  border-radius: 0.4375rem;
}

.schema-source:hover {
  background: var(--bg-hover);
}

.schema-source.nested {
  padding-left: 1.625rem;
}

.schema-source-caret {
  @apply flex flex-none;
  width: 0.625rem;
  color: var(--text-tertiary);
}

.schema-source-caret .open {
  transform: rotate(90deg);
}

.schema-source-icon {
  @apply flex-none;
  color: var(--text-tertiary);
}

.schema-source-name {
  @apply flex-none whitespace-nowrap text-left text-text;
  max-width: 60%;
  overflow: hidden;
  text-overflow: ellipsis;
}

.schema-source-meta {
  @apply flex-1 min-w-0 truncate text-12 text-text-tertiary;
}

.schema-error {
  @apply text-13 break-words;
  color: var(--red-text);
  padding-top: 0.625rem;
}

.schema-warnings {
  @apply text-12 text-text-tertiary;
  padding-top: 0.5rem;
}
</style>
