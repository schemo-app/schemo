<script setup lang="ts">
import { PopoverContent, PopoverPortal, useForwardProps, type PopoverContentProps } from 'reka-ui'

defineOptions({ inheritAttrs: false })

const props = withDefaults(defineProps<PopoverContentProps>(), {
  sideOffset: 6,
  align: 'start',
  collisionPadding: 8,
})

// Only what the caller passed goes on: an absent boolean reads as false here, and false would
// switch off the library's flipping and shifting at the window's edge.
const forwarded = useForwardProps(props)
</script>

<template>
  <PopoverPortal>
    <PopoverContent v-bind="{ ...forwarded, ...$attrs }" class="popover">
      <slot />
    </PopoverContent>
  </PopoverPortal>
</template>
