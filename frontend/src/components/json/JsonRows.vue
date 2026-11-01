<script setup lang="ts">
import { toRef, useTemplateRef, watch } from 'vue'
import { useElementSize, useVirtualList } from '@vueuse/core'
import type { JsonNode } from '../../lib/jsonTree'

const { rows, rowHeight } = defineProps<{ rows: JsonNode[]; rowHeight: number }>()
const measure = useTemplateRef<HTMLElement>('measure')
// Measure a rem in the current interface size, including changes while the viewer is open.
const { height } = useElementSize(measure)
const itemHeight = () => height.value || rowHeight * 16
const { list, containerProps, wrapperProps } = useVirtualList(toRef(() => rows), {
    itemHeight,
    overscan: 12,
})

// Folding a branch or replacing the response can make the old scroll position fall past the end.
watch([() => rows, height], () => {
    const viewport = containerProps.ref.value
    if (!viewport) return
    const end = Math.max(0, rows.length * itemHeight() - viewport.clientHeight)
    viewport.scrollTop = Math.min(viewport.scrollTop, end)
    containerProps.onScroll()
})
</script>

<template>
  <div v-bind="containerProps" class="json-rows" tabindex="0">
    <div ref="measure" class="row-measure" :style="{ height: `${rowHeight}rem` }" aria-hidden="true"></div>
    <div v-bind="wrapperProps">
      <slot v-for="{ data: node } in list" :key="node.path" :node />
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.json-rows {
  @apply relative flex-1 min-h-0 overflow-auto;
  overflow-anchor: none;
}

.row-measure {
  position: absolute;
  width: 1px;
  visibility: hidden;
  pointer-events: none;
}
</style>
