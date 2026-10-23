import assert from 'node:assert/strict'
import { test } from 'node:test'
import { vite } from './vite.mjs'

globalThis.window = {}
globalThis.document = { createElement: () => ({}) }

const load = await vite({
  '/src/stores/environments.ts': 'export function useEnvironmentsStore() { throw new Error("Unexpected environment interaction") }',
})
const { nextTick } = await import('vue')
const { vVarPeek, hoveredVar } = await load('/src/lib/varPeek.ts')

function fieldFor(token) {
  const field = new EventTarget()
  field.parentElement = { querySelectorAll: () => [token] }
  return field
}

function move(field) {
  const event = new Event('mousemove')
  Object.assign(event, { clientX: 5, clientY: 5 })
  field.dispatchEvent(event)
}

test('removing a field cancels its pending variable tooltip', async (context) => {
  context.mock.timers.enable({ apis: ['setTimeout'] })
  const token = { getBoundingClientRect: () => ({ left: 0, top: 0, right: 10, bottom: 10 }) }
  const field = fieldFor(token)
  vVarPeek.mounted(field, {})
  await nextTick()
  move(field)
  vVarPeek.unmounted(field)
  context.mock.timers.tick(200)
  assert.equal(hoveredVar.value, null)
})

test('removing a field clears its tooltip and detaches listeners', async (context) => {
  context.mock.timers.enable({ apis: ['setTimeout'] })
  const token = { getBoundingClientRect: () => ({ left: 0, top: 0, right: 10, bottom: 10 }) }
  const field = fieldFor(token)
  vVarPeek.mounted(field, {})
  await nextTick()
  move(field)
  context.mock.timers.tick(200)
  assert.equal(hoveredVar.value, token)
  vVarPeek.unmounted(field)
  assert.equal(hoveredVar.value, null)
  move(field)
  context.mock.timers.tick(200)
  assert.equal(hoveredVar.value, null)
})
