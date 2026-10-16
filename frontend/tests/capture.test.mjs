import assert from 'node:assert/strict'
import { after, test } from 'node:test'
import { createRenderer, createSSRApp, nextTick, reactive, ref } from 'vue'
import { renderToString } from 'vue/server-renderer'
import vue from '@vitejs/plugin-vue'
import { createServer } from 'vite'

globalThis.captureListeners = new Map()
globalThis.captureStore = reactive({
  records: [],
  historySource: 'all',
  historyQuery: '',
  sessionFilter: '',
  capture: { connected: false, recording: false, paused: false, tabs: 0, tabList: [] },
  setCaptureState(state) { Object.assign(this.capture, state) },
  applyCaptureFilters: async () => {},
})
globalThis.captureSettings = { settings: ref({}) }
globalThis.captureBridge = { Status: async () => ({ connected: false }), Listening: async () => true }

const vite = await createServer({
  configFile: false,
  optimizeDeps: { noDiscovery: true, include: [] },
  server: { middlewareMode: true, watch: null, hmr: false, ws: false },
  appType: 'custom',
  plugins: [vue(), {
    name: 'capture-ports',
    enforce: 'pre',
    load(id) {
      const path = id.replaceAll('\\', '/')
      if (path.endsWith('/bindings/schemo/internal/transport/wails/index.ts')) {
        return 'export const BridgeService = globalThis.captureBridge; export const RecordsService = {}'
      }
      if (path.endsWith('/bindings/schemo/internal/domain/index.ts')) {
        return 'export const RecordSource = {SourceBrowser: "browser"}'
      }
      if (path.endsWith('/src/stores/records.ts')) {
        return 'export const useRecordsStore = () => globalThis.captureStore'
      }
      if (path.endsWith('/src/composables/events/useWailsEvent.ts')) {
        return `import { onScopeDispose } from 'vue';
          export function useWailsEvent(name, handler) {
            globalThis.captureListeners.set(name, handler);
            onScopeDispose(() => globalThis.captureListeners.delete(name));
          }`
      }
      if (path.endsWith('/src/composables/useSettings.ts')) {
        return 'export const useSettings = () => globalThis.captureSettings; export const useSetting = () => ({value: {hosts: []}})'
      }
      if (path.endsWith('/src/composables/useToast.ts')) return 'export const useToast = () => ({show() {}})'
      if (path.endsWith('/src/i18n.ts')) {
        return `export const t = (key, params) => key === 'history.requestsN' ? key + ':' + params?.n : key;
          export const useMessages = () => ({t}); export const describeFailure = String;
          export const formatDate = () => 'today'; export const formatDay = () => 'today';
          export const formatClock = () => '12:00'; export const formatBytes = () => '12 B';
          export const formatDuration = () => '10 ms'; export const formatStatus = String;
          export const daysAgo = () => 0;`
      }
      if (path.endsWith('/node_modules/@wailsio/runtime/dist/index.js')) return 'export const Browser = {OpenURL() {}}'
      if (path.endsWith('.vue') && !path.endsWith('/browser/CapturePage.vue') && !path.endsWith('/history/HistoryPage.vue') && !path.endsWith('/ui/button/Button.vue')) {
        return '<script>export default {render() {return null}}</script>'
      }
    },
  }],
})
after(() => vite.close())
const { useCaptureEvents } = await vite.ssrLoadModule('/src/composables/events/useCaptureEvents.ts')
const { captureSessions } = await vite.ssrLoadModule('/src/lib/recordTabs.ts')
Object.defineProperties(globalThis.captureStore, {
  browserRecords: { get() { return this.records.filter(record => record.source === 'browser') } },
  captureSessions: { get() { return captureSessions(this.browserRecords, this.capture.tabList) } },
})
const { default: CapturePage } = await vite.ssrLoadModule('/src/components/browser/CapturePage.vue')
const { default: HistoryPage } = await vite.ssrLoadModule('/src/components/history/HistoryPage.vue')

function mountEvents(context) {
  const renderer = createRenderer({
    createComment: () => ({}), insert() {}, remove() {},
    parentNode: () => null, nextSibling: () => null,
  })
  const app = renderer.createApp({
    setup() { useCaptureEvents(globalThis.captureStore); return () => null },
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
  assert.equal(globalThis.captureStore.capture.connected, true)
  assert.equal(globalThis.captureStore.capture.recording, false)
  assert.equal(globalThis.captureStore.capture.browser, 'Firefox')
})

test('a late startup status cannot restore a connection that has already ended', async context => {
  let resolve
  globalThis.captureBridge.Status = () => new Promise(done => { resolve = done })
  mountEvents(context)
  globalThis.captureListeners.get('capture-disconnected')()
  resolve({ connected: true, state: { recording: true, tabs: 1 } })
  await nextTick()
  assert.equal(globalThis.captureStore.capture.connected, false)
  assert.equal(globalThis.captureStore.capture.recording, false)
})

test('saved capture sessions remain visible and exportable without a connected extension', async () => {
  globalThis.captureStore.records = [{
    id: 'record-1', source: 'browser', tabId: 7, tabTitle: 'Saved tab',
    tabURL: 'https://example.com', url: 'https://example.com/api', method: 'GET', status: 200, startedAt: 1,
  }]
  globalThis.captureStore.capture.connected = false
  const html = await renderToString(createSSRApp(CapturePage))
  assert.match(html, /Saved tab/)
  assert.match(html, /files.exportHar/)
  assert.doesNotMatch(html, /capture.waiting/)
})

test('an idle connected extension shows the session list rather than installation instructions', async () => {
  globalThis.captureStore.records = []
  Object.assign(globalThis.captureStore.capture, { connected: true, recording: false, paused: false, tabs: 0 })
  const html = await renderToString(createSSRApp(CapturePage))
  assert.match(html, /extension.idle/)
  assert.match(html, /capture.noSessions/)
  assert.doesNotMatch(html, /capture.waiting/)
})

test('an armed tab is visible with zero requests and merges with its first captured request', async () => {
  globalThis.captureStore.records = []
  Object.assign(globalThis.captureStore.capture, {
    connected: true, recording: true, tabs: 1,
    tabList: [{ tabId: 7, since: 1, title: 'New tab', url: 'https://example.com/products', paused: false }],
  })
  const empty = await renderToString(createSSRApp(CapturePage))
  assert.match(empty, /New tab/)
  assert.equal(globalThis.captureStore.captureSessions[0].url, 'https://example.com/products')
  assert.match(empty, /history.requestsN:0/)
  assert.match(empty, /(?:cap-dot live|live cap-dot)/)
  assert.match(empty, /capture.noSessions/)

  globalThis.captureStore.records = [{
    id: 'record-1', source: 'browser', tabId: 7, tabTitle: 'New tab',
    tabURL: 'https://example.com/products', url: 'https://example.com/api', method: 'GET', status: 200, startedAt: 2,
  }]
  const captured = await renderToString(createSSRApp(CapturePage))
  assert.equal((captured.match(/class="cap-chip-name"/g) ?? []).length, 1)
  assert.match(captured, /history.requestsN:1/)

  Object.assign(globalThis.captureStore.capture, { connected: false, tabs: 0, tabList: [] })
  const disconnected = await renderToString(createSSRApp(CapturePage))
  assert.match(disconnected, /New tab/)
  assert.doesNotMatch(disconnected, /(?:cap-dot live|live cap-dot)/)
})

test('history shows the selected tab pause state and keeps the resume action visible', async () => {
  globalThis.captureStore.records = []
  globalThis.captureStore.sessionFilter = '7'
  Object.assign(globalThis.captureStore.capture, {
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
  globalThis.captureStore.records = [{ id: 'r1', source: 'browser', tabId: 7, tabTitle: 'Finished tab', url: 'https://example.com', method: 'GET', status: 200, startedAt: 1 }]
  globalThis.captureStore.sessionFilter = '7'
  Object.assign(globalThis.captureStore.capture, { connected: true, recording: true, tabList: [{ tabId: 8, since: 1, paused: false }] })
  const html = await renderToString(createSSRApp(HistoryPage))
  assert.doesNotMatch(html, /class="hist-rec"/)
})
