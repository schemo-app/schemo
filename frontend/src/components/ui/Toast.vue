<script setup lang="ts">
import { useToast } from '../../composables/useToast'

const { message, kind } = useToast()
</script>

<template>
  <transition name="fade">
    <div v-if="message" class="toast-layout">
      <div class="toast" :class="kind">{{ message }}</div>
    </div>
  </transition>
</template>

<style scoped>
@reference "../../style.css";

.toast-layout {
  @apply fixed inset-x-0 bottom-4 z-2000 flex justify-center pointer-events-none;
}

.toast {
  @apply px-4 py-2 rounded-lg text-12 max-w-[80%] pointer-events-auto;
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
