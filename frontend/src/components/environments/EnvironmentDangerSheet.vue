<script setup lang="ts">
import { computed } from 'vue'
import ConfirmSheet from '../ui/ConfirmSheet.vue'
import { useSheetLines } from '../../composables/useSheetLines'
import { useEnvironmentsStore } from '../../stores/environments'
import { t } from '../../i18n'

// What is about to go, before it goes. The two destructive things in this window are one shape: an
// environment takes its variables and their secrets with it, and the globals, which cannot be
// deleted, only emptied, take what is written in them and leave every environment alone.
const { kind, referenced } = defineProps<{ kind: 'delete' | 'clear'; referenced: boolean }>()

const emit = defineEmits<{ (e: 'close'): void; (e: 'confirm'): void }>()

const environments = useEnvironmentsStore()
const { gone, kept } = useSheetLines()

const env = computed(() => environments.environments.find((environment) => environment.id === environments.editedEnvId) ?? null)
const secretCount = computed(() => env.value?.vars.filter((variable) => variable.kind === 'secret').length ?? 0)

const rows = computed(() =>
  kind === 'delete'
    ? [
        gone(t('environments.deleteSheetVariables'), t('environments.deleteSheetVariablesNote')),
        gone(
          t('environments.deleteSheetSecrets', { n: secretCount.value }),
          t('environments.deleteSheetSecretsNote')
        ),
        kept(t('environments.globals'), t('environments.deleteSheetGlobalsNote')),
      ]
    : [
        gone(
          t('environments.clearSheetVars', { n: environments.globals.length }),
          t('environments.deleteSheetVariablesNote')
        ),
        kept(t('environments.clearSheetOverrides'), t('environments.clearSheetOverridesNote')),
      ]
)

// Nothing references an environment until something does: the sentence is about requests that
// use the variables, and a plain one is the truth when there are none.
const sub = computed(() => {
  if (kind === 'clear') return t('environments.clearSheetSub')
  return referenced ? t('environments.deleteSheetSub') : t('environments.deleteSheetSubPlain')
})
</script>

<template>
  <ConfirmSheet
    :title="kind === 'delete' ? t('environments.dangerDelete') : t('environments.dangerClear')"
    :sub
    :rows
    :action="kind === 'delete' ? t('environments.sheetDelete') : t('environments.sheetClear')"
    @close="emit('close')"
    @confirm="emit('confirm')"
  />
</template>
