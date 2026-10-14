<script setup lang="ts">
import { computed } from 'vue'
import { matchParts } from '../../lib/highlightMatch'

const { text, query } = defineProps<{ text: string; query: string }>()

const parts = computed(() => matchParts(text, query))
</script>

<template>
  <template v-for="(part, i) in parts" :key="i">
    <b v-if="part.match" class="search-match">{{ part.text }}</b>
    <template v-else>{{ part.text }}</template>
  </template>
</template>
