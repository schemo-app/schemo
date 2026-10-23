import assert from 'node:assert/strict'
import { test } from 'node:test'
import { effectScope, nextTick, ref } from 'vue'
import { vite } from './vite.mjs'

globalThis.scriptingPort = {}

const load = await vite({
  '/bindings/schemo/internal/script/index.ts': 'export const Scripts = globalThis.scriptingPort',
})
const { useScriptRuns } = await load('/src/composables/useScriptRuns.ts')

function deferred() {
  let resolve, reject
  const promise = new Promise((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}

function mount(recordId) {
  const scope = effectScope()
  const runs = scope.run(() => useScriptRuns(() => recordId.value))
  return { ...runs, stop: () => scope.stop() }
}

test('the runs of the record on screen are read, and loading ends', async () => {
  globalThis.scriptingPort.Runs = async (id) => [{ id: `run-of-${id}` }]
  const recordId = ref('a')
  const { runs, isLoading, stop } = mount(recordId)
  assert.equal(isLoading.value, true)
  await nextTick()
  await Promise.resolve()
  assert.deepEqual(runs.value, [{ id: 'run-of-a' }])
  assert.equal(isLoading.value, false)
  stop()
})

test('a read that lands after the record changed does not replace the newer one', async () => {
  const older = deferred()
  globalThis.scriptingPort.Runs = (id) => (id === 'old' ? older.promise : Promise.resolve([{ id: 'new-run' }]))
  const recordId = ref('old')
  const { runs, isLoading, stop } = mount(recordId)
  recordId.value = 'new'
  await nextTick()
  await new Promise((done) => setTimeout(done, 0))
  older.resolve([{ id: 'old-run' }])
  await new Promise((done) => setTimeout(done, 0))
  assert.deepEqual(runs.value, [{ id: 'new-run' }])
  assert.equal(isLoading.value, false)
  stop()
})

test('a read that fails leaves nothing and stops loading', async () => {
  globalThis.scriptingPort.Runs = async () => { throw new Error('unreadable') }
  const recordId = ref('a')
  const { runs, isLoading, stop } = mount(recordId)
  await new Promise((done) => setTimeout(done, 0))
  assert.deepEqual(runs.value, [])
  assert.equal(isLoading.value, false)
  stop()
})
