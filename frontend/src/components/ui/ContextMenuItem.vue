<script setup lang="ts">
import { ContextMenuItem, type ContextMenuItemProps } from 'reka-ui'

defineProps<ContextMenuItemProps>()
</script>

<template>
  <ContextMenuItem v-bind="$props" class="menu-item">
    <slot />
  </ContextMenuItem>
</template>
