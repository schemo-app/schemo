<script setup lang="ts">
import { DropdownMenuItem } from 'reka-ui'
import Icon from '../ui/Icon.vue'
import { useEnvironmentsStore } from '../../stores/environments'
import { useMessages } from '../../i18n'
import { useCommands } from '../../composables/useCommands'
import { DropdownMenuContent } from '../ui/dropdown-menu'
import { dotOf } from './palette'

const store = useEnvironmentsStore()

const { t } = useMessages()
const { hint } = useCommands()

const editHint = hint('environments')

function choose(id: string | null) {
  store.setActive(id)
}

function edit() {
  store.openSheet()
}

function create() {
  store.openSheetCreating()
}
</script>

<template>
  <DropdownMenuContent class="env-menu" align="end">
    <DropdownMenuItem
      v-for="env in store.environments"
      :key="env.id"
      class="env-row"
      :class="{ active: env.id === store.activeId }"
      @select="choose(env.id)"
    >
      <span class="env-row-mark">
        <span class="env-row-dot" :style="{ background: dotOf(env.color ?? '') }"></span>
      </span>
      <span class="env-row-name cap-line">{{ env.name }}</span>
      <Icon v-if="env.readonly" name="Lock" :size="12" class="env-row-lock" />
      <span class="env-row-count">{{ env.vars.length }}</span>
      <span class="env-row-check">
        <Icon v-if="env.id === store.activeId" name="Check" :size="12" :stroke-width="2.6" />
      </span>
    </DropdownMenuItem>

    <DropdownMenuItem class="env-row" :class="{ active: store.activeId === null }" @select="choose(null)">
      <span class="env-row-mark"></span>
      <span class="env-row-name none">{{ t('titlebar.noEnvironment') }}</span>
      <span class="env-row-check">
        <Icon v-if="store.activeId === null" name="Check" :size="12" :stroke-width="2.6" />
      </span>
    </DropdownMenuItem>

    <div class="env-divider"></div>

    <DropdownMenuItem class="env-row env-row-action" @select="edit">
      <span class="env-row-mark"><Icon name="AlignLeft" :size="14" :stroke-width="1.8" /></span>
      <span class="env-row-name cap-line">{{ t('environments.editVariables') }}</span>
      <span v-if="editHint" class="env-row-hint">{{ editHint }}</span>
    </DropdownMenuItem>

    <DropdownMenuItem class="env-row env-row-action" @select="create">
      <span class="env-row-mark"><Icon name="Plus" :size="14" :stroke-width="1.9" /></span>
      <span class="env-row-name cap-line">{{ t('environments.newMenu') }}</span>
    </DropdownMenuItem>
  </DropdownMenuContent>
</template>

