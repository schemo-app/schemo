import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createPinia, setActivePinia } from 'pinia'
import { vite } from './vite.mjs'

globalThis.window = new EventTarget()
globalThis.transferService = {}

const load = await vite({
  '/bindings/schemo/internal/convert/index.ts': 'export const Transfer = globalThis.transferService',
  '/src/i18n/index.ts': 'export const t = (key) => key',
  '/src/composables/useToast.ts': 'export const useToast = () => ({ show() {} })',
  '/src/composables/useGettingStarted.ts': 'export function markStep() {}',
  '/src/lib/clipboard.ts': 'export async function copyToClipboard() {}',
  '/src/lib/calls.ts': 'export const callOrToast = async (call) => call.catch(() => undefined)',
  '/src/stores/collections.ts': 'export const useCollectionsStore = () => ({})',
  '/src/stores/environments.ts': 'export const useEnvironmentsStore = () => ({})',
  '/src/stores/workspaces.ts': 'export const useWorkspacesStore = () => ({})',
})
const { useTransferStore } = await load('/src/stores/transfer.ts')

test('the dialogs stay busy until the last of several reads has come back', async () => {
  setActivePinia(createPinia())
  const transfer = useTransferStore()
  const answers = []
  globalThis.transferService.PreviewText = () => new Promise((resolve) => answers.push(resolve))

  const first = transfer.previewText('curl', 'curl a', '', true)
  const second = transfer.previewText('curl', 'curl ab', '', true)
  assert.equal(transfer.busy, true)

  answers[0]({ token: 'first' })
  assert.deepEqual(await first, { token: 'first' })
  assert.equal(transfer.busy, true)

  answers[1]({ token: 'second' })
  await second
  assert.equal(transfer.busy, false)
})

test('a quiet read that fails answers nothing and says nothing', async () => {
  setActivePinia(createPinia())
  const transfer = useTransferStore()
  globalThis.transferService.PreviewText = () => Promise.reject(new Error('not a command yet'))

  assert.equal(await transfer.previewText('curl', 'cur', '', true), null)
  assert.equal(transfer.busy, false)
})
