<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Sheet from '../ui/Sheet.vue'
import SheetRow from '../ui/SheetRow.vue'
import Switch from '../ui/Switch.vue'
import ChipField from '../settings/ChipField.vue'
import { useFailure } from '../../composables/useFailure'
import { useSetting, useSettings } from '../../composables/useSettings'
import { t } from '../../i18n'
import type { CaptureFilters } from '../../../bindings/schemo/internal/domain'

// What the extension lets through, changed where it is read: the page draws the rules, so the
// panel that writes them opens over it rather than leaving the window for the settings.
const emit = defineEmits<{ (e: 'close'): void; (e: 'saved'): void }>()

const settings = useSettings()
const held = useSetting('captureFilters')
const { failure, run } = useFailure()

// A copy, because the sheet edits what it is given and the stored filters are everybody's.
const copy = (filters: CaptureFilters): CaptureFilters => ({ ...filters, hosts: [...(filters.hosts ?? [])] })

const draft = ref<CaptureFilters>(copy(held.value))

onMounted(() => {
  draft.value = copy(held.value)
})

// An empty list is every site, which is what the extension reads it as.
const hostsNote = computed(() =>
  (draft.value.hosts ?? []).length ? t('extension.hostsListNote') : t('extension.hostsAllNote')
)

async function save() {
  const saved = await run(() =>
    settings.save({ captureFilters: { ...draft.value, hosts: draft.value.hosts ?? [] } })
  )
  if (saved) emit('saved')
}
</script>

<template>
  <Sheet
    :title="t('extension.filters')"
    :sub="t('extension.filtersSub')"
    :cancel="t('common.cancel')"
    :action="t('common.save')"
    @close="emit('close')"
    @cancel="emit('close')"
    @action="save"
  >
    <div class="rows">
      <div class="hosts">
        <span class="row-label">{{ t('extension.hosts') }}</span>
        <span class="row-note">{{ hostsNote }}</span>
        <ChipField
          :values="draft.hosts ?? []"
          :placeholder="t('extension.hostsPlaceholder')"
          @change="draft.hosts = $event"
        />
      </div>

      <SheetRow :label="t('extension.skipStatic')" :note="t('extension.skipStaticNote')">
        <Switch
          :model-value="draft.static"
          :aria-label="t('extension.skipStatic')"
          @update:model-value="draft.static = !!$event"
        />
      </SheetRow>

      <SheetRow :label="t('extension.skipAnalytics')" :note="t('extension.skipAnalyticsNote')">
        <Switch
          :model-value="draft.analytics"
          :aria-label="t('extension.skipAnalytics')"
          @update:model-value="draft.analytics = !!$event"
        />
      </SheetRow>

      <SheetRow :label="t('extension.onlyJson')" :note="t('extension.onlyJsonNote')">
        <Switch
          :model-value="draft.json"
          :aria-label="t('extension.onlyJson')"
          @update:model-value="draft.json = !!$event"
        />
      </SheetRow>

      <span v-if="failure" class="notice">{{ failure }}</span>
    </div>
  </Sheet>
</template>

<style scoped>
@reference "../../style.css";

.rows {
  @apply flex flex-col gap-0.5;
}

.hosts {
  @apply flex flex-col gap-1 min-h-11 px-2.5;
  padding-top: 0.5rem;
  padding-bottom: 0.5rem;
}

.row-label {
  @apply text-13;
}

.row-note {
  @apply text-12 text-text-tertiary;
}

.notice {
  @apply text-13;
  padding: 0.3rem 0.625rem 0;
  color: var(--red-text);
}
</style>
