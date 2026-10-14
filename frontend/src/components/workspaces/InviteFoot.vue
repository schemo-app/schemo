<script setup lang="ts">
import { useMessages } from '../../i18n'

// Under the people of a space: the button that writes one more invitation, and what the cloud said.
// Its root carries a name of its own, because a class a pane styles reaches a child's root too, and
// every pane has a `.foot` with a line above it.
const { failure = '', busy = false } = defineProps<{ failure?: string; busy?: boolean }>()

defineEmits<{ (e: 'add'): void }>()

const { t } = useMessages()
</script>

<template>
  <div class="invite-foot">
    <button class="add" type="button" :disabled="busy" @click="$emit('add')">
      {{ t('workspaces.inviteMore') }}
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
  @apply self-start text-11-5 cursor-pointer rounded-[0.25rem] px-1 py-0.5;
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
  @apply text-12-5 text-text-tertiary;
}

.note.bad {
  color: var(--red-text);
}
</style>
