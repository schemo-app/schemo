<script lang="ts">
import * as ICONS from './icons'

/** Every glyph the window draws, named as the icon library names it. */
export type IconName = keyof typeof ICONS
</script>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  name: IconName
  size?: number
  strokeWidth?: number
}>()

const icon = computed(() => ICONS[props.name])
const pixelSize = computed(() => props.size ?? 16)

// The size is in the same unit as everything else, so a glyph grows with the interface. It goes on
// as a style and not as the width/height attributes: those are pixels whatever the root font is.
const box = computed(() => `${pixelSize.value / 16}rem`)

// A glyph carries one weight however large it is drawn: the design's own 1.8 from 14 up, and
// proportionally more below, where a thinner line would fade. A site that draws another weight —
// a bolder tick, a lighter tool — says so and is left alone.
const weight = computed(() => props.strokeWidth ?? Math.max(1.8, (1.8 * 14) / pixelSize.value))

// At 16px every straight line of the glyph's 24-unit grid falls on the edge between two pixels and
// is drawn as two grey ones; half a pixel over, each is one sharp pixel. `translate` and not
// `transform`: a glyph that turns keeps its own transform.
const offset = computed(() => (pixelSize.value === 16 ? '0.5px 0.5px' : undefined))
</script>

<template>
  <component
    :is="icon"
    :size="pixelSize"
    :stroke-width="weight"
    :style="{ width: box, height: box, translate: offset }"
    aria-hidden="true"
  />
</template>
