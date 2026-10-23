<script lang="ts">
import { ref, watch, type Ref } from 'vue'
import { createSharedComposable, useEventListener } from '@vueuse/core'

// One listener for the whole window: every guard asks the same question.
const pointer = { x: -1, y: -1 }

function track(e: PointerEvent) {
  pointer.x = e.clientX
  pointer.y = e.clientY
}

// One listener for however many hints are mounted, and only while any of them is.
const trackPointer = createSharedComposable(() =>
  useEventListener(window, 'pointermove', track, { capture: true, passive: true })
)

function pointerInside(node: HTMLElement): boolean {
  const r = node.getBoundingClientRect()
  return pointer.x >= r.left && pointer.x <= r.right && pointer.y >= r.top && pointer.y <= r.bottom
}

// Whether the pointer arrived at `el` by moving rather than `el` appearing under it (a popover
// opening where the user clicked). Starts unarmed then, and arms on pointerleave.
function useHoverArrival(el: Readonly<Ref<HTMLElement | null>>) {
  const isArmed = ref(false)

  trackPointer()

  // Post-flush: a box means nothing before layout.
  watch(
    el,
    (node) => {
      isArmed.value = node !== null && !pointerInside(node)
    },
    { flush: 'post' }
  )

  return isArmed
}
</script>

<script setup lang="ts">
import { useTemplateRef } from 'vue'
import Tooltip from './Tooltip.vue'

// Each of these boxes is an even number of pixels tall, so an icon in one must be even too: an odd
// size is centred on a half CSS pixel and the strokes are resampled.
const { variant = 'subtle', size = 'md' } = defineProps<{
  variant?: 'outline' | 'bare' | 'subtle' | 'danger'
  size?: 'sm' | 'md' | 'lg' | 'xl' | 'bar' | 'tab'
  hint?: string
  disabled?: boolean
}>()

defineOptions({ inheritAttrs: false })

const hintEl = useTemplateRef<HTMLElement>('hintEl')
const hintArmed = useHoverArrival(hintEl)
</script>

<template>
  <Tooltip v-if="hint && !disabled" side="top" :disabled="!hintArmed">
    <template #trigger>
      <button
        ref="hintEl"
        v-bind="$attrs"
        type="button"
        :class="['icon-btn', `icon-btn--${variant}`, `icon-btn--${size}`]"
        @pointerleave="hintArmed = true"
      >
        <slot />
      </button>
    </template>
    {{ hint }}
  </Tooltip>

  <button
    v-else
    v-bind="$attrs"
    type="button"
    :disabled
    :title="hint"
    :class="['icon-btn', `icon-btn--${variant}`, `icon-btn--${size}`]"
  >
    <slot />
  </button>
</template>

<style scoped>
@reference "../../style.css";

.icon-btn {
  @apply inline-flex flex-none items-center justify-center border-none bg-transparent cursor-pointer;
  font: inherit;
  transition: color 0.12s ease, background 0.12s ease, border-color 0.12s ease;
  --wails-draggable: no-drag;
}

.icon-btn:disabled {
  @apply opacity-50 cursor-default;
}

.icon-btn:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}

.icon-btn--sm {
  width: 1.25rem;
  height: 1.25rem;
  border-radius: 0.25rem;
}

.icon-btn--md {
  width: 1.5rem;
  height: 1.5rem;
  border-radius: 0.3rem;
}

.icon-btn--lg {
  width: 2rem;
  height: 2rem;
  border-radius: 0.5rem;
}

.icon-btn--xl {
  width: 1.625rem;
  height: 1.625rem;
  border-radius: 0.3rem;
}

.icon-btn--bar {
  width: 1.75rem;
  height: 1.75rem;
  border-radius: 0.3rem;
}

.icon-btn--tab {
  width: 1.875rem;
  height: 1.875rem;
  border-radius: 0.3rem;
}

.icon-btn--outline {
  @apply text-text-secondary;
  border: 1px solid var(--border-strong);
  background: var(--bg-inset);
}

.icon-btn--outline:hover:not(:disabled) {
  @apply bg-bg-hover text-text;
}

.icon-btn--bare {
  @apply text-text-secondary;
}

.icon-btn--bare:hover:not(:disabled) {
  @apply bg-bg-hover text-text;
}

.icon-btn--subtle,
.icon-btn--danger {
  @apply text-text-tertiary;
}

.icon-btn--subtle:hover:not(:disabled) {
  @apply bg-bg-hover text-text;
}

.icon-btn--danger:hover:not(:disabled) {
  @apply bg-bg-hover text-red;
}
</style>
