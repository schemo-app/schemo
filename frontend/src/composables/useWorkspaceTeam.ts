import { computed, ref, watch, type Ref } from 'vue'
import { Members, type Team } from '../../bindings/schemo/internal/cloud'
import type { Workspace } from '../../bindings/schemo/internal/domain'
import { roleLabel } from '../lib/roles'
import { useAccount } from './useAccount'
import { useInviteDrafts, type InviteDraft } from './useInviteDrafts'
import { describeFailure, t, te } from '../i18n'

/** The shown workspace's team, read from the cloud (nothing is kept locally) and again after every change. */
export function useWorkspaceTeam(workspace: Ref<Workspace | null>, kind: Ref<string>) {

  const team = ref<Team | null>(null)
  const asking = ref(false)
  const failure = ref('')
  const { drafts, addDraft, dropDraft, clearDrafts } = useInviteDrafts()

  // Signed out, the cloud is not asked: it would only refuse.
  const { state } = useAccount()
  const signedOut = computed(() => state.value !== null && state.value.signedIn !== true)
  const linked = computed(
    () => kind.value === 'team' && !!workspace.value?.remoteId && !signedOut.value
  )
  const canManage = computed(() => workspace.value?.access?.manageMembers ?? false)
  const canInvite = computed(() => workspace.value?.access?.invite ?? false)

  const label = computed(() =>
    team.value
      ? t('workspaces.membersLabel', { count: team.value.members?.length ?? 0 })
      : t('workspaces.membersTeam')
  )

  // Only the latest question is drawn: an answer that arrives after the workspace on screen changed, or
  // after a newer question, is the team of somewhere else.
  let latestAsk = 0

  // Every change answers with the whole team, so the block is drawn from what the server just said.
  async function ask(call?: (workspaceId: string) => Promise<Team>) {
    const ticket = ++latestAsk
    const target = workspace.value
    if (!target || !linked.value) {
      team.value = null
      drafts.value = []
      failure.value = ''
      asking.value = false
      return
    }
    asking.value = true
    failure.value = ''
    try {
      const answer = await (call ?? Members.Team)(target.id)
      if (ticket === latestAsk) team.value = answer
    } catch (error) {
      if (ticket === latestAsk) failure.value = describeFailure(error)
      if (call && ticket === latestAsk) {
        // The write may have committed before refreshing the team failed.
        try {
          const answer = await Members.Team(target.id)
          if (ticket === latestAsk) team.value = answer
        } catch {
          // Keep the original failure until the server can answer again.
        }
      }
    } finally {
      if (ticket === latestAsk) asking.value = false
    }
  }

  // A string key, not an array: a new array per run would fire on every reload and wipe typed rows.
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
    team, asking, failure, signedOut, drafts, label, canManage, canInvite,
    addDraft, dropDraft, send, give, drop, revoke, roleName,
  }
}
