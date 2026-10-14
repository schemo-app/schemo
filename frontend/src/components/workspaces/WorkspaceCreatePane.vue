<script setup lang="ts">
import { useColorLabels } from '../../composables/useColorLabels'
import { computed, onMounted, ref, watch } from 'vue'
import * as Members from '../../../bindings/schemo/internal/transport/wails/membersservice'
import { Code, type Role } from '../../../bindings/schemo/internal/domain'
import type { InviteOutcome } from '../../../bindings/schemo/internal/usecase/workspace'
import { Button } from '../ui/button'
import ColorSwatches from '../ui/ColorSwatches.vue'
import Segment from '../ui/Segment.vue'
import InviteFoot from './InviteFoot.vue'
import InviteRow from './InviteRow.vue'
import SignInGate from './SignInGate.vue'
import { WORKSPACE_COLORS, WORKSPACE_TINTS } from './palette'
import { useInviteDrafts } from '../../composables/workspace/useInviteDrafts'
import { useWorkspacesStore } from '../../stores/workspaces'
import { useSheetNotice } from '../../composables/useSheetNotice'
import { usePlan } from '../../composables/usePlan'
import { useAccount } from '../../composables/useAccount'
import { FEATURES, featureRefusal } from '../../lib/features'
import { describeFailure, useMessages } from '../../i18n'

// The form that makes a space: a name, a colour, what the space is meant to be, and — for one that is
// meant to be worked in together — the people to invite into it.
//
// The drawing has creation and settings as one screen, and this is why: a team's space is made with
// its first invitations written down, so it does not stop at a single email field. What makes that
// possible is that a team's space is made in the account at once rather than by the engine's next
// round — there is nobody to invite into a space the account has never heard of — which is also why
// making one needs the connection and the plan that carries it.
const emit = defineEmits<{ (e: 'cancel'): void; (e: 'created'): void }>()

const store = useWorkspacesStore()
const { notice, setNotice, clearNotice } = useSheetNotice()
const { includes } = usePlan()
const { t, te } = useMessages()

const { drafts, addDraft, dropDraft } = useInviteDrafts()

// The roles that may be handed out here, read the moment the form opens on a team space: the space
// they are for does not exist yet, so they are the organisation's own — see `Members.Roles`.
const roles = ref<Role[]>([])
const rolesFailed = ref('')

// Signed out is known only once the account has answered: until then the form does not close a door
// it may not need to, and the answer is asked for here in case nothing in the window has yet.
const { state, loadAccount } = useAccount()
const signedOut = computed(() => state.value !== null && state.value.signedIn !== true)

const mayShare = computed(() => includes(FEATURES.sharedSpaces))

const colorLabels = useColorLabels('workspaces', WORKSPACE_COLORS)

// The choice is drawn and not offered where the plan does not carry it: the server refuses it either
// way, and a door drawn open that answers with a refusal is worse than one that says why. Without a
// sign-in the choice stays open: what it needs is said in the tab itself, with the way in under it.
const kinds = computed(() => [
  { value: 'personal', label: t('workspaces.kindPersonal') },
  { value: 'team', label: t('workspaces.kindTeam'), soon: !mayShare.value },
])

const name = ref('')
const color = ref<string>(WORKSPACE_COLORS[0])
const kind = ref<string>('personal')

// A team's space chosen without an account: there is nowhere to make it and nobody to invite, so
// the tab says so instead of the invitations.
const needsAccount = computed(() => kind.value === 'team' && signedOut.value)

// A space that cannot be made is not offered to be made: the button is off whenever the server
// would refuse the team's space — no account, or a plan that does not carry it.
const mayCreate = computed(() => kind.value !== 'team' || (mayShare.value && !signedOut.value))

// The block is drawn for a team's space and for nothing else: nobody is invited into a space of
// somebody's own, and the server refuses it — the form does not offer what would be refused.
const asking = computed(() => kind.value === 'team' && mayShare.value && !signedOut.value)

// The roles are read when the block is first drawn rather than when the form opens: a person who is
// making a space of their own never needs them, and that is most people.
watch(asking, (wanted) => {
  if (wanted) void loadRoles()
})

onMounted(() => {
  if (state.value === null) void loadAccount()
  if (asking.value) void loadRoles()
})

async function loadRoles() {
  if (roles.value.length > 0) return
  rolesFailed.value = ''
  try {
    roles.value = (await Members.Roles()) ?? []
  } catch (error) {
    rolesFailed.value = describeFailure(error)
  }
}

async function create() {
  // Enter in the name field comes here too, past the button that is off.
  if (!mayCreate.value) return
  const invites = drafts.value
    .map((one) => ({ email: one.email.trim(), roleId: one.roleId }))
    .filter((one) => one.email !== '')

  let made: { id: string; invites: InviteOutcome[] }
  try {
    made = await store.create({ name: name.value, color: color.value, kind: kind.value, invites })
  } catch (error) {
    setNotice(failureText(error))
    return
  }

  // The space exists either way once the account has it, and a letter the server refused does not
  // undo it: the window says which address failed and hands the rest over — the space is on screen
  // with the invitations that went out already in its list.
  const refused = made.invites.filter((one) => one.failure)
  if (refused.length > 0) {
    setNotice(
      refused
        .map((one) => t('workspaces.inviteRefused', {
          email: one.email,
          reason: describeFailure({ cause: one.failure }),
        }))
        .join('; '),
    )
  } else {
    clearNotice()
  }
  emit('created')
}

// A refusal in words. The plan's own sentence names a feature, and the name on the wire is not a
// word anybody reads — `lib/features.ts` is where it becomes one.
function failureText(error: unknown): string {
  const failure = (error as { cause?: { code?: Code; args?: Record<string, string> } })?.cause
  if (failure?.code === Code.CodeFeatureRequired) {
    return featureRefusal(t, te, failure.args?.feature ?? '')
  }
  return describeFailure(error)
}

function cancel() {
  clearNotice()
  emit('cancel')
}
</script>

<template>
  <div class="pane">
    <div class="pane-body">
      <div class="pair">
        <label class="field">
          <span class="field-label">{{ t('workspaces.nameLabel') }}</span>
          <input
            v-model="name"
            class="input"
            maxlength="40"
            spellcheck="false"
            :placeholder="t('workspaces.namePlaceholder')"
            @keydown.enter="create"
          />
        </label>

        <div class="field tight">
          <span class="field-label">{{ t('workspaces.colorLabel') }}</span>
          <ColorSwatches
            :value="color"
            :colors="WORKSPACE_COLORS"
            :tints="WORKSPACE_TINTS"
            :labels="colorLabels"
            @pick="color = $event"
          />
        </div>
      </div>

      <div class="field">
        <span class="field-label">{{ t('workspaces.kindLabel') }}</span>
        <Segment
          class="kinds"
          :options="kinds"
          :value="kind"
          :soon-hint="t('workspaces.notInPlan')"
          @pick="kind = $event"
        />
      </div>

      <!-- What "Team" needs, said where its invitations would stand, and the way in under it. Once
           signed in, the invitations take its place. -->
      <SignInGate v-if="needsAccount" :note="t('workspaces.teamNeedsAccount')" />

      <!-- Who is invited while the space is being made. The block is the settings pane's own, drawn
           here so that a team's space does not begin with a single email field: the letters go out
           the moment the account has the space, and the role beside each address is what they will
           hold in it. -->
      <div v-if="asking" class="field">
        <span class="field-label">{{ t('workspaces.membersTeam') }}</span>

        <!-- An empty frame is not drawn: not invited yet is not a list but the line under it,
             which is where the first invitation is written. -->
        <div v-if="drafts.length" class="card">
          <InviteRow
            v-for="row in drafts"
            :key="row.key"
            v-model:email="row.email"
            v-model:role-id="row.roleId"
            :roles
            @enter="row.email.trim() && addDraft()"
            @drop="dropDraft(row.key)"
          />
        </div>

        <InviteFoot :failure="rolesFailed" @add="addDraft" />
      </div>
    </div>

    <div class="foot">
      <span class="notice" :class="{ bad: !!notice }">{{ notice }}</span>
      <Button size="pane" @click="cancel">{{ t('common.cancel') }}</Button>
      <Button size="pane" variant="primary" :disabled="!mayCreate" @click="create">
        {{ t('workspaces.createAction') }}
      </Button>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.pane {
  @apply flex-1 min-w-0 flex flex-col min-h-0;
}

.pane-body {
  @apply flex-1 min-h-0 overflow-y-auto flex flex-col gap-4.5;
  padding: 1.125rem 1.25rem;
}

.pair {
  @apply flex gap-3.5 items-end;
}

.field {
  @apply flex flex-col gap-1.5 min-w-0;
}

.pair > .field {
  @apply flex-1;
}

.pair > .field.tight {
  @apply flex-none;
}

.field-label {
  @apply section-label;
}

.input {
  @apply w-full h-8.5 px-2.5 rounded-lg text-13 text-text outline-none;
  font-family: inherit;
  background: var(--bg-panel);
  border: 1px solid var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.kinds {
  max-width: 16.25rem;
}

.card {
  @apply flex flex-col gap-px rounded-lg border border-border p-1;
}

.foot {
  @apply flex-none flex items-center h-12 px-5 gap-2.5;
  border-top: 1px solid var(--border);
}

.notice {
  @apply flex-1 min-w-0 text-13 text-text-tertiary;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.notice.bad {
  @apply text-red;
}

</style>
