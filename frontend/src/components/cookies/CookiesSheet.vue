<script setup lang="ts">
import { computed, ref } from 'vue'
import TwoPaneSheet from '../ui/TwoPaneSheet.vue'
import CookieDangerSheet from './CookieDangerSheet.vue'
import CookieDetailPane from './CookieDetailPane.vue'
import CookieForm from './CookieForm.vue'
import CookieList from './CookieList.vue'
import { useCookiesStore } from '../../stores/cookies'
import { t } from '../../i18n'

// The manager window: the cookie domains on the left and, on the right, either the form that
// writes a cookie or the domain being looked at. It holds what neither side can hold alone: the
// confirmation that stands over everything before a domain is taken away.
const emit = defineEmits<{ (e: 'close'): void }>()

const cookies = useCookiesStore()

const confirming = ref(false)
const jarEmpty = computed(() => cookies.domains.length === 0)

async function confirmDanger() {
  confirming.value = false
  await cookies.removeDomain()
}

// Escape belongs to the innermost thing: the form first, and the sheet after it.
function cancelTop(): boolean {
  if (!cookies.editing) return false
  cookies.cancelEdit()
  return true
}
</script>

<template>
  <TwoPaneSheet :title="t('cookies.title')" :cancel-top="cancelTop" @close="emit('close')">
    <template #default>
      <CookieList v-if="!cookies.editing" @select="cookies.select($event)" @create="cookies.beginNew()" />

      <CookieForm
        v-if="cookies.editing"
        :key="cookies.editing.id || 'new'"
        :cookie="cookies.editing"
        @save="cookies.save($event)"
        @cancel="cookies.cancelEdit()"
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
