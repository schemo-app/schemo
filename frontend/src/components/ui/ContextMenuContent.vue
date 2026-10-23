<script setup lang="ts">
import {
  ContextMenuContent,
  ContextMenuPortal,
  useForwardProps,
  type ContextMenuContentProps,
} from 'reka-ui'

defineOptions({ inheritAttrs: false })

const props = withDefaults(defineProps<ContextMenuContentProps>(), { collisionPadding: 8 })

// Only what the caller passed goes on: an absent boolean reads as false here, and false would
// switch off the library's flipping and shifting at the window's edge.
const forwarded = useForwardProps(props)
</script>

<template>
  <ContextMenuPortal>
    <ContextMenuContent v-bind="{ ...forwarded, ...$attrs }" class="menu" @close-auto-focus.prevent>
      <slot />
    </ContextMenuContent>
  </ContextMenuPortal>
</template>
