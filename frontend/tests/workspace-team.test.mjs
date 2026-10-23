import assert from 'node:assert/strict'
import { test } from 'node:test'
import { nextTick, ref } from 'vue'
import { vite } from './vite.mjs'

globalThis.window = new EventTarget()
globalThis.members = {}

const load = await vite({
  '/bindings/schemo/internal/cloud/index.ts': 'export const Members = {Team: (id) => globalThis.members.Team(id)}',
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
