<script setup lang="ts">
import { defineAsyncComponent, useTemplateRef } from 'vue'
import type { ViewerLanguage } from '../../lib/viewerLanguage'

// Shared by the “Raw” tab and the body of a non-JSON:API answer, so a plain response reads the
// same wherever it is shown. It has no row of its own: it only draws the text and finds in it.
const RawViewer = defineAsyncComponent(() => import('./RawViewer.vue'))

const { text, language = null, query = '', mark = null, picking = false } = defineProps<{
  text: string
  /** Whether a click in the text is a question for the inspector. */
  picking?: boolean
  mark?: { head: number; start: number; end: number } | null
  // The grammar the viewer paints the text with; none is plain text.
  language?: ViewerLanguage
  query?: string
}>()

const emit = defineEmits<{
  stats: [s: { count: number; index: number }]
  pick: [offset: number]
}>()

const viewer = useTemplateRef<{ next: () => void; prev: () => void }>('viewer')

defineExpose({
  next: () => viewer.value?.next(),
  prev: () => viewer.value?.prev(),
})
</script>

<template>
  <RawViewer
    ref="viewer"
    :text
    :query
    :language
    :mark
    :picking
    @stats="(stats) => emit('stats', stats)"
    @pick="(offset) => emit('pick', offset)"
  />
</template>
