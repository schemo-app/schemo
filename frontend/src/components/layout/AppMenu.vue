<script setup lang="ts">
import { t } from '../../i18n'
import { useAppMenu } from '../../composables/useAppMenu'
import Icon from '../ui/Icon.vue'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import DropdownMenuItem from '../ui/DropdownMenuItem.vue'
import DropdownMenuSeparator from '../ui/DropdownMenuSeparator.vue'
import { DropdownMenuRoot as DropdownMenu, DropdownMenuPortal, DropdownMenuSub, DropdownMenuSubContent, DropdownMenuSubTrigger, DropdownMenuTrigger } from 'reka-ui'

const { open, groups } = useAppMenu()
</script>

<template>
  <DropdownMenu v-model:open="open">
    <DropdownMenuTrigger as-child>
      <button class="menu-btn" :class="{ open }" :title="t('appMenu.title')">
        <!-- The library stands the outer lines at 5 and 19 of 24, which at 16px is between pixels;
             drawn without smoothing they are three lines of one pixel, evenly apart. -->
        <Icon name="Menu" :size="16" :stroke-width="1.8" class="menu-glyph" />
      </button>
    </DropdownMenuTrigger>

    <DropdownMenuContent class="app-menu" align="start" :side-offset="4">
      <DropdownMenuSub v-for="group in groups" :key="group.label">
        <DropdownMenuSubTrigger class="menu-item">
          <span class="app-menu-item cap-line">{{ t(group.label) }}</span>
          <Icon class="menu-caret" name="ChevronRight" :size="10" :stroke-width="2.6" />
        </DropdownMenuSubTrigger>

        <DropdownMenuPortal>
          <DropdownMenuSubContent class="menu app-menu-sub">
            <template v-for="(section, index) in group.sections" :key="index">
              <DropdownMenuSeparator v-if="index > 0" />
              <DropdownMenuItem
                v-for="row in section"
                :key="row.label"
                :disabled="row.disabled"
                @select="row.run?.()"
              >
                <span class="menu-mark">
                  <Icon v-if="row.checked" name="Check" :size="12" :stroke-width="2.6" />
                </span>
                <span class="app-menu-item cap-line">{{ t(row.label) }}</span>
                <span v-if="row.kbd" class="menu-key">{{ row.kbd }}</span>
              </DropdownMenuItem>
            </template>
          </DropdownMenuSubContent>
        </DropdownMenuPortal>
      </DropdownMenuSub>
    </DropdownMenuContent>
  </DropdownMenu>
</template>

<style scoped>
@reference "../../style.css";

.menu-btn {
  @apply flex items-center justify-center flex-none;
  width: 2rem;
  height: 1.75rem;
  border: 0;
  border-radius: 0.3rem;
  background: transparent;
  color: var(--text-secondary);
  cursor: default;
  --wails-draggable: no-drag;
}

.menu-btn:hover,
.menu-btn.open {
  background: var(--bg-hover);
  color: var(--text);
}

.menu-glyph {
  shape-rendering: crispEdges;
}
</style>
