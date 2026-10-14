<script setup lang="ts">
import {
  DropdownMenuContent,
  DropdownMenuPortal,
  useForwardProps,
  type DropdownMenuContentProps,
} from 'reka-ui'

defineOptions({ inheritAttrs: false })

const props = withDefaults(defineProps<DropdownMenuContentProps>(), {
  sideOffset: 6,
  align: 'end',
  collisionPadding: 8,
})

// Only what the caller passed goes on: an absent boolean reads as false here, and false would
// switch off the library's flipping and shifting at the window's edge.
const forwarded = useForwardProps(props)
</script>

<template>
  <DropdownMenuPortal>
    <DropdownMenuContent v-bind="{ ...forwarded, ...$attrs }" class="menu">
      <slot />
    </DropdownMenuContent>
  </DropdownMenuPortal>
</template>
