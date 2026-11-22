<script setup lang="ts">
import { computed } from 'vue'
import Modal from '../ui/Modal.vue'
import DialogButton from '../ui/DialogButton.vue'
import SheetRow from '../ui/SheetRow.vue'
import { useSheetLines } from '../../composables/useSheetLines'
import { useEnvironmentsStore } from '../../stores/environments'
import { t } from '../../i18n'

// Asks before an environment is deleted or merged into another.
const { into = null } = defineProps<{ into?: string | null }>()

const emit = defineEmits<{ (e: 'close'): void; (e: 'confirm'): void }>()

const environments = useEnvironmentsStore()
const { gone, kept } = useSheetLines()

const env = computed(() => environments.envOf(environments.editedEnvId))
const target = computed(() => environments.envOf(into))
const blocks = computed(() => Object.values(env.value?.collections ?? {}).filter((vars) => vars.length).length)

const names = computed(() => ({ from: env.value?.name ?? '', into: target.value?.name ?? '' }))

const rows = computed(() =>
  target.value
    ? [
        gone(names.value.from, t('environments.mergeSheetFromNote')),
        kept(names.value.into, t('environments.mergeSheetIntoNote')),
      ]
    : [
        gone(t('environments.deleteSheetVariables', { n: env.value?.vars.length ?? 0 })),
        gone(t('environments.deleteSheetBlocks', { n: blocks.value })),
        kept(t('environments.globals')),
      ]
)
</script>

<template>
  <Modal
    :open="true"
    :title="target ? t('environments.mergeSheetTitle', names) : t('environments.deleteSheetTitle', names)"
    variant="ask"
    @close="emit('close')"
  >
    <p class="hint">{{ target ? t('environments.mergeSheetSub', names) : t('environments.deleteSheetSub') }}</p>
    <div class="rows">
      <SheetRow v-for="row in rows" :key="row.label" v-bind="row" />
    </div>
    <template #foot>
      <DialogButton @click="emit('close')">{{ t('common.cancel') }}</DialogButton>
      <DialogButton :primary="Boolean(target)" :danger="!target" @click="emit('confirm')">
        {{ target ? t('environments.merge') : t('common.delete') }}
      </DialogButton>
    </template>
  </Modal>
</template>

<style scoped>
@reference "../../style.css";

.hint {
  @apply text-13 text-text-secondary;
  line-height: 1.5;
  text-wrap: pretty;
}

.rows {
  @apply flex flex-col gap-0.5;
}
</style>
