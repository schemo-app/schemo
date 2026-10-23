<script setup lang="ts">
defineProps<{ title: string }>()
</script>

<template>
  <section class="block">
    <div class="block-title">
      <span>{{ title }}</span>
      <slot name="aside" />
    </div>
    <slot />
  </section>
</template>

<style scoped>
@reference "../../style.css";

.block {
  @apply flex flex-col gap-1.5;
}

.block-title {
  @apply flex items-center justify-between section-label;
  min-height: 1.25rem;
}
</style>
