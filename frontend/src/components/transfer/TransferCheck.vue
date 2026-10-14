<script setup lang="ts">
import Icon from '../ui/Icon.vue'

defineProps<{ on: boolean }>()
</script>

<template>
  <span class="box" :class="{ on }">
    <Icon v-if="on" name="Check" :size="8" :stroke-width="4" />
  </span>
</template>

<style scoped>
@reference "../../style.css";

.box {
  @apply flex items-center justify-center flex-none;
  width: 0.875rem;
  height: 0.875rem;
  border-radius: 0.25rem;
  background: transparent;
  box-shadow: inset 0 0 0 1.5px var(--text-tertiary);
}

.box.on {
  background: var(--accent);
  box-shadow: none;
  color: #fff;
}
</style>
