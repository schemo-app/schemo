<script setup lang="ts">
// Drawn in the current text colour.
</script>

<template>
  <span class="spinner"></span>
</template>

<style scoped>
.spinner {
  display: inline-block;
  flex: none;
  box-sizing: border-box;
  width: 0.875rem;
  height: 0.875rem;
  border-radius: 50%;
  border: 0.125rem solid color-mix(in srgb, currentColor 35%, transparent);
  border-top-color: currentColor;
  animation: spin 0.72s linear infinite;
}
</style>
