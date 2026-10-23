import assert from 'node:assert/strict'
import { test } from 'node:test'
import { vite } from './vite.mjs'

globalThis.window = new EventTarget()

const load = await vite({
  '/src/i18n/index.ts': 'export const describeFailure = (error) => `failed: ${error.message}`',
}, { inline: ['@wailsio/runtime'] })
const { useFailure } = await load('/src/composables/useFailure.ts')
const { CancelError } = await load('@wailsio/runtime/cancellable')

test('an answered call gives its answer and leaves no failure', async () => {
  const { failure, attempt } = useFailure()
  assert.equal(await attempt(async () => 42), 42)
  assert.equal(failure.value, '')
})

test('a refused call has no answer and says why', async () => {
  const { failure, attempt } = useFailure()
  assert.equal(await attempt(async () => { throw new Error('locked') }), undefined)
  assert.equal(failure.value, 'failed: locked')
})

test('the next call clears the last refusal before it starts', async () => {
  const { failure, attempt } = useFailure()
  await attempt(async () => { throw new Error('locked') })
  let seen
  await attempt(async () => { seen = failure.value })
  assert.equal(seen, '')
  assert.equal(failure.value, '')
})

test('a call that answers nothing says whether it went through', async () => {
  const { failure, run } = useFailure()
  assert.equal(await run(async () => {}), true)
  assert.equal(await run(async () => { throw new Error('locked') }), false)
  assert.equal(failure.value, 'failed: locked')
})

test('a call the window cancelled is not a refusal to word', async () => {
  const { failure, run } = useFailure()
  assert.equal(await run(async () => { throw new CancelError('cancelled') }), false)
  assert.equal(failure.value, '')
})
