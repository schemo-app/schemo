<script setup lang="ts">
import { computed } from 'vue'
import Sheet from '../ui/Sheet.vue'
import { t } from '../../i18n'

// What a `.env` file is about to become, before it becomes it. The three lines are the whole set of
// decisions an import makes (which key wins, what counts as a secret, what is not read at all), so
// they are stated once here instead of being asked per key.
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
