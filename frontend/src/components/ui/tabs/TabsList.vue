<script setup lang="ts">
import { TabsList, type TabsListProps } from 'reka-ui'

const props = defineProps<TabsListProps>()

defineOptions({ inheritAttrs: false })
</script>

<template>
  <TabsList v-bind="{ ...props, ...$attrs }" class="reading-strip">
    <slot />
  </TabsList>
</template>
