<script setup lang="ts">
import { TabsTrigger, type TabsTriggerProps } from 'reka-ui'

const props = defineProps<TabsTriggerProps>()

defineOptions({ inheritAttrs: false })
</script>

<template>
  <TabsTrigger v-bind="{ ...props, ...$attrs }" class="reading-tab">
    <slot />
  </TabsTrigger>
</template>
