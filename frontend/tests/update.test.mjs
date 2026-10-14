import assert from 'node:assert/strict'
import { after, test } from 'node:test'
import { createRenderer, nextTick } from 'vue'
import { createServer } from 'vite'

globalThis.window = new EventTarget()
globalThis.document = { createElement: () => ({}) }
globalThis.updateListeners = new Map()
globalThis.updateService = {
  Status: async () => ({ latest: '1.1.0', available: true, checkedAt: 1 }),
}

const vite = await createServer({
  configFile: false,
  optimizeDeps: { noDiscovery: true, include: [] },
  server: { middlewareMode: true, watch: null, hmr: false, ws: false },
  appType: 'custom',
  plugins: [{
    name: 'update-ports',
    load(id) {
      const path = id.replaceAll('\\', '/')
      if (path.endsWith('/bindings/schemo/internal/transport/wails/index.ts')) {
        return 'export const UpdateService = globalThis.updateService'
      }
      if (path.endsWith('/src/composables/events/useWailsEvent.ts')) {
        return `import { onScopeDispose } from 'vue';
          export function useWailsEvent(name, handler) {
            globalThis.updateListeners.set(name, handler);
            onScopeDispose(() => globalThis.updateListeners.delete(name));
          }`
      }
      if (path.endsWith('/src/i18n.ts')) {
        return 'export const describeFailure = String; export const t = (key, params) => params?.error ?? key'
      }
    },
  }],
})
after(() => vite.close())
const { useUpdateCheck } = await vite.ssrLoadModule('/src/composables/useUpdateCheck.ts')

function mountUpdate(context) {
  let state
  const renderer = createRenderer({
    createComment: () => ({}), insert() {}, remove() {},
    parentNode: () => null, nextSibling: () => null,
  })
  const app = renderer.createApp({
    setup() {
      state = useUpdateCheck()
      return () => null
    },
  })
  app.mount({})
  context.after(() => app.unmount())
  return state
}

test('Wails download and failure events synchronize a window that did not start installation', async (context) => {
  const state = mountUpdate(context)
  await nextTick()
  assert.equal(state.phase.value, 'available')
  globalThis.updateListeners.get('wails:updater:download-started')({})
  assert.equal(state.phase.value, 'installing')

  globalThis.updateListeners.get('update-changed')({ data: { checkedAt: 2, available: true } })
  assert.equal(state.phase.value, 'installing')

  globalThis.updateListeners.get('wails:updater:error')({ data: { stage: 'verify', message: 'Invalid signature' } })
  assert.equal(state.phase.value, 'error')
  assert.equal(state.error.value, 'Invalid signature')
})

test('background check failures do not replace the last known update in another window', async (context) => {
  const state = mountUpdate(context)
  await nextTick()
  globalThis.updateListeners.get('wails:updater:error')({ data: { stage: 'check', message: 'Offline' } })
  assert.equal(state.phase.value, 'available')
  assert.equal(state.error.value, '')
})
