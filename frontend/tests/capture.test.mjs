import assert from 'node:assert/strict'
import { after, test } from 'node:test'
import { createRenderer, createSSRApp, nextTick, reactive, ref } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { vite } from './vite.mjs'

globalThis.captureListeners = new Map()
globalThis.recordsStore = reactive({
  records: [],
  historySource: 'all',
  historyQuery: '',
  sessionFilter: '',
})
globalThis.captureStore = reactive({
  connected: false,
  recording: false,
  browser: '',
  tabList: [],
  adopt(state) {
    Object.assign(this, { connected: true, recording: state.recording, browser: state.browser, tabList: state.tabList ?? [] })
  },
  disconnect() {
    Object.assign(this, { connected: false, recording: false, browser: '', tabList: [] })
  },
  applyFilters: async () => {},
  toggle: async () => {},
})
globalThis.captureSettings = { settings: ref({}) }
globalThis.captureBridge = { Status: async () => ({ connected: false }), Listening: async () => true }

// Only the capture and history pages and Button are real; every other component draws nothing.
const realComponents = ['/history/CapturePage.vue', '/history/HistoryPage.vue', '/components/ui/Button.vue']
const load = await vite({
  '/bindings/schemo/internal/agent/index.ts': 'export const Extension = globalThis.captureBridge',
  '/bindings/schemo/internal/domain/index.ts': 'export const RecordSource = {SourceBrowser: "browser"}',
  '/bindings/schemo/internal/convert/index.ts': 'export const Transfer = {}',
  '/src/stores/records.ts': 'export const useRecordsStore = () => globalThis.recordsStore',
  '/src/stores/capture.ts': 'export const useCaptureStore = () => globalThis.captureStore',
  // useAppEvents follows every feature; these tests exercise only the extension's part.
  '/src/stores/sync.ts': 'export const useSyncStore = () => ({})',
  '/src/stores/content.ts': 'export const ownerOf = () => undefined',
  '/src/lib/earlyAnswers.ts': 'export const holdAnswer = () => {}',
  '/src/stores/collections.ts': 'export const useCollectionsStore = () => ({})',
  '/src/stores/cookies.ts': 'export const useCookiesStore = () => ({})',
  '/src/stores/environments.ts': 'export const useEnvironmentsStore = () => ({})',
  '/src/stores/transfer.ts': 'export const useTransferStore = () => ({})',
  '/src/stores/workspaces.ts': 'export const useWorkspacesStore = () => ({})',
  '/src/composables/useWailsEvent.ts': `import { onScopeDispose } from 'vue';
    export function useWailsEvent(name, handler) {
      globalThis.captureListeners.set(name, handler);
      onScopeDispose(() => globalThis.captureListeners.delete(name));
    }`,
  '/src/composables/useSettings.ts': 'export const useSettings = () => globalThis.captureSettings; export const useSetting = () => ({value: {hosts: []}})',
  '/src/composables/useToast.ts': 'export const useToast = () => ({show() {}})',
  '/src/i18n/index.ts': `export const t = (key, params) => key === 'history.requestsN' ? key + ':' + params?.n : key;
    export const describeFailure = String;
    export const formatDate = () => 'today'; export const formatDay = () => 'today';
    export const formatClock = () => '12:00'; export const formatBytes = () => '12 B';
    export const formatDuration = () => '10 ms'; export const formatStatus = String;
    export const daysAgo = () => 0;`,
  '/node_modules/@wailsio/runtime/dist/index.js': 'export const Browser = {OpenURL() {}}',
}, {
  vue: true,
  other: (path) => path.endsWith('.vue') && !realComponents.some((real) => path.endsWith(real))
    ? '<script>export default {render() {return null}}</script>'
    : undefined,
})
const { useCaptureEvents } = await load('/src/composables/useAppEvents.ts')
const { captureSessions } = await load('/src/lib/recordTabs.ts')
Object.defineProperty(globalThis.recordsStore, 'browserRecords', {
  get() { return this.records.filter(record => record.source === 'browser') },
})
Object.defineProperty(globalThis.captureStore, 'sessions', {
  get() { return captureSessions(globalThis.recordsStore.browserRecords, this.tabList) },
})
const { default: CapturePage } = await load('/src/components/history/CapturePage.vue')
const { default: HistoryPage } = await load('/src/components/history/HistoryPage.vue')

function mountEvents(context) {
  const renderer = createRenderer({
    createComment: () => ({}), insert() {}, remove() {},
    parentNode: () => null, nextSibling: () => null,
  })
  const app = renderer.createApp({
    setup() { useCaptureEvents(); return () => null },
  })
  app.mount({})
  context.after(() => app.unmount())
}

test('an idle extension connected before mount is restored without waiting for a capture event', async context => {
  globalThis.captureBridge.Status = async () => ({
    connected: true, state: { browser: 'Firefox', recording: false, paused: false, tabs: 0 },
  })
  mountEvents(context)
  await nextTick()
  assert.equal(globalThis.captureStore.connected, true)
  assert.equal(globalThis.captureStore.recording, false)
  assert.equal(globalThis.captureStore.browser, 'Firefox')
})

test('a late startup status cannot restore a connection that has already ended', async context => {
  let resolve
  globalThis.captureBridge.Status = () => new Promise(done => { resolve = done })
  mountEvents(context)
  globalThis.captureListeners.get('capture-disconnected')()
  resolve({ connected: true, state: { recording: true, tabs: 1 } })
  await nextTick()
  assert.equal(globalThis.captureStore.connected, false)
  assert.equal(globalThis.captureStore.recording, false)
})

test('saved capture sessions remain visible and exportable without a connected extension', async () => {
  globalThis.recordsStore.records = [{
    id: 'record-1', source: 'browser', tabId: 7, tabTitle: 'Saved tab',
    tabURL: 'https://example.com', url: 'https://example.com/api', method: 'GET', status: 200, startedAt: 1,
  }]
  globalThis.captureStore.connected = false
  const html = await renderToString(createSSRApp(CapturePage))
  assert.match(html, /Saved tab/)
  assert.match(html, /files.exportHar/)
  assert.doesNotMatch(html, /capture.waiting/)
})

test('an idle connected extension shows the session list rather than installation instructions', async () => {
  globalThis.recordsStore.records = []
  Object.assign(globalThis.captureStore, { connected: true, recording: false, paused: false, tabs: 0 })
  const html = await renderToString(createSSRApp(CapturePage))
  assert.match(html, /extension.idle/)
  assert.match(html, /capture.noSessions/)
  assert.doesNotMatch(html, /capture.waiting/)
})

test('an armed tab is visible with zero requests and merges with its first captured request', async () => {
  globalThis.recordsStore.records = []
  Object.assign(globalThis.captureStore, {
    connected: true, recording: true, tabs: 1,
    tabList: [{ tabId: 7, since: 1, title: 'New tab', url: 'https://example.com/products', paused: false }],
  })
  const empty = await renderToString(createSSRApp(CapturePage))
  assert.match(empty, /New tab/)
  assert.equal(globalThis.captureStore.sessions[0].url, 'https://example.com/products')
  assert.match(empty, /history.requestsN:0/)
  assert.match(empty, /(?:cap-dot live|live cap-dot)/)
  assert.match(empty, /capture.noSessions/)

  globalThis.recordsStore.records = [{
    id: 'record-1', source: 'browser', tabId: 7, tabTitle: 'New tab',
    tabURL: 'https://example.com/products', url: 'https://example.com/api', method: 'GET', status: 200, startedAt: 2,
  }]
  const captured = await renderToString(createSSRApp(CapturePage))
  assert.equal((captured.match(/class="cap-chip-name"/g) ?? []).length, 1)
  assert.match(captured, /history.requestsN:1/)

  Object.assign(globalThis.captureStore, { connected: false, tabs: 0, tabList: [] })
  const disconnected = await renderToString(createSSRApp(CapturePage))
  assert.match(disconnected, /New tab/)
  assert.doesNotMatch(disconnected, /(?:cap-dot live|live cap-dot)/)
})

test('history shows the selected tab pause state and keeps the resume action visible', async () => {
  globalThis.recordsStore.records = []
  globalThis.recordsStore.sessionFilter = '7'
  Object.assign(globalThis.captureStore, {
    connected: true, recording: true, tabs: 2, tabList: [
      { tabId: 7, since: 1, title: 'Paused tab', paused: true },
      { tabId: 8, since: 1, title: 'Recording tab', paused: false },
    ],
  })
  const html = await renderToString(createSSRApp(HistoryPage))
  assert.match(html, /Paused tab/)
  assert.match(html, /(?:hist-rec paused|paused hist-rec)/)
  assert.match(html, /history.resume/)
  assert.doesNotMatch(html, /capture.pauseAll/)
})

test('history does not offer capture controls for a finished tab session', async () => {
  globalThis.recordsStore.records = [{ id: 'r1', source: 'browser', tabId: 7, tabTitle: 'Finished tab', url: 'https://example.com', method: 'GET', status: 200, startedAt: 1 }]
  globalThis.recordsStore.sessionFilter = '7'
  Object.assign(globalThis.captureStore, { connected: true, recording: true, tabList: [{ tabId: 8, since: 1, paused: false }] })
  const html = await renderToString(createSSRApp(HistoryPage))
  assert.doesNotMatch(html, /class="hist-rec"/)
})
