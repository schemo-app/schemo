<script lang="ts">
import * as ICONS from '../../lib/icons'

/** Every glyph the window draws, named as the icon library names it. */
export type IconName = keyof typeof ICONS
</script>

<script setup lang="ts">
import { computed } from 'vue'
import { rem } from '../../lib/units'

const props = defineProps<{
  name: IconName
  size?: number
  strokeWidth?: number
}>()

const icon = computed(() => ICONS[props.name])
const pixelSize = computed(() => props.size ?? 16)

// The size is in the same unit as everything else, so a glyph grows with the interface. It goes on
// as a style and not as the width/height attributes: those are pixels whatever the root font is.
const box = computed(() => rem(pixelSize.value))

// 1.8 from 14px up and proportionally heavier below, where a thinner line fades.
const weight = computed(() => props.strokeWidth ?? Math.max(1.8, (1.8 * 14) / pixelSize.value))
</script>

<template>
  <component
    :is="icon"
    :size="pixelSize"
    :stroke-width="weight"
    :style="{ width: box, height: box }"
    aria-hidden="true"
  />
</template>
