<script setup lang="ts">
import { computed } from 'vue'
import Icon from '../ui/Icon.vue'
import { Button, IconButton } from '../ui/button'
import { Popover, PopoverClose, PopoverContent, PopoverTrigger } from '../ui/popover'
import { useCopyFeedback } from '../../composables/useCopyFeedback'
import { isNumberText, methodInkClass } from '../../lib/format'
import type { RecordView } from '../../lib/requestRecord'
import { useMessages } from '../../i18n'

// The line a captured request gets, and the only one it can: it has no command line to be read in,
// so where it went and what it carried are said here. What came back is the strip below — a method
// and a status in this line would be the response said twice.
//
// The field is the editor's own object, drawn read-only: the verb as its own plate, the address
// after it. The one way on from here is into the request line: the captured request becomes the one
// being composed, which is what the button at the end of the line does.
const { record, canOpen = false } = defineProps<{ record: RecordView; canOpen?: boolean }>()

const emit = defineEmits<{ openInRequest: [] }>()

const { t } = useMessages()

// The host and the path of the address, without the query: the query is what the Params button
// beside it is for, and an address spelled twice reads as two different addresses.
function hostPath(url: string): string {
  try {
    const parsed = new URL(url)
    return parsed.host + parsed.pathname
  } catch {
    return url
  }
}

// Values with commas are list-style JSON:API params (include, fields[type]) — chipped one item at a
// time rather than read as one long string.
const params = computed(() => {
  const out: { name: string; values: string[] }[] = []
  try {
    new URL(record.url).searchParams.forEach((value, name) =>
      out.push({ name, values: value.split(',') })
    )
  } catch {
    // Not an address: there is no query to draw, and the field above shows it as it came.
  }
  return out
})

// The same query as one line of text, which is what a reader copies.
const query = computed(() => {
  try {
    return new URL(record.url).search
  } catch {
    return ''
  }
})

// A number reads as a number and a word as a word, which is the whole of what the tint says.
function tint(value: string): string {
  return isNumberText(value) ? 'num' : ''
}

const { copied, copy } = useCopyFeedback()

const copyQuery = () => copy(query.value)
</script>

<template>
  <div class="request-block request-block-solo">
    <div class="bar-row">
      <div class="url-field">
        <span class="method-plate" :class="methodInkClass(record.method)">{{ record.method }}</span>
        <span class="req-url mono" :title="record.url">{{ hostPath(record.url) }}</span>
      </div>

      <Popover v-if="params.length">
        <PopoverTrigger as-child>
          <button
            type="button"
            class="params-btn"
            :title="`${t('response.paramsTitle')} · ${params.length}`"
            :aria-label="t('response.paramsTitle')"
          >
            <Icon name="Settings2" :size="16" :stroke-width="1.7" />
          </button>
        </PopoverTrigger>
        <!-- Hung from the button's right edge rather than its left: the button is at the end of
             the bar, and a 38.75rem panel opening to the right of it would run off the window. -->
        <PopoverContent class="params-menu" align="end" :side-offset="9">
          <div class="params-head">
            <span class="params-title">{{ t('response.paramsTitle') }}</span>
            <span class="params-count mono">{{ params.length }}</span>
            <span class="params-spacer"></span>
            <IconButton
              :hint="copied ? t('common.copied') : t('response.copyQuery')"
              size="xl"
              @click="copyQuery"
            >
              <Icon :name="copied ? 'Check' : 'Copy'" :size="14" :stroke-width="1.8" />
            </IconButton>
            <PopoverClose as-child>
              <IconButton :hint="t('common.close')" size="xl">
                <Icon name="X" :size="14" />
              </IconButton>
            </PopoverClose>
          </div>

          <!-- One row per parameter rather than one grid with rows in it: the divider between two
               parameters is drawn by the row and spans it, which a cell of a shared grid cannot do. -->
          <div v-for="p in params" :key="p.name" class="params-row">
            <div class="params-name-cell">
              <span class="params-name mono">{{ p.name }}</span>
              <span v-if="p.values.length > 1" class="params-item-count mono">
                {{ p.values.length }}
              </span>
            </div>
            <div class="params-chips">
              <span v-for="(v, i) in p.values" :key="i" class="params-chip mono" :class="tint(v)">
                {{ v }}
              </span>
            </div>
          </div>

          <!-- The query as one string, which is what the button above hands over: a chip per value
               says what the request asked for, and this says what the address was. -->
          <div class="params-string">
            <span class="params-string-label">{{ t('response.queryString') }}</span>
            <span class="params-string-text mono">{{ query }}</span>
          </div>
        </PopoverContent>
        </Popover>

      <Button
        v-if="canOpen"
        variant="primary"
        class="repeat-btn"
        :title="t('response.openInRequest')"
        @click="emit('openInRequest')"
      >
        {{ t('response.repeat') }}
      </Button>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.req-url {
  @apply flex-1 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-text;
  font-family: var(--mono);
  font-size: var(--text-13);
}

.params-head {
  @apply flex items-center gap-2.5 pb-2.5;
}

.params-title {
  @apply text-14 font-semibold;
}

.params-count {
  @apply text-13 text-text-tertiary;
  font-family: var(--mono);
}

.params-spacer {
  @apply flex-1;
}

.params-row {
  @apply grid items-start gap-3 py-2.5 px-1.5;
  grid-template-columns: 10.625rem minmax(0, 1fr);
  border-top: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
}

.params-name-cell {
  @apply flex items-baseline gap-1.5;
}

.params-name {
  @apply text-13 text-text;
}

.params-item-count {
  @apply text-12 text-text-tertiary;
}

.params-chips {
  @apply flex flex-wrap gap-1;
}

.params-chip {
  @apply text-13 text-accent py-0.5 px-2 rounded-md;
  background: var(--accent-soft);
}

.params-chip.num {
  @apply text-tok-num bg-bg-hover;
}

.params-string {
  @apply flex gap-3 mt-2.5 p-3 rounded-lg bg-bg-sidebar;
}

.params-string-label {
  @apply flex-none pt-0.5 section-label;
}

.params-string-text {
  @apply flex-1 min-w-0 text-13 break-all;
  line-height: 1.6;
  color: var(--text-secondary);
}

.params-btn {
  @apply relative flex flex-none items-center justify-center cursor-pointer border-0;
  width: 2.125rem;
  height: 2.125rem;
  border-radius: 0.375rem;
  background: transparent;
  color: var(--text-secondary);
  --wails-draggable: no-drag;
}

.params-btn:hover,
.params-btn[data-state='open'] {
  background: var(--bg-hover);
  color: var(--accent);
}

.btn.repeat-btn {
  @apply flex-none;
  height: 2.125rem;
  padding: 0 0.9375rem;
  border-radius: 0.375rem;
  font-size: var(--text-13);
  font-weight: 600;
}
</style>
