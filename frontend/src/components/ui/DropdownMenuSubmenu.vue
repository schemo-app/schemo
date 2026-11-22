<script setup lang="ts">
import { DropdownMenuPortal, DropdownMenuSub, DropdownMenuSubContent, DropdownMenuSubTrigger } from 'reka-ui'
import Icon from './Icon.vue'

defineProps<{ label: string; disabled?: boolean }>()
</script>

<template>
  <DropdownMenuSub>
    <DropdownMenuSubTrigger class="menu-item" :disabled>
      <span class="label cap-line">{{ label }}</span>
      <slot name="indicator" />
      <Icon class="caret" name="ChevronRight" :size="10" :stroke-width="2.6" />
    </DropdownMenuSubTrigger>
    <DropdownMenuPortal>
      <DropdownMenuSubContent class="menu" :collision-padding="8">
        <div class="menu-options">
          <slot />
        </div>
      </DropdownMenuSubContent>
    </DropdownMenuPortal>
  </DropdownMenuSub>
</template>

<style scoped>
.label {
  flex: 1;
  min-width: 0;
}

.caret {
  display: block;
  flex: none;
  align-self: center;
  color: var(--text-secondary);
}
</style>
