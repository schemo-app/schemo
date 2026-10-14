<script setup lang="ts">
import { computed } from 'vue'
import type { Role } from '../../../bindings/schemo/internal/domain'
import Select from '../ui/Select.vue'
import MemberRow from './MemberRow.vue'
import { roleLabel } from '../../lib/roles'
import { useMessages } from '../../i18n'

// A row somebody is filling in: an address, and the role it will be given. An empty role is the one
// the organisation gives a new member by default.
const { roles } = defineProps<{ roles: Role[] }>()

defineEmits<{ (e: 'enter'): void; (e: 'drop'): void }>()

const email = defineModel<string>('email', { required: true })
const roleId = defineModel<string>('roleId', { required: true })

const { t, te } = useMessages()

const options = computed(() => [
  { value: '', label: t('workspaces.roleDefault') },
  ...roles.map((role) => ({ value: role.id, label: roleLabel(t, te, role.name) })),
])
</script>

<template>
  <MemberRow quiet live drop-icon="X" @drop="$emit('drop')">
    <input
      v-model="email"
      class="invite-input"
      type="email"
      spellcheck="false"
      :placeholder="t('workspaces.inviteEmail')"
      @keydown.enter="$emit('enter')"
    />
    <template #role>
      <Select v-model="roleId" :options />
    </template>
  </MemberRow>
</template>

<style scoped>
@reference "../../style.css";

.invite-input {
  @apply flex-1 min-w-0 h-5.5 border-0 bg-transparent text-12 text-text outline-none;
  font-family: inherit;
  padding: 0;
}
</style>
