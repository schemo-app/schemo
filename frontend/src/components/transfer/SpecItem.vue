<script setup lang="ts">
import { computed } from 'vue'
import Icon from '../ui/Icon.vue'
import ChangeSign from '../ui/ChangeSign.vue'
import Segment from '../ui/Segment.vue'
import TransferCheck from './TransferCheck.vue'
import SpecDiff from './SpecDiff.vue'
import { methodInkClass, shortMethod } from '../../lib/format'
import { t, textOf } from '../../i18n'
import { Field, type Change, type Item } from '../../../bindings/schemo/internal/convert/merge'

const { item, checked, open, takeFile } = defineProps<{
  item: Item
  checked: boolean
  open: boolean
  /** Whether this item's conflicts go the file's way. */
  takeFile: boolean
}>()

const emit = defineEmits<{
  toggle: []
  expand: []
  resolve: [takeFile: boolean]
}>()

const changes = computed(() => item.changes ?? [])
// Go starts an item's id with what the item is: add, request, remove, settings or env.
const kind = computed(() => item.id.split(':')[0])
const conflicted = computed(() => changes.value.some((change) => change.conflict))
const sides = computed(() => [
  { value: 'mine', label: t('spec.mine') },
  { value: 'file', label: t('spec.incoming') },
])

const mark = computed(() => {
  if (kind.value === 'add') return { sign: '+', tone: 'add', title: t('spec.willAdd') }
  if (kind.value === 'remove') return { sign: '−', tone: 'gone', title: t('spec.willRemove') }
  return { sign: '~', tone: 'edit', title: t('spec.willChange') }
})

const name = computed(() => {
  if (kind.value === 'settings') return t('spec.collectionRow')
  if (kind.value === 'env') return t('spec.environmentRow', { name: item.name })
  return item.name
})

const what = computed(() => {
  if (kind.value === 'add') return item.path ?? ''
  if (kind.value === 'remove') return t('spec.notInFile')
  return [...new Set(changes.value.map(word))].join(', ')
})

function word(change: Change) {
  if (change.field === Field.FieldVariable) return change.name ?? ''
  return textOf(`spec.words.${change.field}`, { name: change.name ?? '' })
}

function press() {
  if (changes.value.length) emit('expand')
  else emit('toggle')
}
</script>

<template>
  <div class="spec-item">
    <div class="row" @click="press">
      <button
        type="button"
        class="check"
        role="checkbox"
        :aria-checked="checked"
        :aria-label="name"
        @click.stop="emit('toggle')"
      >
        <TransferCheck :on="checked" />
      </button>
      <ChangeSign :sign="mark.sign" :tone="mark.tone" :title="mark.title" />
      <span class="title">
        <span v-if="item.method" class="method" :class="methodInkClass(item.method)">
          {{ shortMethod(item.method) }}
        </span>
        <span v-else class="method glyph">
          <Icon :name="kind === 'env' ? 'Globe' : 'Settings2'" :size="13" />
        </span>
        <span class="name" :title="item.folder?.join(' / ')">{{ name }}</span>
      </span>
      <span class="what" :title="what">{{ what }}</span>
      <span v-if="conflicted" class="badge warn">{{ t('spec.conflict') }}</span>
      <span v-if="item.declined" class="badge">{{ t('spec.declined') }}</span>
      <span v-if="item.edited" class="badge warn">{{ t('spec.edited') }}</span>
      <button
        v-if="changes.length"
        type="button"
        class="more"
        :aria-expanded="open"
        :aria-label="t('spec.details')"
        @click.stop="emit('expand')"
      >
        <Icon :name="open ? 'ChevronDown' : 'ChevronRight'" :size="10" :stroke-width="2.6" />
      </button>
      <span v-else class="more"></span>
    </div>

    <div v-if="open && changes.length" class="details">
      <!-- An unchecked item is not applied, whatever its conflicts are set to. -->
      <SpecDiff :changes :kind :take-file="checked && takeFile">
        <template v-if="conflicted" #toolbar>
          <span>{{ t('spec.conflicts') }}</span>
          <Segment
            size="xs"
            :grow="false"
            :disabled="!checked"
            :options="sides"
            :value="takeFile ? 'file' : 'mine'"
            @pick="emit('resolve', $event === 'file')"
          />
        </template>
      </SpecDiff>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.row {
  @apply flex items-center;
  height: 2.125rem;
  gap: 0.625rem;
  padding: 0 0.625rem;
  border-radius: 0.5rem;
  cursor: default;
}

.row:hover {
  background: var(--bg-hover);
}

.check,
.more {
  @apply flex items-center justify-center flex-none border-0 p-0 cursor-pointer;
  background: transparent;
}

.more {
  width: 0.875rem;
  height: 0.875rem;
  color: var(--text-tertiary);
}

span.more {
  cursor: default;
}

/* A fixed width, so summaries line up with each other and with the diff's second column. */
.title {
  @apply flex items-center min-w-0;
  flex: 0 0 calc(50% - 1.3125rem);
  gap: 0.625rem;
}

.method {
  @apply flex flex-none items-center;
  width: 2.5rem;
  font-family: var(--mono);
  font-size: var(--text-11);
  font-weight: 700;
}

.glyph {
  color: var(--text-secondary);
}

.name {
  @apply min-w-0 truncate;
}

.what {
  @apply flex-1 min-w-0 truncate;
  color: var(--text-tertiary);
  font-size: var(--text-12);
}

.badge {
  @apply flex-none;
  padding: 0 0.375rem;
  border-radius: 0.25rem;
  background: var(--chip);
  color: var(--text-secondary);
  font-size: var(--text-11);
  line-height: 1.125rem;
}

.badge.warn {
  background: var(--orange-soft);
  color: var(--orange-text);
}

/* The diff starts under the change mark and ends under the chevron. */
.details {
  margin: 0.125rem 0.625rem 0.5rem 2.125rem;
}
</style>
