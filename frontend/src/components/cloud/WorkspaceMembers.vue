<script setup lang="ts">
import Select from '../ui/Select.vue'
import InviteFoot from './InviteFoot.vue'
import InviteRow from './InviteRow.vue'
import MemberRow from './MemberRow.vue'
import SignInGate from './SignInGate.vue'
import { roleTint } from '../../lib/roles'
import { formatAgo, t } from '../../i18n'
import type { useWorkspaceTeam } from '../../composables/useWorkspaceTeam'

// Who is in a team's workspace and who is invited to it, with the rows written to invite more; a workspace
// of somebody's own is drawn without the block. The team itself is the pane's (useWorkspaceTeam.ts).
const props = defineProps<{
  state: ReturnType<typeof useWorkspaceTeam>
}>()

const {
  team, asking, failure, signedOut, drafts,
  addDraft, dropDraft, send, give, drop, revoke, roleName,
} = props.state
</script>

<template>
  <!-- A team's people are the cloud's to say, and without a sign-in it is not asked: the frame says
  so, with the way in under it, instead of an empty list and the cloud's refusal. The create form
  draws the same frame for the same reason. -->
  <SignInGate v-if="signedOut" :note="t('workspaces.membersNeedAccount')" />

  <template v-else>
    <!-- An empty frame is not drawn. A workspace that has just been linked is one the account has not
    answered about yet, and a card with nothing in it says "nobody" where the truth is "not yet"; the
    line under it says which of the two it is. -->
    <div v-if="team?.members?.length || team?.invites?.length || drafts.length" class="card">
      <!-- Somebody else's role is a control; the reader's own is a word. A workspace's owner is what they
      were made, and handing their own place about is not a row in a list: there is nothing to remove on
      it either, and the cell is kept so the buttons stay a column. -->
      <MemberRow
        v-for="one in team?.members ?? []"
        :key="one.userId"
        :tint="roleTint(one.roleName)"
        :live="!one.self"
        :drop-icon="one.self ? undefined : 'Trash2'"
        :drop-title="t('workspaces.removeMember')"
        @drop="drop(one.userId)"
      >
        <span class="member-name">
          {{ one.name || one.email }}<span v-if="one.self" class="member-you"> · {{
            t('workspaces.you') }}</span>
        </span>
        <template #role>
          <span v-if="one.self" class="role-word">{{ roleName(one.roleName) }}</span>
          <Select
            v-else
            :options="(team?.roles ?? []).map((role) => ({ value: role.id, label: roleName(role.name) }))"
            :model-value="one.roleId"
            @update:model-value="give(one.userId, $event)"
          />
        </template>
      </MemberRow>

      <!-- Invitations nobody has taken up yet, and how long each has been standing there. The address
      is what the letter went to, and an invitation to nobody in particular says that instead of drawing
      an empty row. -->
      <MemberRow
        v-for="one in team?.invites ?? []"
        :key="one.id"
        quiet
        :tint="roleTint(one.roleName ?? '')"
        drop-icon="Trash2"
        :drop-title="t('workspaces.revokeInvite')"
        @drop="revoke(one.id)"
      >
        <span class="member-column">
          <span class="member-name muted">{{ one.email || t('workspaces.inviteWaiting') }}</span>
          <span v-if="one.createdAt" class="member-since">
            {{ t('workspaces.inviteSince', { ago: formatAgo(one.createdAt) }) }}
          </span>
        </span>
        <template #role>
          <span class="role-word">{{ roleName(one.roleName ?? '') }}</span>
        </template>
      </MemberRow>

      <InviteRow
        v-for="row in drafts"
        :key="row.key"
        v-model:email="row.email"
        v-model:role-id="row.roleId"
        :roles="team?.roles ?? []"
        @enter="send(row)"
        @drop="dropDraft(row.key)"
      />
    </div>

    <InviteFoot :failure :busy="asking" @add="addDraft" />
  </template>
</template>

<style scoped>
@reference "../../style.css";

.card {
  @apply flex flex-col gap-px rounded-lg border border-border p-1;
}

.member-name {
  @apply min-w-0 text-12 overflow-hidden text-ellipsis whitespace-nowrap;
}

.member-name.muted {
  @apply text-text-tertiary;
}

.member-you {
  @apply text-text-tertiary;
}

.member-column {
  @apply flex flex-col min-w-0;
}

.member-since {
  @apply text-10 text-text-tertiary;
}

.role-word {
  @apply flex-1 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap;
}
</style>
