<script setup lang="ts">
// The words of a tab, drawn twice: what the eye reads, and an invisible copy at the heavier weight
// the chosen tab takes. The invisible one holds the width, so the row stands still when the choice
// moves to another tab.
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
