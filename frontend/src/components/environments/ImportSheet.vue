<script setup lang="ts">
import { computed } from 'vue'
import Sheet from '../ui/Sheet.vue'
import { t } from '../../i18n'

const emit = defineEmits<{ (e: 'close'): void; (e: 'choose'): void }>()


const rows = computed(() => [
  {
    label: t('environments.importOverwrite'),
    note: t('environments.importOverwriteNote'),
    tag: t('environments.importOn'),
    tone: 'keep' as const,
  },
  {
    label: t('environments.importSecrets'),
    note: t('environments.importSecretsNote'),
    tag: t('environments.importOn'),
    tone: 'keep' as const,
  },
  {
    label: t('environments.importComments'),
    note: t('environments.importCommentsNote'),
    tag: t('environments.importSkip'),
    tone: 'drop' as const,
  },
])
</script>

<template>
  <Sheet
    :title="t('environments.importEnv')"
    :sub="t('environments.importSub')"
    :rows
    :action="t('environments.chooseFile')"
    @close="emit('close')"
    @action="emit('choose')"
  />
</template>
