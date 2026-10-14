<script setup lang="ts">
import { Button } from '../ui/button'
import { useAccount } from '../../composables/useAccount'
import { useMessages } from '../../i18n'

// What a team's space needs, said where its people would stand, and the way in under it: the
// account's own dialog, over this sheet.
defineProps<{ note: string }>()

const { t } = useMessages()
const { openSignIn } = useAccount()
</script>

<template>
  <div class="gate">
    <span class="gate-note">{{ note }}</span>
    <Button variant="primary" size="field" @click="openSignIn()">
      {{ t('account.signIn') }}
    </Button>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.gate {
  @apply flex flex-col items-start gap-2.5 rounded-lg border border-border px-3 py-2.5;
}

.gate-note {
  @apply text-12-5 text-text-secondary;
}
</style>
