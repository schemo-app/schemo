import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { useTimeoutFn } from '@vueuse/core'
import {
  SearchKind,
  SearchTarget,
  type SearchGroup,
  type SearchHit,
} from '../../bindings/schemo/internal/domain'
import { SearchService, SystemService } from '../../bindings/schemo/internal/transport/wails'
import type { IconName } from '../components/ui/Icon.vue'
import { kindLabel } from '../lib/searchKinds'
import { callOrToast } from './calls'
import { t } from '../i18n'
import { useCollectionsStore } from './collections'
import { useEnvironmentsStore } from './environments'
import { useRecordsStore } from './records'
import { useTransferStore } from './transfer'

const QUERY_PAUSE_MS = 120

export interface Command {
  id: string
  label: string
  icon: IconName
}

export interface PaletteRow {
  hit: SearchHit | null
  command: Command | null
}

export interface Section {
  kind: SearchKind
  label: string
  total: number
  rows: PaletteRow[]
}

export const useSearchStore = defineStore('search', () => {
  const open = ref(false)
  const text = ref('')
  const kind = ref<SearchKind | null>(null)
  const groups = ref<SearchGroup[]>([])
  const activeIndex = ref(0)

  // A leading '>' switches the palette to commands.
  const query = computed(() => text.value.replace(/^\s*>/, '').trim())
  const commandsMode = computed(() => text.value.trimStart().startsWith('>'))
  const recent = computed(
    () => query.value === '' && (kind.value === null || kind.value === SearchKind.SearchHistory)
  )

  const commands = computed<Command[]>(() => {
    const all: Command[] = [
      { id: 'new-request', label: t('search.commands.newRequest'), icon: 'Plus' },
      { id: 'import-collection', label: t('search.commands.importCollection'), icon: 'Upload' },
      { id: 'export-collection', label: t('search.commands.exportCollection'), icon: 'Download' },
      { id: 'environments', label: t('search.commands.environments'), icon: 'Globe' },
      { id: 'settings', label: t('search.commands.settings'), icon: 'Settings2' },
    ]
    const needle = query.value.toLowerCase()
    return needle ? all.filter((command) => command.label.toLowerCase().includes(needle)) : all
  })

  const sections = computed<Section[]>(() => {
    const out: Section[] = []
    if (!commandsMode.value) {
      for (const group of groups.value) {
        if (kind.value !== null && group.kind !== kind.value) continue
        const hits = group.hits ?? []
        if (hits.length === 0) continue
        const history = recent.value && group.kind === SearchKind.SearchHistory
        out.push({
          kind: group.kind,
          label: history ? t('search.recent') : kindLabel(group.kind),
          total: group.total,
          rows: hits.map((hit) => ({ hit, command: null })),
        })
      }
    }
    const wanted = kind.value === null || kind.value === SearchKind.SearchAction
    if (wanted && commands.value.length > 0) {
      out.push({
        kind: SearchKind.SearchAction,
        label: kindLabel(SearchKind.SearchAction),
        total: commands.value.length,
        rows: commands.value.map((command) => ({ hit: null, command })),
      })
    }
    return out
  })

  const rows = computed(() => sections.value.flatMap((section) => section.rows))
  const active = computed(() => rows.value[activeIndex.value] ?? null)

  // The pause before a query goes out, and the number of the latest one: an answer to a query that
  // has since been typed over is dropped.
  const wait = useTimeoutFn(() => void ask(), QUERY_PAUSE_MS, { immediate: false })
  let generation = 0

  function openPalette() {
    open.value = true
    text.value = ''
    kind.value = null
    activeIndex.value = 0
    void ask()
  }

  function close() {
    open.value = false
    wait.stop()
  }

  function setText(next: string) {
    text.value = next
    wait.start()
  }

  // The same chip again turns the filter off.
  function setKind(next: SearchKind | null) {
    kind.value = kind.value === next ? null : next
    void ask()
  }

  async function ask() {
    const mine = ++generation
    const result = await callOrToast(SearchService.Find({ text: query.value, kind: kind.value }), 'search.failed')
    if (mine !== generation || result === undefined) return
    groups.value = result.groups ?? []
    activeIndex.value = 0
  }

  // Wraps around at either end.
  function move(step: number) {
    const count = rows.value.length
    if (count > 0) activeIndex.value = (activeIndex.value + step + count) % count
  }

  async function activate(row: PaletteRow | null, background = false) {
    if (!row) return
    if (!background) close()
    if (row.command) return run(row.command)
    const hit = row.hit
    if (!hit) return

    // «In the background» is the ⌘-open staged one further: the tab is kept and the work area stays
    // on the tab it was on, so the palette can hand over several things in a row.
    const keep = background
    const records = useRecordsStore()
    switch (hit.open.target) {
      case SearchTarget.TargetRequest:
      case SearchTarget.TargetCollection: {
        const collections = useCollectionsStore()
        collections.reveal(hit.open.id)
        await collections.select(hit.open.id, keep, background)
        break
      }
      case SearchTarget.TargetEnvironment:
        await useEnvironmentsStore().setActive(hit.open.id)
        break
      case SearchTarget.TargetVariable:
        useEnvironmentsStore().openSheet({ envId: hit.open.scope || null, varName: hit.title })
        break
      case SearchTarget.TargetHistory: {
        if (!records.records.some((r) => r.id === hit.open.id)) break
        await records.selectRecord(hit.open.id, keep, background)
        break
      }
    }
  }

  function run(command: Command) {
    switch (command.id) {
      case 'new-request':
        useRecordsStore().focusSearch()
        break
      case 'import-collection':
        useTransferStore().askImport()
        break
      case 'export-collection':
        useTransferStore().askExport(useCollectionsStore().collectionId ?? '')
        break
      case 'environments':
        useEnvironmentsStore().openSheet()
        break
      case 'settings':
        void SystemService.ShowSettings('')
        break
    }
  }

  return {
    open,
    text,
    kind,
    groups,
    activeIndex,
    query,
    commandsMode,
    recent,
    commands,
    sections,
    rows,
    active,
    openPalette,
    close,
    setText,
    setKind,
    ask,
    move,
    activate,
    run,
  }
})
