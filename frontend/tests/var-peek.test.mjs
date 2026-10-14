import assert from 'node:assert/strict'
import { after, test } from 'node:test'
import { createServer } from 'vite'

globalThis.window = {}
globalThis.document = { createElement: () => ({}) }

const vite = await createServer({
  configFile: false,
  optimizeDeps: { noDiscovery: true, include: [] },
  server: { middlewareMode: true, watch: null, hmr: false, ws: false },
  appType: 'custom',
  plugins: [{
    name: 'environment-port',
    load(id) {
      if (id.replaceAll('\\', '/').endsWith('/src/stores/environments.ts')) {
        return 'export function useEnvironmentsStore() { throw new Error("Unexpected environment interaction") }'
      }
    },
  }],
})
after(() => vite.close())
const { nextTick } = await import('vue')
const { vVarPeek, hoveredVar } = await vite.ssrLoadModule('/src/composables/useVarPeek.ts')

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
