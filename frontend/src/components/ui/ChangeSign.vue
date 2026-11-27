<script setup lang="ts">
// How a list of changes marks a row: it comes, changes or goes.
defineProps<{ sign: string; tone: string }>()
</script>

<template>
  <span class="sign" :class="tone">{{ sign }}</span>
</template>

<style scoped>
@reference "../../style.css";

.sign {
  @apply flex items-center justify-center flex-none;
  width: 1rem;
  height: 1rem;
  border-radius: 0.25rem;
  font-family: var(--mono);
  font-size: var(--text-11);
  font-weight: 700;
  line-height: 1;
}

.sign.add {
  background: var(--green-soft);
  color: var(--green);
}

.sign.edit {
  background: var(--orange-soft);
  color: var(--orange);
}

.sign.gone {
  background: var(--red-soft);
  color: var(--red);
}
</style>
