<script setup lang="ts">
import { t } from '../../i18n'

// The root has its own class because every pane styles a `.foot`, and that would reach this root.
const { failure = '', busy = false, hasInvitations = false, canInvite = true } = defineProps<{
  failure?: string
  busy?: boolean
  hasInvitations?: boolean
  canInvite?: boolean
}>()

defineEmits<{ (e: 'add'): void }>()

</script>

<template>
  <div class="invite-foot">
    <button v-if="canInvite" class="add" type="button" :disabled="busy" @click="$emit('add')">
      {{ t(hasInvitations ? 'workspaces.inviteMore' : 'workspaces.inviteAdd') }}
    </button>
    <span v-if="failure" class="note bad">{{ failure }}</span>
    <span v-else-if="busy" class="note">{{ t('workspaces.membersLoading') }}</span>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.invite-foot {
  @apply flex items-center gap-3 pt-0.5;
}

.add {
  @apply self-start text-12 cursor-pointer rounded-[0.25rem] px-1 py-0.5;
  font-family: inherit;
  color: var(--accent);
  background: transparent;
  border: 0;
}

.add:hover {
  @apply bg-accent-soft;
}

.add:disabled {
  @apply opacity-50 cursor-default;
}

.note {
  @apply text-13 text-text-tertiary;
}

.note.bad {
  color: var(--red-text);
}
</style>
