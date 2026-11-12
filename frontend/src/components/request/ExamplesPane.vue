<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { DropdownMenuRoot, DropdownMenuTrigger } from 'reka-ui'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import DropdownMenuItem from '../ui/DropdownMenuItem.vue'
import DropdownMenuSeparator from '../ui/DropdownMenuSeparator.vue'
import EmptyState from '../ui/EmptyState.vue'
import Icon from '../ui/Icon.vue'
import StatusBadge from '../ui/StatusBadge.vue'
import ExampleForm from './ExampleForm.vue'
import type { RequestSource } from '../../lib/requestSource'
import { exampleName, exampleOf, isKeepable } from '../../lib/examples'
import { formatBytes, t } from '../../i18n'
import type { ResponseExample } from '../../../bindings/schemo/internal/domain'

const { source } = defineProps<{ source: RequestSource }>()

const picked = ref(0)
const fresh = ref(-1)
const pane = ref<'body' | 'headers'>('body')
const example = computed(() => source.responses[picked.value])
const keepable = computed(() => {
  const record = source.record
  return !source.loading && record && isKeepable(record) ? record : null
})
const sizeOf = (body?: string) => body ? formatBytes(new TextEncoder().encode(body).length) : ''

watch(() => source.responses.length, (length) => {
  if (picked.value >= length) picked.value = Math.max(0, length - 1)
})

function pick(index: number) {
  picked.value = index
  fresh.value = -1
}

function addExample(example: ResponseExample) {
  const list = [...source.responses, example]
  source.saveExamples(list)
  picked.value = list.length - 1
  fresh.value = picked.value
}

function saveResponse() {
  const record = keepable.value
  if (record) addExample(exampleOf(record, exampleName(record.status)))
}

function blankExample() {
  addExample({
    name: '',
    status: 200,
    body: '',
    headers: [{ name: 'Content-Type', value: 'application/json; charset=utf-8' }],
  })
}

function patch(fields: Partial<ResponseExample>) {
  source.saveExamples(source.responses.map((one, index) => index === picked.value ? { ...one, ...fields } : one))
}

function removeExample() {
  const list = source.responses.filter((_, index) => index !== picked.value)
  source.saveExamples(list)
  pick(Math.max(0, Math.min(picked.value, list.length - 1)))
}
</script>

<template>
  <ExampleForm
    v-if="example"
    :key="`${picked}:${source.responses.length}`"
    v-model:pane="pane"
    :example
    :fresh="picked === fresh"
    @patch="patch"
    @remove="removeExample"
  >
    <DropdownMenuRoot>
      <DropdownMenuTrigger as-child>
        <button type="button" class="example-switch" :title="t('request.examples.select')" :aria-label="t('request.examples.select')">
          {{ t('request.examples.selection', { current: picked + 1, total: source.responses.length }) }}
          <Icon name="ChevronDown" :size="10" :stroke-width="1.5" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        <div class="example-menu">
          <DropdownMenuItem
            v-for="(one, index) in source.responses"
            :key="index"
            class="example-option"
            :class="{ selected: index === picked }"
            @select="pick(index)"
          >
            <StatusBadge :status="one.status ?? 0" class="example-code" />
            <span class="min-w-0 flex-1 truncate" :class="{ 'text-text-tertiary': !one.name }">
              {{ one.name || t('request.examples.untitled') }}
            </span>
            <span class="example-size">{{ sizeOf(one.body) }}</span>
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem v-if="keepable" @select="saveResponse">
            <Icon name="Bookmark" :size="14" :stroke-width="1.5" />
            <span class="flex-1">{{ t('request.examples.saveResponse') }}</span>
            <span class="example-size">{{ keepable.status }}</span>
          </DropdownMenuItem>
          <DropdownMenuItem @select="blankExample">
            <Icon name="Plus" :size="14" :stroke-width="1.5" />
            {{ t('request.examples.createBlank') }}
          </DropdownMenuItem>
        </div>
      </DropdownMenuContent>
    </DropdownMenuRoot>
  </ExampleForm>
  <EmptyState
    v-else
    :title="t('request.examples.emptyTitle')"
    :text="t(keepable ? 'request.examples.emptyWithResponse' : 'request.examples.panelEmpty')"
  >
    <div class="example-empty-actions">
      <button v-if="keepable" type="button" class="example-link" @click="saveResponse">
        {{ t('request.examples.saveResponse') }}
      </button>
      <button type="button" class="example-link" @click="blankExample">
        {{ t('request.examples.createBlank') }}
      </button>
    </div>
  </EmptyState>
</template>

<style scoped>
@reference "../../style.css";

.example-switch {
  @apply inline-flex flex-none items-center gap-1.5 rounded-md border-0 bg-transparent text-12 text-text-tertiary whitespace-nowrap cursor-pointer;
  height: 1.375rem;
  padding: 0 0.25rem 0 0.375rem;
}

.example-switch:hover,
.example-switch[data-state="open"],
.example-option.selected {
  background: var(--bg-hover);
}

.example-menu {
  width: min(18rem, calc(var(--reka-dropdown-menu-content-available-width, 100vw) - 0.75rem));
  max-height: min(21.75rem, calc(var(--reka-dropdown-menu-content-available-height, 100vh) - 0.75rem));
  overflow: auto;
}

.example-option {
  height: 2.125rem;
  gap: 0.625rem;
  padding-inline: 0.375rem 0.625rem;
}

.example-option.selected {
  font-weight: 500;
}

.example-code {
  @apply flex-none text-12;
  width: 2.25rem;
}

.example-size {
  @apply flex-none text-12 text-text-tertiary;
}

.example-empty-actions {
  @apply flex flex-wrap justify-center gap-x-4 gap-y-2;
  margin-top: 0.125rem;
}

.example-link {
  @apply cursor-pointer border-0 bg-transparent p-0 text-13 text-accent;
}

.example-link:hover {
  text-decoration: underline;
}
</style>
