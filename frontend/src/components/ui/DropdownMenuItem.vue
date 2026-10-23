<script setup lang="ts">
import { DropdownMenuItem, type DropdownMenuItemProps } from 'reka-ui'

defineProps<DropdownMenuItemProps>()
</script>

<template>
  <DropdownMenuItem v-bind="$props" class="menu-item">
    <slot />
  </DropdownMenuItem>
</template>
