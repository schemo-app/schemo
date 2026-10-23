<script setup lang="ts">
import { computed } from 'vue'
import Icon from '../ui/Icon.vue'
import { nameKey, type CategoryId } from '../../lib/settingsCategories'
import { t } from '../../i18n'
import type { SettingsHit } from '../../lib/settingsIndex'

// The rows the query found, gathered under the section each lives in, with the matching part of the
// name picked out. The whole line is the button: a row is a way into its section.
const { hits } = defineProps<{ hits: SettingsHit[] }>()
const emit = defineEmits<{ (e: 'pick', tab: CategoryId): void }>()


const groups = computed(() => {
  const out: { tab: CategoryId; name: string; items: SettingsHit[] }[] = []
  for (const hit of hits) {
    let held = out.find((one) => one.tab === hit.tab)
    if (!held) {
      held = { tab: hit.tab, name: t(nameKey(hit.tab)), items: [] }
      out.push(held)
    }
    held.items.push(hit)
  }
  return out
})
</script>

<template>
  <div v-if="groups.length" class="groups">
    <div v-for="group in groups" :key="group.tab" class="group">
      <span class="label">{{ group.name }}</span>
      <div class="card">
        <button
          v-for="hit in group.items"
          :key="hit.group + hit.title"
          type="button"
          class="hit"
          @click="emit('pick', hit.tab)"
        >
          <span class="lines">
            <span class="title">
              {{ hit.before }}<span class="mark">{{ hit.match }}</span>{{ hit.after }}
            </span>
            <span class="path">{{ group.name }} › {{ hit.group }}</span>
          </span>
          <Icon name="ChevronRight" :size="12" :stroke-width="2.4" class="caret" />
        </button>
      </div>
    </div>
  </div>

  <div v-else class="empty">
    <span class="empty-title">{{ t('settings.search.none') }}</span>
    <span class="empty-hint">{{ t('settings.search.hint') }}</span>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.groups {
  @apply flex flex-col;
  gap: 1.375rem;
}

.group {
  @apply flex flex-col;
  gap: 0.625rem;
}

.label {
  @apply text-12 font-semibold text-text-tertiary;
}

.card {
  @apply border border-border rounded-[0.625rem] overflow-hidden;
}

.hit {
  @apply w-full flex items-center justify-between bg-transparent border-0 cursor-pointer
         text-left text-text;
  font: inherit;
  gap: 1rem;
  padding: 0.625rem 1rem;
}

.hit + .hit {
  border-top: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
}

.hit:hover {
  @apply bg-bg-hover;
}

.lines {
  @apply flex flex-col min-w-0;
  gap: 0.125rem;
}

.title {
  @apply text-14;
}

.mark {
  @apply rounded-[0.25rem];
  background: color-mix(in srgb, var(--yellow) 28%, transparent);
}

.path {
  @apply text-12 text-text-tertiary;
}

.caret {
  @apply flex-none text-text-tertiary;
}

.empty {
  @apply flex flex-col items-center text-center;
  gap: 0.3rem;
  padding: 3.75rem 1.5rem;
}

.empty-title {
  @apply text-15 font-semibold;
}

.empty-hint {
  @apply text-13 text-text-secondary;
}
</style>
