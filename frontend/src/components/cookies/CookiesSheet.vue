<script setup lang="ts">
import { computed, ref } from 'vue'
import TwoPaneSheet from '../ui/TwoPaneSheet.vue'
import EmptyState from '../ui/EmptyState.vue'
import CookieDangerSheet from './CookieDangerSheet.vue'
import CookieDetailPane from './CookieDetailPane.vue'
import CookieForm from './CookieForm.vue'
import CookieList from './CookieList.vue'
import { useCookiesStore } from '../../stores/cookies'
import { t } from '../../i18n'

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

      <EmptyState v-else-if="jarEmpty" :title="t('cookies.emptyJar')" :text="t('cookies.emptyHint')" />

      <CookieDetailPane v-else @danger="confirming = true" />
    </template>

    <template #over>
      <CookieDangerSheet v-if="confirming" @close="confirming = false" @confirm="confirmDanger" />
    </template>
  </TwoPaneSheet>
</template>
