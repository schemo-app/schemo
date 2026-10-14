<script setup lang="ts">
import { useToast } from '../../composables/useToast'

const { message, kind } = useToast()
</script>

<template>
  <transition name="fade">
    <div v-if="message" class="toast" :class="kind">{{ message }}</div>
  </transition>
</template>

<style scoped>
@reference "../../style.css";

.toast {
  @apply fixed bottom-4 left-1/2 px-4 py-2 rounded-lg text-12 z-2000 max-w-[80%];
  transform: translateX(-50%);
  background: var(--glass-overlay);
  backdrop-filter: var(--blur-overlay);
  border: 1px solid var(--glass-overlay-border);
  box-shadow: var(--glass-overlay-shadow);
}

.toast.error {
  @apply text-red;
}

.toast.info {
  @apply text-text;
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
