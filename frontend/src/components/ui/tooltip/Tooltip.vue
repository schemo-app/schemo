<script setup lang="ts">
import {
  TooltipContent,
  TooltipPortal,
  TooltipProvider,
  TooltipRoot,
  TooltipTrigger,
} from 'reka-ui'

withDefaults(
  defineProps<{
    side?: 'top' | 'right' | 'bottom' | 'left'
    delayDuration?: number
    disabled?: boolean
    sideOffset?: number
    ignoreNonKeyboardFocus?: boolean
    open?: boolean
  }>(),
  { side: 'bottom', delayDuration: 150, sideOffset: 6, disabled: false, ignoreNonKeyboardFocus: true, open: undefined }
)

defineOptions({ inheritAttrs: false })
</script>

<template>
  <TooltipProvider :delay-duration>
    <TooltipRoot
      :open
      :delay-duration
      :disabled
      :ignore-non-keyboard-focus
    >
      <TooltipTrigger as-child>
        <slot name="trigger" />
      </TooltipTrigger>
      <TooltipPortal>
        <TooltipContent
          v-bind="$attrs"
          class="tooltip"
          :side
          :side-offset
        >
          <slot />
        </TooltipContent>
      </TooltipPortal>
    </TooltipRoot>
  </TooltipProvider>
</template>
