<script setup lang="ts">
import { ref, watch } from 'vue'
import { groupHue } from '../../lib/recordTabs'

const { src, label, tabKey } = defineProps<{ src?: string; label: string; tabKey: string }>()
const failed = ref(false)
watch(() => src, () => { failed.value = false })
</script>

<template>
  <span class="tab-favicon" aria-hidden="true">
    <img v-if="src && !failed" :src alt="" loading="lazy" referrerpolicy="no-referrer" @error="failed = true" />
    <span v-else class="tab-initial" :style="{ '--tab-hue': groupHue(tabKey) }">
      {{ label.slice(0, 1).toUpperCase() }}
    </span>
  </span>
</template>

<style scoped>
@reference "../../style.css";

.tab-favicon {
  @apply inline-flex flex-none;
  width: 1.125rem;
  height: 1.125rem;
}

.tab-favicon.small {
  width: 1rem;
  height: 1rem;
}

.tab-favicon img {
  @apply size-full object-contain rounded-sm;
}

.tab-initial {
  @apply grid size-full place-items-center rounded-full text-white text-10 font-bold;
  background: hsl(var(--tab-hue) 58% 45%);
}
</style>
