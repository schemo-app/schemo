<script setup lang="ts">
import { computed, ref } from 'vue'
import { TwoPaneSheet } from '../ui/sheet'
import CookieDangerSheet from './CookieDangerSheet.vue'
import CookieDetailPane from './CookieDetailPane.vue'
import CookieForm from './CookieForm.vue'
import CookieList from './CookieList.vue'
import { useCookiesStore } from '../../stores/cookies'
import { useMessages } from '../../i18n'

// The manager window: the domains of the jar on the left and, on the right, either the form that
// writes a cookie or the domain being looked at. It holds what neither side can hold alone: the
// confirmation that stands over everything before a domain is taken away.
const emit = defineEmits<{ (e: 'close'): void }>()

const jar = useCookiesStore()
const { t } = useMessages()

const confirming = ref(false)
const jarEmpty = computed(() => jar.domains.length === 0)

async function confirmDanger() {
  confirming.value = false
  await jar.removeDomain()
}

// Escape belongs to the innermost thing: the form first, and the sheet after it.
function cancelTop(): boolean {
  if (!jar.editing) return false
  jar.cancelEdit()
  return true
}
</script>

<template>
  <TwoPaneSheet :title="t('cookies.title')" :cancel-top="cancelTop" @close="emit('close')">
    <template #default>
      <CookieList v-if="!jar.editing" @select="jar.select($event)" @create="jar.beginNew()" />

      <CookieForm
        v-if="jar.editing"
        :key="jar.editing.id || 'new'"
        :cookie="jar.editing"
        @save="jar.save($event)"
        @cancel="jar.cancelEdit()"
      />

      <div v-else-if="jarEmpty" class="empty">
        <span class="empty-title">{{ t('cookies.emptyJar') }}</span>
        <span class="empty-hint">{{ t('cookies.emptyHint') }}</span>
      </div>

      <CookieDetailPane v-else @danger="confirming = true" />
    </template>

    <template #over>
      <CookieDangerSheet v-if="confirming" @close="confirming = false" @confirm="confirmDanger" />
    </template>
  </TwoPaneSheet>
</template>

<style scoped>
@reference "../../style.css";

.empty {
  @apply flex flex-1 flex-col items-center justify-center gap-2 p-8 text-center;
}

.empty-title {
  @apply text-13 font-semibold;
}

.empty-hint {
  @apply text-12 text-text-tertiary;
  max-width: 23.75rem;
  line-height: 1.5;
}
</style>
