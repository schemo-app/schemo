<script setup lang="ts">
defineProps<{
  colors: readonly string[]
  tints: Record<string, string>
  labels?: Record<string, string>
  value: string
  ground?: string
  disabled?: boolean
}>()

const emit = defineEmits<{ (e: 'pick', color: string): void }>()
</script>

<template>
  <div class="swatches">
    <button
      v-for="color in colors"
      :key="color"
      type="button"
      class="swatch"
      :class="{ chosen: value === color }"
      :disabled
      :title="labels?.[color]"
      :style="{ background: tints[color], '--tint': tints[color], '--ground': ground }"
      @click="emit('pick', color)"
    ></button>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.swatches {
  @apply flex items-center gap-2 h-8.5;
}

.swatch {
  @apply w-5 h-5 flex-none rounded-full border-0 p-0 cursor-pointer;
}

.swatch:disabled {
  @apply opacity-50 cursor-default;
}

.swatch.chosen {
  box-shadow:
    0 0 0 2px var(--ground, var(--bg-panel)),
    0 0 0 4px var(--tint);
}
</style>
