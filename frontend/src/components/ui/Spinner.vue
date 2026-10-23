<script setup lang="ts">
// The one spinner of the window: the ring a request shows on its button while it is under way. It
// is drawn in the colour of the text it stands beside, so a place that wants the accent says so
// with a text colour of its own.
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
