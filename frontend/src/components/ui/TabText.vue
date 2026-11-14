<script setup lang="ts">
// An invisible bold copy holds the width, so the row does not shift when the selection moves.
</script>

<template>
  <span class="tab-hold" aria-hidden="true"><slot /></span>
  <span class="tab-word"><slot /></span>
</template>

<style scoped>
@reference "../../style.css";

.tab-hold,
.tab-word {
  @apply flex items-center;
  grid-area: 1 / 1;
  gap: 0.3125rem;
}

.tab-hold {
  visibility: hidden;
  font-weight: 500;
}
</style>
