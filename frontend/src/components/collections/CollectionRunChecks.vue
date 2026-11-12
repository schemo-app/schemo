<script setup lang="ts">
import { computed } from 'vue'
import { useScriptRuns } from '../../composables/useScriptRuns'
import { t } from '../../i18n'
import Icon from '../ui/Icon.vue'

const { recordId } = defineProps<{ recordId: string }>()
const { runs, isLoading } = useScriptRuns(() => recordId)
const checks = computed(() => runs.value.flatMap(run => run.tests ?? []))
</script>

<template>
  <div class="run-checks">
    <p v-if="isLoading" class="px-2.5 py-1.5 text-text-tertiary">{{ t('response.scripts.loading') }}</p>
    <div v-for="(check, index) in checks" :key="index" class="run-check">
      <Icon :name="check.passed ? 'Check' : 'X'" :size="13" :class="check.passed ? 'text-green' : 'text-red'" />
      <span class="min-w-0 break-words">
        {{ check.name }}
        <span v-if="check.error" class="block text-12 text-text-tertiary">{{ check.error }}</span>
      </span>
    </div>
    <p v-if="!isLoading && !checks.length" class="px-2.5 py-1.5 text-text-tertiary">{{ t('response.scripts.none') }}</p>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.run-checks {
  @apply overflow-y-auto border-b border-border;
  max-height: 16rem;
  margin-bottom: 0.25rem;
}

.run-check {
  @apply flex items-start gap-2 px-2.5 py-1.5;
  min-height: 2rem;
}
</style>
