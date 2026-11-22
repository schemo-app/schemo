<script setup lang="ts">
import { computed, ref } from 'vue'
import { DropdownMenuGroup } from 'reka-ui'
import Icon from '../ui/Icon.vue'
import Input from '../ui/Input.vue'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import DropdownMenuItem from '../ui/DropdownMenuItem.vue'
import DropdownMenuSeparator from '../ui/DropdownMenuSeparator.vue'
import { useEnvironmentsStore } from '../../stores/environments'
import { useCommands } from '../../composables/useCommands'
import { dotOf } from '../../lib/environmentPalette'
import { t } from '../../i18n'

const { selected, noneLabel, writableOnly = false, manage = true } = defineProps<{
  selected: string | null
  noneLabel: string
  writableOnly?: boolean
  manage?: boolean
}>()
const emit = defineEmits<{ select: [id: string | null] }>()
const environments = useEnvironmentsStore()
const editHint = useCommands().hint('environments')
const query = ref('')
const options = computed(() => environments.environments.filter((env) => !writableOnly || !env.readonly))
const searchable = computed(() => options.value.length > 7)
const listed = computed(() => {
  const needle = query.value.trim().toLowerCase()
  return options.value.filter((env) => !needle || env.name.toLowerCase().includes(needle))
})
</script>

<template>
  <DropdownMenuContent class="environment-menu" @close-auto-focus="query = ''">
    <div v-if="searchable" class="search" @keydown.stop>
      <Icon name="Search" :size="13" />
      <Input v-model="query" variant="bare" size="sm" :placeholder="t('common.search')" />
    </div>
    <DropdownMenuItem class="option" @select="emit('select', null)">
      <span class="dot none"></span>
      <span class="label">{{ noneLabel }}</span>
      <span class="check"><Icon v-if="!selected" name="Check" :size="13" :stroke-width="2.6" /></span>
    </DropdownMenuItem>
    <DropdownMenuSeparator />
    <DropdownMenuGroup class="options">
      <DropdownMenuItem v-for="env in listed" :key="env.id" class="option" @select="emit('select', env.id)">
        <span class="dot" :style="{ background: dotOf(env.color ?? '') }"></span>
        <span class="label">{{ env.name }}</span>
        <Icon v-if="env.readonly" name="Lock" :size="12" class="lock" />
        <span class="check"><Icon v-if="env.id === selected" name="Check" :size="13" :stroke-width="2.6" /></span>
      </DropdownMenuItem>
      <span v-if="query && !listed.length" class="empty">{{ t('common.nothingFound') }}</span>
    </DropdownMenuGroup>
    <template v-if="manage">
      <DropdownMenuSeparator />
      <DropdownMenuItem class="option" @select="environments.openSheet()">
        <span class="label">{{ t('environments.manage') }}</span>
        <span class="hint">{{ editHint }}</span>
      </DropdownMenuItem>
    </template>
  </DropdownMenuContent>
</template>

<style scoped>
@reference "../../style.css";

.environment-menu {
  display: flex;
  flex-direction: column;
  width: 240px;
  min-width: 0;
  max-height: var(--reka-dropdown-menu-content-available-height);
  overflow: hidden;
}

.option {
  gap: 9px;
  height: 30px;
  padding: 0 8px;
  border-radius: 7px;
  font-size: var(--text-13);
}

.dot {
  flex: none;
  width: 7px;
  height: 7px;
  border-radius: 50%;
}

.dot.none {
  box-shadow: inset 0 0 0 1.5px var(--text-tertiary);
}

.label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.check {
  display: flex;
  justify-content: center;
  flex: none;
  width: 14px;
  color: var(--text-secondary);
}

.hint,
.lock {
  flex: none;
  font-size: var(--text-12);
  color: var(--text-tertiary);
}

.options {
  min-height: 0;
  max-height: 256px;
  overflow-y: auto;
  overscroll-behavior-y: contain;
}

.search {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 30px;
  margin-bottom: 4px;
  padding: 0 9px;
  border-radius: 7px;
  background: var(--bg-inset);
  box-shadow: inset 0 0 0 1px var(--border);
  color: var(--text-tertiary);
}

.search :deep(input) {
  width: 100%;
  min-width: 0;
}

.empty {
  display: block;
  padding: 7px 8px;
  font-size: var(--text-13);
  color: var(--text-tertiary);
}
</style>
