<script setup lang="ts">
import { computed } from 'vue'
import { Browser } from '@wailsio/runtime'
import { renderMarkdown } from '../../lib/markdown'

const { text } = defineProps<{ text: string }>()

const html = computed(() => renderMarkdown(text))

// A link in a description is a way out of the window, and the window is not a browser: it never
// navigates — the click is taken, and only the two schemes the app already opens go to the browser.
function onClick(event: MouseEvent) {
  const target = event.target
  if (!(target instanceof Element)) return
  const link = target.closest('a')
  if (!link) return
  event.preventDefault()
  const href = link.getAttribute('href') ?? ''
  if (/^(https?:|mailto:)/i.test(href)) void Browser.OpenURL(href)
}
</script>

<template>
  <div class="md" data-md v-html="html" @click="onClick"></div>
</template>

<style scoped>
@reference "../../style.css";

.md {
  @apply text-text;
}

.md :deep(a) {
  cursor: pointer;
  text-decoration: none;
}

.md :deep(a:hover) {
  text-decoration: underline;
}
</style>
