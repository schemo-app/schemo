<script setup lang="ts">
import { onMounted, ref, useTemplateRef } from 'vue'
import EnvironmentDetailPane from './EnvironmentDetailPane.vue'
import EnvironmentDangerSheet from './EnvironmentDangerSheet.vue'
import EnvironmentList from './EnvironmentList.vue'
import TwoPaneSheet from '../ui/TwoPaneSheet.vue'
import { useEnvironmentsStore } from '../../stores/environments'
import { useSheetNotice } from '../../composables/useSheetNotice'
import { t } from '../../i18n'

const emit = defineEmits<{ (e: 'close'): void }>()

const environments = useEnvironmentsStore()
const { clearNotice, withNotice } = useSheetNotice()

const detail = useTemplateRef<InstanceType<typeof EnvironmentDetailPane>>('detail')

// The confirmation on top: a delete (into is null) or a merge into another environment.
const asking = ref<{ into: string | null } | null>(null)

async function confirm() {
  const from = environments.editedEnvId
  const into = asking.value?.into ?? null
  asking.value = null
  if (!from) return
  await withNotice(async () => {
    if (into) {
      await environments.mergeEnv(from, into)
      return
    }
    await environments.removeEnv(from)
    environments.editEnv(environments.environments[0]?.id ?? null)
  })
}

function cancelTop(): boolean {
  return detail.value?.cancelTop() ?? false
}

onMounted(clearNotice)
</script>

<template>
  <TwoPaneSheet :title="t('environments.title')" :width="920" split-header :cancel-top="cancelTop" @close="emit('close')">
    <EnvironmentList @close="emit('close')" />
    <EnvironmentDetailPane
      ref="detail"
      @merge="asking = { into: $event }"
      @delete="asking = { into: null }"
      @close="emit('close')"
    />

    <template #over>
      <EnvironmentDangerSheet v-if="asking" :into="asking.into" @close="asking = null" @confirm="confirm" />
    </template>
  </TwoPaneSheet>
</template>
