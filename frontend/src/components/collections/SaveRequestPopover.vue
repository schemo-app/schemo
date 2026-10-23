<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef, watch } from 'vue'
import { DraftID } from '../../../bindings/schemo/internal/domain'
import Icon from '../ui/Icon.vue'
import PopoverContent from '../ui/PopoverContent.vue'
import { PopoverRoot as Popover, PopoverAnchor } from 'reka-ui'
import { useCollectionsStore } from '../../stores/collections'
import { useCommands } from '../../composables/useCommands'
import { useToast } from '../../composables/useToast'
import { t } from '../../i18n'
import { collectionPlaces } from '../../lib/collectionPlaces'


const { url, defaultName, draftId } = defineProps<{
  url: string
  defaultName: string
  /** The active request tab's draft — what is saved and what the saved node adopts. */
  draftId: DraftID | null
}>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ (e: 'saved'): void }>()

const collections = useCollectionsStore()
const toast = useToast()
const { hint } = useCommands()

interface Target {
  key: string
  name: string
  meta: string
  created: boolean
}

const levels = computed<Target[]>(() =>
  collectionPlaces(collections.tree).map((place) => ({
    key: place.id,
    name: place.path.join(' / '),
    meta: t('collections.savePlace', {
      kind: t(place.path.length > 1 ? 'collections.folderTag' : 'collections.aCollection'),
      requests: t('counts.requests', place.requests),
    }),
    created: false,
  }))
)

const query = ref('')
const chosen = ref('')
const name = ref('')
const saving = ref(false)

const targets = computed<Target[]>(() => {
  const typed = query.value.trim()
  const needle = typed.toLowerCase()
  const found = levels.value.filter((level) => !needle || level.name.toLowerCase().includes(needle))
  const names = needle && !levels.value.some((level) => level.name.toLowerCase() === needle)

  if (!names) return found
  return [
    {
      key: `new:${typed}`,
      name: t('collections.saveCreate', { name: typed }),
      meta: t('collections.newCollection'),
      created: true,
    },
    ...found,
  ]
})

const target = computed(() => targets.value.find((row) => row.key === chosen.value) ?? null)

const targetName = computed(() => {
  const place = target.value
  if (!place) return ''
  return place.created ? t('collections.saveTargetNew', { name: place.key.slice(4) }) : place.name
})

const canSave = computed(() => !!target.value && url.trim().length > 0)

const searchInput = useTemplateRef<HTMLInputElement>('searchInput')

watch(
  open,
  (isOpen) => {
    if (!isOpen) return
    query.value = ''
    name.value = defaultName
    chosen.value = collections.collectionId || levels.value[0]?.key || ''
    nextTick(() => searchInput.value?.focus())
  }
)

watch(targets, (rows) => {
  if (!rows.some((row) => row.key === chosen.value)) chosen.value = rows[0]?.key ?? ''
})

function move(by: number) {
  const rows = targets.value
  if (rows.length === 0) return
  const at = rows.findIndex((row) => row.key === chosen.value)
  chosen.value = rows[(at + by + rows.length) % rows.length].key
}

function onSearchKeydown(e: KeyboardEvent) {
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    move(1)
    return
  }
  if (e.key === 'ArrowUp') {
    e.preventDefault()
    move(-1)
  }
}

function onInteractOutside(e: Event) {
  const target = (e as CustomEvent<{ originalEvent?: Event }>).detail?.originalEvent?.target
  if (target instanceof Element && target.closest('.save-anchor')) e.preventDefault()
}

function onSaveKey() {
  void save()
}

defineExpose({ onSaveKey })

async function save() {
  const place = target.value
  const called = name.value.trim() || defaultName
  if (!place || !canSave.value || saving.value || !draftId) return
  saving.value = true
  try {
    const done = place.created
      ? await collections.saveDraftToNew(place.key.slice(4), called, draftId)
      : await collections.saveDraft(place.key, called, draftId)
    if (!done) return
    toast.show(t('collections.savedTo', { name: targetName.value }))
    emit('saved')
    open.value = false
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <Popover v-model:open="open">
    <PopoverAnchor as-child>
      <span class="anchor"></span>
    </PopoverAnchor>

    <PopoverContent class="save-pop" align="end" @open-auto-focus.prevent @interact-outside="onInteractOutside">
      <div class="sheet-body">
        <div class="head">{{ t('collections.saveRequest') }}</div>

        <input
          v-model="name"
          class="name-input"
          type="text"
          :placeholder="t('collections.saveNamePlaceholder')"
          spellcheck="false"
          @keydown.enter.prevent="save"
        />

        <div class="head">{{ t('collections.saveCollection') }}</div>

        <label class="search">
          <Icon name="Search" :size="14" :stroke-width="1.9" />
          <input
            ref="searchInput"
            v-model="query"
            type="text"
            :placeholder="t('collections.saveFindPlace')"
            spellcheck="false"
            @keydown="onSearchKeydown"
          />
        </label>

        <div class="places">
          <button
            v-for="row in targets"
            :key="row.key"
            type="button"
            class="place"
            :class="{ active: row.key === chosen }"
            @click="chosen = row.key"
          >
            <span class="place-mark">
              <Icon v-if="row.key === chosen" name="Check" :size="14" :stroke-width="2.6" />
              <Icon v-else-if="row.created" name="Plus" :size="14" :stroke-width="2.2" />
            </span>
            <span class="place-text">
              <span class="place-name">{{ row.name }}</span>
              <span class="place-meta">{{ row.meta }}</span>
            </span>
          </button>
        </div>

        <div class="foot">
          <span class="target" :title="targetName">{{ targetName }}<template v-if="hint('save')"> · {{ hint('save') }}</template></span>
          <button type="button" class="save" :disabled="!canSave || saving" @click="save">
            {{ t('common.save') }}
          </button>
        </div>
      </div>
    </PopoverContent>
  </Popover>
</template>

<style scoped>
@reference "../../style.css";

.anchor {
  @apply block w-0 h-0;
}

.sheet-body {
  @apply flex flex-col overflow-hidden;
}

.head {
  @apply flex items-center flex-none text-text-tertiary;
  height: 1.625rem;
  padding: 0 0 0.125rem;
  font-size: var(--text-11-5);
}

.name-input {
  @apply h-7.5 text-13 text-text outline-none;
  width: 100%;
  box-sizing: border-box;
  margin: 0 0 0.5rem;
  padding: 0 0.5rem;
  border-radius: 0.4375rem;
  background: var(--bg-inset);
  border: 1px solid var(--border-strong);
}

.name-input:focus {
  border-color: var(--accent);
}

.search {
  @apply flex items-center gap-[0.5625rem] h-7.5 text-text-tertiary;
  margin: 0 0 0.25rem;
  padding: 0 0.5rem;
  border-radius: 0.4375rem;
  background: var(--bg-inset);
  border: 1px solid var(--border-strong);
}

.search:focus-within {
  border-color: var(--accent);
}

.search input {
  @apply flex-1 min-w-0 bg-transparent border-0 outline-none text-13 text-text;
  font: inherit;
  font-size: var(--text-13);
}

.places {
  @apply flex flex-col gap-px overflow-y-auto;
  max-height: 14.5rem;
}

.place {
  @apply flex items-center gap-[0.5625rem] w-full min-h-9 px-2 rounded-[0.4375rem] border-none bg-transparent text-text text-left cursor-pointer;
  font: inherit;
  --wails-draggable: no-drag;
}

.place:hover {
  background: var(--bg-hover);
}

.place.active {
  @apply bg-accent-soft;
}

.place-mark {
  @apply flex-none w-4 inline-flex items-center justify-center text-accent;
}

.place-text {
  @apply flex-1 min-w-0 flex flex-col gap-0.5;
}

.place-name {
  @apply text-13 truncate;
}

.place.active .place-name {
  @apply font-medium;
}

.place-meta {
  @apply text-text-tertiary truncate;
  font-size: var(--text-11-5);
}

.foot {
  @apply flex items-center gap-2.5;
  margin-top: 0.25rem;
  padding: 0.5rem 0 0.25rem;
  border-top: 1px solid var(--border);
}

.target {
  @apply flex-1 min-w-0 text-text-tertiary overflow-hidden text-ellipsis whitespace-nowrap;
  font-size: var(--text-11-5);
}

.save {
  @apply flex-none h-7.5 px-3 rounded-[0.4375rem] border-0 cursor-pointer text-accent-text text-13 font-semibold;
  font-family: inherit;
  background: var(--accent);
}

.save:hover:not(:disabled) {
  @apply brightness-110;
}

.save:disabled {
  @apply opacity-50 cursor-default;
}
</style>
