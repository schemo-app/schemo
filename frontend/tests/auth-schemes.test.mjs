import assert from 'node:assert/strict'
import { test } from 'node:test'
import { vite } from './vite.mjs'

let reads = 0
globalThis.draftService = {
  AuthSchemes: async () => {
    if (++reads === 1) throw new Error('the backend was not ready')
    return [{ type: 'bearer', label: 'auth.bearer' }]
  },
}

const load = await vite({
  '/bindings/schemo/internal/request/index.ts': 'export const Drafts = globalThis.draftService',
  '/src/i18n/index.ts': 'export const textOf = (key) => key',
})
const { useAuthSchemes } = await load('/src/composables/useAuthSchemes.ts')

const settled = () => new Promise((resolve) => setTimeout(resolve, 0))

test('a failed read of the schemes is asked for again by the next view', async () => {
  const first = useAuthSchemes()
  await settled()
  assert.deepEqual(first.offered(true), [])

  const second = useAuthSchemes()
  await settled()
  assert.deepEqual(second.offered(true).map((scheme) => scheme.type), ['bearer'])
})

test('the schemes are read once while the read holds', async () => {
  const before = reads
  useAuthSchemes()
  useAuthSchemes()
  await settled()
  assert.equal(reads, before)
})
