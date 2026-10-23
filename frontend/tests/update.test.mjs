import assert from 'node:assert/strict'
import { after, test } from 'node:test'
import { createRenderer, nextTick } from 'vue'
import english from '../src/i18n/locales/en.json' with { type: 'json' }
import { vite } from './vite.mjs'

globalThis.window = new EventTarget()
globalThis.document = { createElement: () => ({}) }
globalThis.updateListeners = new Map()
globalThis.updateFailureTexts = english.errors.codes
globalThis.updateService = {
  Status: async () => ({ latest: '1.1.0', available: true, checkedAt: 1 }),
}

const load = await vite({
  '/bindings/schemo/internal/desktop/index.ts': 'export const Updates = globalThis.updateService',
  '/src/composables/useWailsEvent.ts': `import { onScopeDispose } from 'vue';
    export function useWailsEvent(name, handler) {
      globalThis.updateListeners.set(name, handler);
      onScopeDispose(() => globalThis.updateListeners.delete(name));
    }`,
  '/src/i18n/index.ts': `export const describeFailure = String;
    export const failureText = failure => globalThis.updateFailureTexts[failure?.code] ?? null;
    export const t = (key, params) => params?.error ?? key;`,
})
const { useUpdateCheck } = await load('/src/composables/useUpdateCheck.ts')

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

test('read-only installation failures from updater events use the application catalogue', async (context) => {
  const state = mountUpdate(context)
  await nextTick()
  globalThis.updateListeners.get('wails:updater:error')({ data: { stage: 'install', message: 'updateReadOnly' } })
  assert.equal(state.phase.value, 'error')
  assert.equal(state.error.value, english.errors.codes.updateReadOnly)
})
