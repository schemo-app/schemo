import { computed, ref, watch, type Ref } from 'vue'
import * as Members from '../../../bindings/schemo/internal/transport/wails/membersservice'
import type { Team } from '../../../bindings/schemo/internal/usecase/members/models'
import type { Workspace } from '../../../bindings/schemo/internal/domain'
import { roleLabel } from '../../lib/roles'
import { useAccount } from '../useAccount'
import { useInviteDrafts, type InviteDraft } from './useInviteDrafts'
import { describeFailure, useMessages } from '../../i18n'

/**
 * The team of the space on screen: who is in it, what is waiting to let more in, and the roles that may
 * be handed out. It is the cloud's answer, not this device's — no membership is kept beside the
 * collections — so it is read when a space is shown and read again after every change.
 */
export function useWorkspaceTeam(workspace: Ref<Workspace | null>, kind: Ref<string>) {
  const { t, te } = useMessages()

  const team = ref<Team | null>(null)
  const asking = ref(false)
  const failure = ref('')
  const { drafts, addDraft, dropDraft, clearDrafts } = useInviteDrafts()

  // A space not in the account has no team and nothing to make a link in; a space of somebody's own
  // holds one person, and the server refuses every change to it. Without a sign-in the cloud is not
  // asked at all: its answer would be a refusal, and the block says why instead.
  const { state } = useAccount()
  const signedOut = computed(() => state.value !== null && state.value.signedIn !== true)
  const linked = computed(
    () => kind.value === 'team' && !!workspace.value?.remoteId && !signedOut.value
  )

  const label = computed(() =>
    team.value
      ? t('workspaces.membersLabel', { count: team.value.members?.length ?? 0 })
      : t('workspaces.membersTeam')
  )

  // Every change answers with the whole team, so the block is drawn from what the server just said.
  async function ask(call?: (spaceId: string) => Promise<Team>) {
    const target = workspace.value
    if (!target || !linked.value) {
      team.value = null
      drafts.value = []
      failure.value = ''
      return
    }
    asking.value = true
    failure.value = ''
    try {
      team.value = await (call ?? Members.Team)(target.id)
    } catch (error) {
      failure.value = describeFailure(error)
    } finally {
      asking.value = false
    }
  }

  // Which space, whether the account has it yet, and whether there is a sign-in to ask with — the
  // second moves on its own when the engine links a space made here, the third when somebody signs
  // in or out. A string rather than an array: a getter building a new array on every run would fire
  // on every reload of the list and wipe rows somebody is writing.
  watch(
    () => [workspace.value?.id, workspace.value?.remoteId, kind.value, linked.value].join('|'),
    () => {
      clearDrafts()
      void ask()
    },
    { immediate: true }
  )

  // Sent on Enter once the address is finished: a row that sent itself on the way to the role list
  // would be one nobody could fill in.
  async function send(row: InviteDraft) {
    const address = row.email.trim()
    if (!address) return
    await ask(async (id) => {
      const held = await Members.Invite(id, address, row.roleId)
      dropDraft(row.key)
      return held
    })
  }

  const give = (userId: string, roleId: string) =>
    roleId ? ask((id) => Members.SetRole(id, userId, roleId)) : Promise.resolve()
  const drop = (userId: string) => ask((id) => Members.Remove(id, userId))
  const revoke = (inviteId: string) => ask((id) => Members.Revoke(id, inviteId))

  // The product's own roles are worded in the catalogue; a role the organisation wrote is drawn as written.
  const roleName = (name: string) => roleLabel(t, te, name)

  return {
    team, asking, failure, signedOut, drafts, label,
    addDraft, dropDraft, send, give, drop, revoke, roleName,
  }
}
