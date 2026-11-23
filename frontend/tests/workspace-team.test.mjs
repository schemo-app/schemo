import assert from 'node:assert/strict'
import { test } from 'node:test'
import { nextTick, ref } from 'vue'
import { vite } from './vite.mjs'

globalThis.window = new EventTarget()
globalThis.members = {}

const load = await vite({
  '/bindings/schemo/internal/cloud/index.ts': 'export const Members = {Team: (id) => globalThis.members.Team(id), Remove: (id, user) => globalThis.members.Remove(id, user)}',
  '/src/i18n/index.ts': `export const describeFailure = String;
    export const t = (key) => key; export const te = () => false`,
  '/src/lib/roles.ts': 'export const roleLabel = (t, te, name) => name',
  '/src/composables/useAccount.ts': `import { ref } from 'vue';
    export const useAccount = () => ({ state: ref({ signedIn: true }) })`,
})
const { useWorkspaceTeam } = await load('/src/composables/useWorkspaceTeam.ts')

test('a team that arrives after the workspace on screen has changed is not drawn', async () => {
  const answers = new Map()
  globalThis.members.Team = (id) => new Promise((resolve) => answers.set(id, resolve))

  const workspace = ref({ id: 'first', remoteId: 'r1' })
  const { team } = useWorkspaceTeam(workspace, ref('team'))
  await nextTick()

  workspace.value = { id: 'second', remoteId: 'r2' }
  await nextTick()

  answers.get('second')({ members: [{ id: 'b' }] })
  await Promise.resolve()
  answers.get('first')({ members: [{ id: 'a' }] })
  await new Promise((resolve) => setTimeout(resolve, 0))

  assert.deepEqual(team.value, { members: [{ id: 'b' }] })
})

test('a committed removal is refreshed even when its response fails', async () => {
  let people = [{userId:'owner'}, {userId:'guest'}]
  globalThis.members.Team = async () => ({members:[...people]})
  globalThis.members.Remove = async (workspace, user) => {
    assert.equal(workspace, 'team')
    people = people.filter(person=>person.userId !== user)
    throw new Error('the team response was lost')
  }
  const state = useWorkspaceTeam(ref({id:'team', remoteId:'remote'}), ref('team'))
  await new Promise(resolve=>setTimeout(resolve,0))
  assert.equal(state.team.value.members.length, 2)
  await state.drop('guest')
  assert.deepEqual(state.team.value.members, [{userId:'owner'}])
  assert.match(state.failure.value, /response was lost/)
})

test('regular members have no management or invitation controls', async () => {
  globalThis.members.Team = async () => ({members:[{userId:'guest'}]})
  const workspace = ref({id:'team', remoteId:'remote', access:{manageMembers:false, invite:false}})
  const state = useWorkspaceTeam(workspace, ref('team'))
  await nextTick()
  assert.equal(state.canManage.value, false)
  assert.equal(state.canInvite.value, false)
  workspace.value.access.manageMembers = true
  assert.equal(state.canManage.value, true)
  assert.equal(state.canInvite.value, false)
})
