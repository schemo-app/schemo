<script setup lang="ts">
import { t } from '../../i18n'

// The close button of a window that draws its own title bar. What closing means is the window's — one
// ends itself, the main one ends the application — so the button only says it was pressed.
const emit = defineEmits<{ (e: 'close'): void }>()

</script>

<template>
  <button class="cap-btn cap-close" :title="t('common.close')" @click="emit('close')">
    <span class="cap-icon cap-icon-close">
      <span class="cap-icon-close-bar cap-icon-close-bar-1"></span>
      <span class="cap-icon-close-bar cap-icon-close-bar-2"></span>
    </span>
  </button>
</template>
