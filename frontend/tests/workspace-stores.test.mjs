import assert from 'node:assert/strict'
import { after, afterEach, test } from 'node:test'
import { resolve } from 'node:path'
import { createServer } from 'vite'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { createRenderer } from 'vue'

globalThis.window = new EventTarget()
globalThis.document = Object.assign(new EventTarget(), { createElement: () => ({}) })
globalThis.settingsPort = { settings: { value: null } }
globalThis.environmentServices = {}
globalThis.cookieServices = {}
globalThis.tabServices = {}
globalThis.draftServices = {}
globalThis.tabContent = {}
globalThis.collectionServices = {}
globalThis.recordServices = {}
globalThis.workspaceServices = {}
globalThis.publicationServices = {}
globalThis.notices = []

const runtimePath = resolve('node_modules/@wailsio/runtime/dist').replaceAll('\\', '/')
const runtime = `export { CancellablePromise, CancelError } from '${runtimePath}/cancellable.js';
export * as Create from '${runtimePath}/create.js';`
const vite = await createServer({
  configFile: false,
  ssr: { noExternal: ['@wailsio/runtime'] },
  optimizeDeps: { noDiscovery: true, include: [] },
  server: { middlewareMode: true, watch: null, hmr: false, ws: false },
  appType: 'custom',
  plugins: [{
    name: 'workspace-ports',
    resolveId(id) {
      if (id === '@wailsio/runtime' || id === 'test:wails') return '\0test:wails'
    },
    load(id) {
      if (id === '\0test:wails') return runtime
      const path = id.replaceAll('\\', '/')
      if (path.endsWith('/@wailsio/runtime/dist/index.js')) return runtime
      if (path.endsWith('/bindings/schemo/internal/transport/wails/index.ts')) {
        return `export const EnvironmentsService = globalThis.environmentServices;
          export const CookiesService = globalThis.cookieServices;
          export const TabsService = globalThis.tabServices;
          export const DraftService = globalThis.draftServices;
          export const CommandService = {};
          export const ScriptingService = {};
          export const CollectionsService = globalThis.collectionServices;
          export const RecordsService = globalThis.recordServices;
          export const WorkspaceService = globalThis.workspaceServices;
          export const PublicationsService = globalThis.publicationServices;
          export const BridgeService = {};`
      }
      if (path.endsWith('/src/stores/content.ts')) {
        return `export const askUnsaved = id => globalThis.tabContent.askUnsaved(id);
          export const contentOf = id => globalThis.tabContent.instances.get(id) ?? null;
          export const keepOnly = ids => globalThis.tabContent.calls.push(['keep', ids]);
          export const warmContent = tab => globalThis.tabContent.calls.push(['warm', tab.id]);
          export const ensureContent = tab => globalThis.tabContent.calls.push(['ensure', tab.id]);
          export const markBornEmpty = id => globalThis.tabContent.calls.push(['born', id]);
          export const dropContent = id => globalThis.tabContent.calls.push(['drop', id]);
          export const recordInto = () => {};
          export const adoptRecord = record => globalThis.tabContent.calls.push(['record', record.id]);
          export const flushAll = async () => {};
          export const paneSource = instance => instance;`
      }
      if (path.endsWith('/src/composables/useSettings.ts')) return 'export const useSettings = () => globalThis.settingsPort'
      if (path.endsWith('/src/composables/useLocale.ts')) return 'export const useLocale = () => ({ locale: { value: "en" } })'
      if (path.endsWith('/src/composables/intents.ts')) return 'export const focusUrlField = () => {}'
      if (path.endsWith('/src/composables/useGettingStarted.ts')) return 'export function markStep() {}'
      if (path.endsWith('/src/i18n.ts')) {
        return 'export const describeFailure = error => error.message; export const t = key => key;'
      }
      if (path.endsWith('/src/composables/useToast.ts')) {
        return 'export const useToast = () => ({ show: text => globalThis.notices.push(text) })'
      }
    },
  }],
})
after(() => vite.close())
const { CancellablePromise, CancelError } = await vite.ssrLoadModule('test:wails')
const { VariableKind } = await vite.ssrLoadModule('/bindings/schemo/internal/domain/models.ts')
const { useEnvironmentsStore } = await vite.ssrLoadModule('/src/stores/environments.ts')
const { useCookiesStore } = await vite.ssrLoadModule('/src/stores/cookies.ts')
const { useTabsStore } = await vite.ssrLoadModule('/src/stores/tabs.ts')
const { useDraft } = await vite.ssrLoadModule('/src/stores/draft.ts')
const { useCollectionsStore } = await vite.ssrLoadModule('/src/stores/collections.ts')
const { useRecordsStore } = await vite.ssrLoadModule('/src/stores/records.ts')
const { useWorkspacesStore } = await vite.ssrLoadModule('/src/stores/workspaces.ts')
const { usePublicationsStore } = await vite.ssrLoadModule('/src/stores/publications.ts')
const { useSessionPersistence } = await vite.ssrLoadModule('/src/composables/useSessionPersistence.ts')
const { useSheetNotice } = await vite.ssrLoadModule('/src/composables/useSheetNotice.ts')
let store
let pinia
afterEach(() => {
  if (pinia) disposePinia(pinia)
  pinia = undefined
  setActivePinia(undefined)
  globalThis.settingsPort = { settings: { value: null } }
})

test('session loading waits for an explicit ready signal and orders dependent reads', async (context) => {
  setupRecords()
  const calls = []
  globalThis.settingsPort = {
    settings: { value: { onboarding: { seen: true } } },
    loadSettings: async () => { calls.push('settings') },
    setLayout: () => {},
    setOnboarding: async () => {},
  }
  globalThis.workspaceServices.Snapshot = () => {
    calls.push('workspace')
    return CancellablePromise.resolve({ activeId: 'workspace', workspaces: [] })
  }
  globalThis.environmentServices.Snapshot = () => CancellablePromise.resolve(snapshot())
  globalThis.tabServices.Strip = () => {
    calls.push('tabs')
    return CancellablePromise.resolve({ tabs: [], active: '' })
  }
  globalThis.recordServices.List = () => {
    calls.push('history')
    return CancellablePromise.resolve([])
  }
  let load
  const renderer = createRenderer({
    createComment: () => ({}), insert: () => {}, remove: () => {},
    parentNode: () => null, nextSibling: () => null,
  })
  const app = renderer.createApp({ setup() {
    load = useSessionPersistence(store, useEnvironmentsStore())
    return () => null
  } })
  app.use(pinia).mount({})
  context.after(() => app.unmount())
  assert.deepEqual(calls, [])
  await load()
  assert.deepEqual(calls, ['settings', 'workspace', 'tabs', 'history'])
})

function setup() {
  pinia = createPinia()
  setActivePinia(pinia)
  globalThis.notices.length = 0
  store = useEnvironmentsStore()
  store.envState = snapshot('current', [{ id: 'secret', name: 'TOKEN', kind: VariableKind.VariableSecret, enabled: true }])
  return store
}

function deferred() {
  return CancellablePromise.withResolvers()
}

function snapshot(id = 'current', globals = []) {
  return { environments: [{ id, name: id, vars: [] }], globals, activeId: id }
}

function setupCookies() {
  pinia = createPinia()
  setActivePinia(pinia)
  globalThis.notices.length = 0
  globalThis.cookieServices.Jar = () => CancellablePromise.resolve([])
  store = useCookiesStore()
  return store
}

function strip(id = 'current') {
  return { tabs: [{ id, kind: 'request', ref: id }], active: id }
}

function setupTabs() {
  pinia = createPinia()
  setActivePinia(pinia)
  globalThis.notices.length = 0
  globalThis.tabContent = { calls: [], instances: new Map(), askUnsaved: async () => true }
  globalThis.tabServices.Strip = () => CancellablePromise.resolve(strip())
  store = useTabsStore()
  return store
}

function setupCollections() {
  setupTabs()
  store = useCollectionsStore()
  return store
}

function collection(id) {
  return { id, name: id, children: [], items: [] }
}

test('collection snapshots ignore older reads after a rename and clear on nil', async () => {
  setupCollections()
  const old = deferred()
  globalThis.collectionServices.Tree = () => old.promise
  const loading = store.load()
  globalThis.collectionServices.Rename = () => CancellablePromise.resolve([collection('renamed')])
  await store.rename('id', 'renamed')
  old.resolve([collection('old')])
  await loading
  assert.equal(store.tree[0].id, 'renamed')
  globalThis.collectionServices.Tree = () => CancellablePromise.resolve(null)
  await store.load()
  assert.deepEqual(store.tree, [])
})

test('collection refresh removes inactive stale tabs before warming their content', async () => {
  setupCollections()
  const tabs = useTabsStore()
  globalThis.tabServices.Strip = () => CancellablePromise.resolve({
    tabs: [{ id: 'active', kind: 'request' }, { id: 'removed', kind: 'request', ref: 'removed-node' }],
    active: 'active',
  })
  await tabs.load()
  globalThis.tabContent.calls.length = 0
  globalThis.collectionServices.Tree = () => CancellablePromise.resolve([collection('remaining')])
  globalThis.tabServices.Strip = () => CancellablePromise.resolve({
    tabs: [{ id: 'active', kind: 'request' }], active: 'active',
  })
  await store.load()
  assert.deepEqual(tabs.tabs.map(tab => tab.id), ['active'])
  assert.deepEqual(globalThis.tabContent.calls, [['keep', ['active']], ['warm', 'active'], ['ensure', 'active']])
  assert.deepEqual(globalThis.notices, [])
})

test('a failed collection refresh preserves tabs and a reset cannot reload the strip', async () => {
  setupCollections()
  let reads = 0
  globalThis.tabServices.Strip = () => {
    reads++
    return CancellablePromise.resolve(strip())
  }
  globalThis.collectionServices.Tree = () => CancellablePromise.reject(new Error('unavailable'))
  await store.load()
  assert.equal(reads, 0)
  const old = deferred()
  globalThis.collectionServices.Tree = () => old.promise
  const loading = store.load()
  store.reset()
  old.resolve([])
  await loading
  assert.equal(reads, 0)
})

test('workspace reset cancels collection creation and clears pending UI intentions', async () => {
  setupCollections()
  const old = deferred()
  globalThis.collectionServices.CreateCollection = () => old.promise
  const creating = store.createCollection('old')
  store.pendingCreate = 'old'
  store.pendingRemove = 'old'
  store.pendingRename = 'old'
  store.pendingSection = 'docs'
  store.reset()
  old.resolve({ tree: [collection('old')], collection: collection('old') })
  await creating
  assert.deepEqual(store.tree, [])
  for (const key of ['pendingCreate', 'pendingRemove', 'pendingRename', 'pendingSection']) assert.equal(store[key], null)
  assert.deepEqual(globalThis.notices, [])
})

test('saving a draft adopts the original tab even if another tab becomes active', async () => {
  setupCollections()
  const tabs = useTabsStore()
  globalThis.tabServices.Strip = () => CancellablePromise.resolve({
    tabs: [{ id: 'original', kind: 'request', draftId: 'draft' }], active: 'original',
  })
  await tabs.load()
  const old = deferred()
  globalThis.collectionServices.SaveDraft = () => old.promise
  const saving = store.saveDraft('folder', 'saved', 'draft')
  globalThis.tabServices.Strip = () => CancellablePromise.resolve({
    tabs: [{ id: 'original', kind: 'request', draftId: 'draft' }, { id: 'other', kind: 'request', draftId: 'another' }], active: 'other',
  })
  await tabs.load()
  const adopted = []
  globalThis.tabServices.Adopt = (id, node) => {
    adopted.push([id, node])
    return CancellablePromise.resolve(strip('other'))
  }
  old.resolve({ tree: [collection('folder')], node: { id: 'saved', collectionId: 'folder' } })
  assert.equal(await saving, true)
  assert.deepEqual(adopted, [['original', 'saved']])
})

test('finished collection runs supersede old reads and ignore other workspaces', async () => {
  setupCollections()
  store.tree = [collection('current')]
  const old = deferred()
  globalThis.collectionServices.LastRun = () => old.promise
  const loading = store.loadLastRun('current')
  store.applyRunFinished({ id: 'finished', collectionId: 'current', results: [] })
  old.resolve({ id: 'old', collectionId: 'current' })
  await loading
  assert.equal(store.lastRun.id, 'finished')
  store.reset()
  store.applyRunFinished({ id: 'late', collectionId: 'current' })
  assert.equal(store.lastRun, null)
})

function setupRecords() {
  setupTabs()
  useWorkspacesStore().state = { activeId: 'workspace', workspaces: [], personalId: 'workspace', counts: {} }
  store = useRecordsStore()
  store.workspaceId = 'workspace'
  return store
}

function record(id, startedAt = 1) {
  return { id, workspaceId: 'workspace', startedAt }
}

test('workspace switching supersedes a snapshot requested before the switch', async () => {
  setupTabs()
  store = useWorkspacesStore()
  const old = deferred()
  globalThis.workspaceServices.Snapshot = () => old.promise
  const loading = store.load()
  globalThis.workspaceServices.Switch = () => CancellablePromise.resolve({ activeId: 'new', workspaces: [] })
  await store.switch('new')
  old.resolve({ activeId: 'old', workspaces: [] })
  await loading
  assert.equal(store.activeId, 'new')
})

function setupPublications() {
  setupRecords()
  globalThis.publicationServices.Changes = () => CancellablePromise.resolve({})
  store = usePublicationsStore()
  return store
}

test('closing a publication popover cancels its preview and clears its password', async () => {
  setupPublications()
  const old = deferred()
  globalThis.publicationServices.Preview = () => old.promise
  store.openShare('collection')
  store.password = 'private password'
  store.closeShare()
  old.resolve({ environments: [], publication: { visibility: 'password' } })
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(store.preview, null)
  assert.equal(store.password, '')
  assert.equal(store.reading, false)
  assert.deepEqual(globalThis.notices, [])
})

test('saved publication passwords load in both editors without becoming pending changes', async () => {
  setupPublications()
  const page = {collectionId: 'collection', status: 'active', visibility: 'password', canManage: true}
  globalThis.publicationServices.Preview = () => CancellablePromise.resolve({publication: page, environments: []})
  globalThis.publicationServices.Password = () => CancellablePromise.resolve('saved-password')
  for (const edit of [() => store.openShare('collection'), () => store.edit('collection')]) {
    edit()
    await new Promise(resolve => setImmediate(resolve))
    assert.equal(store.password, 'saved-password')
    assert.equal(store.passwordChanged, false)
    store.password = 'replacement-password'
    assert.equal(store.passwordChanged, true)
    store.closeShare()
    assert.equal(store.password, '')
    assert.equal(store.passwordChanged, false)
  }
})

test('a password read finishing after the editor closes cannot restore the secret', async () => {
  setupPublications()
  const old = deferred()
  globalThis.publicationServices.Preview = () => CancellablePromise.resolve({
    publication: {collectionId: 'collection', status: 'active', visibility: 'password', canManage: true}, environments: [],
  })
  globalThis.publicationServices.Password = () => old.promise
  store.openShare('collection')
  await new Promise(resolve => setImmediate(resolve))
  store.closeShare()
  old.resolve('late-secret')
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(store.password, '')
  assert.equal(store.passwordChanged, false)
  assert.deepEqual(globalThis.notices, [])
})

test('updating documentation preserves a saved password without sending it again', async () => {
  setupPublications()
  const page = {collectionId: 'collection', status: 'active', visibility: 'password', canManage: true}
  globalThis.publicationServices.Preview = () => CancellablePromise.resolve({publication: page, environments: []})
  globalThis.publicationServices.Password = () => CancellablePromise.resolve('saved-password')
  globalThis.publicationServices.List = () => CancellablePromise.resolve([page])
  const sent = []
  globalThis.publicationServices.Publish = choice => {
    sent.push(choice.password)
    return CancellablePromise.resolve({page, passwordSaved: true})
  }
  store.edit('collection')
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(await store.publish(true), true)
  assert.deepEqual(sent, [''])
  assert.equal(store.password, 'saved-password')
  store.password = 'replacement-password'
  assert.equal(await store.publish(true), true)
  assert.deepEqual(sent, ['', 'replacement-password'])
  assert.equal(store.passwordChanged, false)
})

test('a keyring save failure preserves the password and does not report a failed publication', async () => {
  setupPublications()
  const page = {collectionId: 'collection', status: 'active', visibility: 'password'}
  store.collection = 'collection'
  store.visibility = 'password'
  store.password = 'replacement-password'
  globalThis.publicationServices.Publish = () => CancellablePromise.resolve({page, passwordSaved: false})
  globalThis.publicationServices.Preview = () => CancellablePromise.resolve({publication: page, environments: []})
  globalThis.publicationServices.List = () => CancellablePromise.resolve([page])
  assert.equal(await store.publish(true), true)
  assert.equal(store.password, 'replacement-password')
  assert.deepEqual(globalThis.notices, ['publications.passwordSaveFailed'])
})

test('publication reset cancels old lists and marks without restoring another workspace', async () => {
  setupPublications()
  const list = deferred()
  const mark = deferred()
  globalThis.publicationServices.List = () => list.promise
  globalThis.publicationServices.Changes = () => mark.promise
  const loading = store.load()
  const marking = store.mark('old')
  store.reset()
  list.resolve([{ collectionId: 'old' }])
  mark.resolve({ behind: true })
  await Promise.all([loading, marking])
  assert.deepEqual(store.pages, [])
  assert.deepEqual(store.marks, {})
  assert.equal(store.loading, false)
})

test('older publication previews cannot change the current environment or visibility', async () => {
  setupPublications()
  const old = deferred()
  globalThis.publicationServices.Preview = () => old.promise
  store.openShare('collection')
  globalThis.publicationServices.Preview = () => CancellablePromise.resolve({ environments: [{ id: 'new' }] })
  await store.choose('new')
  old.resolve({ environments: [{ id: 'old' }], publication: { visibility: 'password' } })
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(store.environment, 'new')
  assert.equal(store.visibility, 'unlisted')
})

test('reset cancels publishing without refreshing the new workspace', async () => {
  setupPublications()
  const old = deferred()
  let reads = 0
  globalThis.publicationServices.Publish = () => old.promise
  globalThis.publicationServices.Preview = () => { reads++; return CancellablePromise.resolve({}) }
  store.collection = 'old'
  const publishing = store.publish(true)
  store.reset()
  old.resolve({ collectionId: 'old' })
  assert.equal(await publishing, false)
  assert.equal(reads, 0)
  assert.equal(store.busy, false)
})

test('history reads preserve events received while the snapshot was loading', async () => {
  setupRecords()
  const old = deferred()
  globalThis.recordServices.List = () => old.promise
  const loading = store.load()
  store.prepend(record('new', 2))
  old.resolve([record('old')])
  await loading
  assert.deepEqual(store.records.map((record) => record.id), ['new', 'old'])
})

test('history reset cancels old pages without changing loading state of a new page', async () => {
  setupRecords()
  const old = deferred()
  globalThis.recordServices.Page = () => old.promise
  const paging = store.loadOlder()
  store.reset()
  const next = deferred()
  globalThis.recordServices.Page = () => next.promise
  const loading = store.loadOlder()
  old.resolve([record('old')])
  await paging
  assert.equal(store.olderLoading, true)
  next.resolve(null)
  await loading
  assert.equal(store.olderLoading, false)
  assert.equal(store.olderExhausted, true)
  assert.deepEqual(store.records, [])
})

test('a history reload invalidates a page captured before deleted rows disappeared', async () => {
  setupRecords()
  const old = deferred()
  globalThis.recordServices.Page = () => old.promise
  const paging = store.loadOlder()
  globalThis.recordServices.List = () => CancellablePromise.resolve([record('remaining')])
  await store.load()
  old.resolve([record('removed')])
  await paging
  assert.deepEqual(store.records.map((record) => record.id), ['remaining'])
  assert.equal(store.olderLoading, false)
})

test('deletion invalidates late history and body reads', async () => {
  setupRecords()
  store.prepend({ ...record('removed'), requestBody: { size: 100 } })
  const oldList = deferred()
  const oldBody = deferred()
  globalThis.recordServices.List = () => oldList.promise
  globalThis.recordServices.Body = () => oldBody.promise
  globalThis.recordServices.Clear = () => CancellablePromise.resolve()
  const loading = store.load()
  const reading = store.loadBodies('removed')
  await store.clearRecords(['removed'])
  oldList.resolve([record('removed')])
  oldBody.resolve('private payload')
  await loading
  assert.equal(await reading, null)
  assert.deepEqual(store.records, [])
  assert.deepEqual(store.bodies, {})
  assert.deepEqual(globalThis.notices, [])
})

test('workspace reset prevents record refresh from updating tabs', async () => {
  setupRecords()
  const old = deferred()
  globalThis.recordServices.Record = () => old.promise
  const refreshing = store.refresh('old')
  store.reset()
  old.resolve(record('old'))
  await refreshing
  assert.deepEqual(globalThis.tabContent.calls, [])
})

test('a refreshed record invalidates its cached bodies and obsolete body read', async () => {
  setupRecords()
  store.prepend({ ...record('id'), requestBody: { size: 100 } })
  const old = deferred()
  globalThis.recordServices.Body = () => old.promise
  const reading = store.loadBodies('id')
  store.adoptRecord(record('id'))
  old.resolve('old body')
  assert.equal(await reading, null)
  assert.deepEqual(store.bodies, {})
})

test('tab snapshots and content ignore late reads after an acknowledged change', async () => {
  setupTabs()
  const old = deferred()
  globalThis.tabServices.Strip = () => old.promise
  const loading = store.load()
  globalThis.tabServices.Pin = () => CancellablePromise.resolve(strip('pinned'))
  await store.pin('current', true)
  const calls = [...globalThis.tabContent.calls]
  old.resolve(strip('old'))
  await loading
  assert.equal(store.activeId, 'pinned')
  assert.deepEqual(globalThis.tabContent.calls, calls)
})

test('workspace reset cancels tab reads and prevents old content warming', async () => {
  setupTabs()
  const old = deferred()
  globalThis.tabServices.Strip = () => old.promise
  const loading = store.load()
  store.reset()
  old.resolve(strip('old'))
  await loading
  assert.deepEqual(store.tabs, [])
  assert.equal(store.activeId, '')
  assert.deepEqual(globalThis.tabContent.calls, [['keep', []]])
  assert.deepEqual(globalThis.notices, [])
})

test('returning to the active tab supersedes a pending activation of another tab', async () => {
  setupTabs()
  await store.load()
  const other = deferred()
  globalThis.tabServices.Activate = id => id === 'other' ? other.promise : CancellablePromise.resolve(strip())
  const switching = store.activate('other')
  await store.activate('current')
  other.resolve(strip('other'))
  await switching
  assert.equal(store.activeId, 'current')
  assert.equal(store.activationTick, 2)
})

test('failed tab commands retain the current strip and report the failure', async () => {
  setupTabs()
  await store.load()
  globalThis.tabServices.Move = () => CancellablePromise.reject(new Error('move failed'))
  await store.move('current', 'other')
  assert.equal(store.activeId, 'current')
  assert.deepEqual(globalThis.notices, ['errors.tabsFailed'])
})

test('new tabs are marked blank before their content is warmed', async () => {
  setupTabs()
  globalThis.tabServices.New = () => CancellablePromise.resolve(strip('blank'))
  await store.newTab()
  assert.deepEqual(globalThis.tabContent.calls, [
    ['born', 'blank'], ['keep', ['blank']], ['warm', 'blank'], ['ensure', 'blank'],
  ])
})

test('a canceled new tab does not mark or warm content in the new workspace', async () => {
  setupTabs()
  const old = deferred()
  globalThis.tabServices.New = () => old.promise
  const creating = store.newTab()
  store.reset()
  old.resolve(strip('old'))
  await creating
  assert.deepEqual(globalThis.tabContent.calls, [['keep', []]])
})

test('adopting a saved node drops its previous content before warming the replacement', async () => {
  setupTabs()
  globalThis.tabServices.Adopt = () => CancellablePromise.resolve(strip())
  await store.adoptNode('current', 'saved')
  assert.deepEqual(globalThis.tabContent.calls, [
    ['drop', 'current'], ['keep', ['current']], ['warm', 'current'], ['ensure', 'current'],
  ])
})

test('nil tab lists clear the strip and its content', async () => {
  setupTabs()
  await store.load()
  globalThis.tabServices.Strip = () => CancellablePromise.resolve({ tabs: null, active: '' })
  globalThis.tabContent.calls.length = 0
  await store.load()
  assert.deepEqual(store.tabs, [])
  assert.equal(store.activeTab, null)
  assert.deepEqual(globalThis.tabContent.calls, [['keep', []]])
})

for (const operation of ['close', 'closeOthers', 'closeRight', 'openNode']) {
  test(`workspace reset while confirming ${operation} prevents the old command`, async () => {
    setupTabs()
    globalThis.tabServices.Strip = () => CancellablePromise.resolve({
      tabs: [{ id: 'current', preview: true }, { id: 'other' }], active: 'current',
    })
    await store.load()
    const confirmation = deferred()
    let commands = 0
    globalThis.tabContent.askUnsaved = () => confirmation.promise
    for (const command of ['Close', 'CloseOthers', 'CloseRight', 'Open']) {
      globalThis.tabServices[command] = () => { commands++; return CancellablePromise.resolve(strip()) }
    }
    const pending = store[operation](operation === 'openNode' ? 'new-node' : 'current')
    store.reset()
    confirmation.resolve(true)
    await pending
    assert.equal(commands, 0)
    assert.deepEqual(store.tabs, [])
  })
}

test('workspace reset while flushing a closing draft prevents abandonment and closing', async () => {
  setupTabs()
  await store.load()
  const flushing = deferred()
  let abandoned = false
  let closing = false
  globalThis.tabContent.instances.set('current', { draft: {
    flush: () => { store.reset(); return flushing.promise },
    abandon: () => { abandoned = true },
  } })
  globalThis.tabServices.Close = () => { closing = true; return CancellablePromise.resolve(strip()) }
  const pending = store.close('current')
  flushing.resolve()
  await pending
  assert.equal(abandoned, false)
  assert.equal(closing, false)
})

test('failed closing keeps the draft buffer usable until the backend accepts the close', async () => {
  setupTabs()
  await store.load()
  let abandoned = false
  globalThis.tabContent.instances.set('current', { draft: {
    flush: async () => true,
    abandon: () => { abandoned = true },
  } })
  globalThis.tabServices.Close = () => CancellablePromise.reject(new Error('database write failed'))
  await store.close('current')
  assert.equal(abandoned, false)
  assert.equal(store.activeId, 'current')
  assert.deepEqual(globalThis.notices, ['errors.tabsFailed'])
})

test('failed draft flushing prevents a tab close', async () => {
  setupTabs()
  await store.load()
  let closing = false
  globalThis.tabContent.instances.set('current', { draft: { flush: async () => false } })
  globalThis.tabServices.Close = () => { closing = true; return CancellablePromise.resolve(strip()) }
  await store.close('current')
  assert.equal(closing, false)
  assert.equal(store.activeId, 'current')
})

test('duplicating a tab waits for its buffered edits', async () => {
  setupTabs()
  await store.load()
  const flushing = deferred()
  let copied = false
  globalThis.tabContent.instances.set('current', { draft: { flush: () => flushing.promise } })
  globalThis.tabServices.Duplicate = () => { copied = true; return CancellablePromise.resolve(strip()) }
  const copying = store.duplicate('current')
  assert.equal(copied, false)
  flushing.resolve(true)
  await copying
  assert.equal(copied, true)
})

test('failed draft flushing prevents duplication', async () => {
  setupTabs()
  await store.load()
  let copied = false
  globalThis.tabContent.instances.set('current', { draft: { flush: async () => false } })
  globalThis.tabServices.Duplicate = () => { copied = true; return CancellablePromise.resolve(strip()) }
  await store.duplicate('current')
  assert.equal(copied, false)
})

function draftForTest() {
  let state = { draft: { id: 'draft', url: '', body: '' } }
  return useDraft({
    id: () => 'draft', state: () => state, keep: next => { state = next },
    finished: async () => {}, scriptsLevel: () => null,
  })
}

test('failed text writes remain buffered and can be retried', async (context) => {
  context.mock.timers.enable({ apis: ['setTimeout'] })
  globalThis.notices = []
  const draft = draftForTest()
  draft.setUrl('https://example.test')
  globalThis.draftServices.SetText = () => CancellablePromise.reject(new Error('database write failed'))
  assert.equal(await draft.flush(), false)
  assert.equal(draft.typing.value, true)
  assert.equal(draft.url.value, 'https://example.test')
  globalThis.draftServices.SetText = (id, part) => CancellablePromise.resolve({
    draft: { id, url: part.text, body: '' }, field: part.field, rev: part.rev,
  })
  assert.equal(await draft.flush(), true)
  assert.equal(draft.typing.value, false)
})

test('a flush cannot acknowledge text typed while its write was pending', async (context) => {
  context.mock.timers.enable({ apis: ['setTimeout'] })
  const draft = draftForTest()
  const writing = deferred()
  let part
  globalThis.draftServices.SetText = (id, pending) => { part = pending; return writing.promise }
  draft.setUrl('https://first.test')
  const flushed = draft.flush()
  draft.setUrl('https://second.test')
  writing.resolve({ draft: { id: 'draft', url: part.text, body: '' }, field: part.field, rev: part.rev })
  assert.equal(await flushed, false)
  assert.equal(draft.typing.value, true)
  assert.equal(draft.url.value, 'https://second.test')
})

test('disposing the tab store clears content and cancels pending IPC', async () => {
  setupTabs()
  const old = deferred()
  globalThis.tabServices.Strip = () => old.promise
  const loading = store.load()
  store.$dispose()
  old.resolve(strip('old'))
  await loading
  assert.deepEqual(store.tabs, [])
  assert.deepEqual(globalThis.tabContent.calls, [['keep', []]])
})

test('closing around an unknown tab leaves other tabs intact', async () => {
  setupTabs()
  await store.load()
  let commands = 0
  globalThis.tabServices.CloseOthers = globalThis.tabServices.CloseRight = () => {
    commands++
    return CancellablePromise.resolve(strip())
  }
  await store.closeOthers('missing')
  await store.closeRight('missing')
  assert.equal(commands, 0)
  assert.equal(store.activeId, 'current')
})

test('cookie reads ignore late responses and their domain selection', async () => {
  setupCookies()
  const old = deferred()
  globalThis.cookieServices.Jar = () => old.promise
  const previous = store.load()
  globalThis.cookieServices.Jar = () => CancellablePromise.resolve([{ domain: 'new.example', cookies: [] }])
  await store.load()
  store.select('')
  old.resolve([{ domain: 'old.example', cookies: [] }])
  await previous
  assert.equal(store.domains[0].domain, 'new.example')
  assert.equal(store.domain, '')
})

test('cookie reset cancels old reads and clears the editor', async () => {
  setupCookies()
  const old = deferred()
  globalThis.cookieServices.Jar = () => old.promise
  const pending = store.load()
  store.edit({ id: 'cookie', value: 'private value' })
  await store.reset()
  old.resolve([{ domain: 'old.example', cookies: [] }])
  await pending
  assert.deepEqual(store.domains, [])
  assert.equal(store.domain, '')
  assert.equal(store.editing, null)
  assert.deepEqual(globalThis.notices, [])
})

test('an open cookie manager reloads only the new workspace on reset', async () => {
  setupCookies()
  const old = deferred()
  globalThis.cookieServices.Jar = () => old.promise
  const previous = store.openManager('old.example')
  globalThis.cookieServices.Jar = () => CancellablePromise.resolve([{ domain: 'new.example', cookies: [] }])
  await store.reset()
  old.resolve([{ domain: 'old.example', cookies: [] }])
  await previous
  assert.equal(store.domains[0].domain, 'new.example')
  assert.equal(store.domain, 'new.example')
})

test('an old cookie save does not close a newer editor', async () => {
  setupCookies()
  const pending = deferred()
  globalThis.cookieServices.Save = () => pending.promise
  store.edit({ id: 'old-cookie', value: 'old value' })
  const saving = store.save(store.editing)
  store.edit({ id: 'new-cookie', value: 'new value' })
  pending.resolve()
  await saving
  assert.equal(store.editing.id, 'new-cookie')
})

test('reset cancels a cookie write without reloading another workspace', async () => {
  setupCookies()
  const pending = deferred()
  let reads = 0
  globalThis.cookieServices.Jar = () => { reads++; return CancellablePromise.resolve([]) }
  globalThis.cookieServices.Save = () => pending.promise
  const saving = store.save({ id: 'cookie', value: 'private value' })
  await store.reset()
  pending.resolve()
  await saving
  assert.equal(reads, 0)
  assert.deepEqual(globalThis.notices, [])
})

test('a late domain deletion keeps a newly selected domain and its editor', async () => {
  setupCookies()
  const pending = deferred()
  globalThis.cookieServices.DeleteDomain = () => pending.promise
  globalThis.cookieServices.Jar = () => CancellablePromise.resolve([{ domain: 'new.example', cookies: [] }])
  store.select('old.example')
  const deleting = store.removeDomain()
  store.select('new.example')
  store.edit({ id: 'new-cookie', domain: 'new.example' })
  pending.resolve()
  await deleting
  assert.equal(store.domain, 'new.example')
  assert.equal(store.editing.id, 'new-cookie')
})

test('a nil cookie list clears the previous jar while retaining the requested host', async () => {
  setupCookies()
  globalThis.cookieServices.Jar = () => CancellablePromise.resolve([{ domain: 'host.example', cookies: [] }])
  await store.load('host.example')
  globalThis.cookieServices.Jar = () => CancellablePromise.resolve(null)
  await store.load()
  assert.deepEqual(store.domains, [])
  assert.equal(store.domain, 'host.example')
})

test('disposing the cookie store cancels reads and drops its editor', async () => {
  setupCookies()
  const old = deferred()
  globalThis.cookieServices.Jar = () => old.promise
  const pending = store.load('host.example')
  store.edit({ id: 'cookie', value: 'private value' })
  store.$dispose()
  old.resolve([{ domain: 'host.example', cookies: [] }])
  await pending
  assert.deepEqual(store.domains, [])
  assert.equal(store.editing, null)
  assert.equal(store.domain, '')
})

test('a late snapshot cannot overwrite a newer snapshot', async () => {
  setup()
  const old = deferred()
  globalThis.environmentServices.Snapshot = () => old.promise
  const previous = store.load()
  globalThis.environmentServices.Snapshot = () => CancellablePromise.resolve(snapshot('new'))
  await store.load()
  old.resolve(snapshot('old'))
  await previous
  assert.equal(store.activeId, 'new')
})

test('reset cancels an old workspace load without restoring state or showing an error', async () => {
  setup()
  const old = deferred()
  globalThis.environmentServices.Snapshot = () => old.promise
  const pending = store.load()
  store.reset()
  old.resolve(snapshot('old'))
  await pending
  assert.equal(store.envState, null)
  assert.deepEqual(globalThis.notices, [])
})

test('closing the sheet cancels a reveal and clears secrets', async () => {
  setup()
  const old = deferred()
  globalThis.environmentServices.Reveal = () => old.promise
  const pending = store.reveal('secret')
  const rejected = assert.rejects(pending, CancelError)
  store.closeSheet()
  old.resolve('private value')
  await rejected
  assert.deepEqual(store.revealed, {})
})

test('closing the sheet rejects an already fulfilled reveal before it can populate the cache', async () => {
  setup()
  globalThis.environmentServices.Reveal = () => CancellablePromise.resolve('private value')
  const pending = store.reveal('secret')
  const rejected = assert.rejects(pending, CancelError)
  store.closeSheet()
  await rejected
  assert.deepEqual(store.revealed, {})
})

test('an old snapshot cannot overwrite an acknowledged edit', async () => {
  setup()
  const old = deferred()
  globalThis.environmentServices.Snapshot = () => old.promise
  const previous = store.load()
  globalThis.environmentServices.UpdateEnvironment = () => CancellablePromise.resolve(snapshot('edited'))
  await store.renameEnv('current', 'edited')
  old.resolve(snapshot('old'))
  await previous
  assert.equal(store.activeId, 'edited')
})

test('a secret save after sheet closure does not restore its revealed value', async () => {
  setup()
  const variable = { id: 'secret', name: 'TOKEN', kind: VariableKind.VariableSecret, enabled: true }
  globalThis.environmentServices.Snapshot = () => CancellablePromise.resolve(snapshot('current', [variable]))
  await store.load()
  const edit = deferred()
  globalThis.environmentServices.UpdateVariable = () => edit.promise
  const pending = store.updateVar(null, 'secret', { value: 'new private value' })
  store.closeSheet()
  edit.resolve(snapshot('current', [variable]))
  await pending
  assert.equal(store.globals[0].id, 'secret')
  assert.deepEqual(store.revealed, {})
})

test('disposing the store cancels its in-flight reads', async () => {
  setup()
  const old = deferred()
  globalThis.environmentServices.Reveal = () => old.promise
  const pending = store.reveal('secret')
  const rejected = assert.rejects(pending, CancelError)
  store.$dispose()
  old.resolve('private value')
  await rejected
  assert.deepEqual(store.revealed, {})
})

test('reset cancels a secret read for a cell editor', async () => {
  setup()
  const old = deferred()
  globalThis.environmentServices.Reveal = () => old.promise
  const pending = store.valueForEdit('secret')
  const rejected = assert.rejects(pending, CancelError)
  store.reset()
  old.resolve('private value')
  await rejected
  assert.deepEqual(store.revealed, {})
})

test('clearing globals drops revealed values of removed variables', async () => {
  setup()
  const variable = { id: 'secret', name: 'TOKEN', kind: VariableKind.VariableSecret, enabled: true }
  globalThis.environmentServices.Snapshot = () => CancellablePromise.resolve(snapshot('current', [variable]))
  globalThis.environmentServices.Reveal = () => CancellablePromise.resolve('private value')
  globalThis.environmentServices.ClearGlobals = () => CancellablePromise.resolve(snapshot())
  await store.load()
  await store.reveal('secret')
  assert.equal(store.isRevealed('secret'), true)
  await store.clearGlobals()
  assert.equal(store.isRevealed('secret'), false)
})

test('a delayed reveal cannot restore a secret deleted while it was being read', async () => {
  setup()
  const variable = { id: 'secret', name: 'TOKEN', kind: VariableKind.VariableSecret, enabled: true }
  globalThis.environmentServices.Snapshot = () => CancellablePromise.resolve(snapshot('current', [variable]))
  globalThis.environmentServices.ClearGlobals = () => CancellablePromise.resolve(snapshot())
  await store.load()
  const old = deferred()
  globalThis.environmentServices.Reveal = () => old.promise
  const pending = store.reveal('secret')
  const rejected = assert.rejects(pending, CancelError)
  await store.clearGlobals()
  old.resolve('private value')
  await rejected
  assert.deepEqual(store.revealed, {})
})

test('canceled operations do not overwrite a newer sheet notice', async () => {
  setup()
  const notice = useSheetNotice()
  notice.setNotice('newer failure')
  assert.equal(await notice.withNotice(() => Promise.reject(new CancelError())), false)
  assert.equal(notice.notice.value, 'newer failure')
})

test('publication options are sent to the preview and discarded when the popover closes', async () => {
  setupPublications()
  const chosen = []
  globalThis.publicationServices.Preview = (workspace, collection, environment, options) => {
    chosen.push(options)
    return CancellablePromise.resolve({environments: [], options: options ?? {docs: true, examples: true}})
  }
  store.openShare('collection')
  await new Promise(resolve => setImmediate(resolve))
  await store.chooseOption('examples')
  assert.equal(chosen[0], null)
  assert.deepEqual(chosen[1], {docs: true, examples: false})
  assert.equal(store.options.examples, false)
  store.closeShare()
  assert.equal(store.options, null)
})

test('saved tree changes refresh published ancestors, cached folders and the open preview', async () => {
  setupPublications()
  const publications = store
  const collections = useCollectionsStore()
  publications.pages = [{ collectionId: 'parent' }]
  publications.marks = { child: { behind: false } }
  const marked = []
  globalThis.publicationServices.Changes = (workspace, id) => {
    marked.push(id)
    return CancellablePromise.resolve({ behind: true, changes: [{name: id}] })
  }
  let previews = 0
  globalThis.publicationServices.Preview = () => {
    previews++
    return CancellablePromise.resolve({ behind: true, environments: [], options: {docs: true, examples: true} })
  }
  publications.openShare('child')
  await new Promise(resolve => setImmediate(resolve))
  marked.length = 0
  const before = previews
  globalThis.collectionServices.Describe = () => CancellablePromise.resolve([
    {...collection('parent'), children: [collection('child')]},
  ])
  await collections.describe('child', 'Updated folder description.')
  await new Promise(resolve => setImmediate(resolve))
  assert.deepEqual(marked.sort(), ['child', 'parent'])
  assert.equal(publications.markOf('parent').behind, true)
  assert.equal(publications.markOf('child').behind, true)
  assert.equal(previews, before + 1)
})

test('the publications page refreshes its editing preview without opening a share popover', async () => {
  setupPublications()
  const publications = store
  const collections = useCollectionsStore()
  const previews = []
  globalThis.publicationServices.Preview = (workspace, id, environment, options) => {
    previews.push({id, options})
    return CancellablePromise.resolve({environments: [], options: options ?? {docs: true, examples: true}})
  }
  publications.edit('collection')
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(publications.open, false)
  await publications.chooseOption('examples')
  collections.tree = [collection('collection')]
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(previews.length, 3)
  assert.deepEqual(previews.at(-1), {id: 'collection', options: {docs: true, examples: false}})
  assert.equal(publications.collection, 'collection')
})

test('an earlier publication mark cannot overwrite a newer saved-tree result', async () => {
  setupPublications()
  const old = deferred()
  globalThis.publicationServices.Changes = () => old.promise
  const earlier = store.mark('collection')
  globalThis.publicationServices.Changes = () => CancellablePromise.resolve({behind: true})
  await store.mark('collection')
  old.resolve({behind: false})
  await earlier
  assert.equal(store.markOf('collection').behind, true)
  assert.deepEqual(globalThis.notices, [])
})

test('removing a folder cancels its pending publication mark and drops the cached result', async () => {
  setupPublications()
  const publications = store
  const collections = useCollectionsStore()
  const old = deferred()
  globalThis.publicationServices.Changes = () => old.promise
  publications.marks = { child: { behind: false } }
  const pending = publications.mark('child')
  collections.tree = [collection('parent')]
  await new Promise(resolve => setImmediate(resolve))
  old.resolve({behind: true})
  await pending
  assert.equal(publications.markOf('child'), null)
  assert.deepEqual(globalThis.notices, [])
})

test('publishing clears stale child marks while preserving changes in another publication', async () => {
  setupPublications()
  const page = {collectionId: 'parent', status: 'active'}
  const other = {collectionId: 'other', status: 'active'}
  store.marks = {
    parent: {page, behind: true},
    child: {page, behind: true, inheritedFrom: 'Parent'},
    other: {page: other, behind: true},
  }
  store.collection = 'parent'
  globalThis.publicationServices.Publish = () => CancellablePromise.resolve({page, passwordSaved: true})
  globalThis.publicationServices.Preview = () => CancellablePromise.resolve({environments: []})
  globalThis.publicationServices.List = () => CancellablePromise.resolve([page, other])
  globalThis.publicationServices.Changes = (workspace, id) =>
    CancellablePromise.resolve({page: id === 'other' ? other : page, behind: id === 'other'})
  assert.equal(store.hasChanges, true)
  assert.equal(await store.publish(true), true)
  assert.equal(store.markOf('parent').behind, false)
  assert.equal(store.markOf('child').behind, false)
  assert.equal(store.hasChanges, true)
  store.marks.other = {page: other, behind: false}
  assert.equal(store.hasChanges, false)
})

test('unpublishing clears cached descendant marks and inactive pages do not warn in the sidebar', async () => {
  setupPublications()
  const page = {collectionId: 'parent', status: 'active'}
  store.marks = {parent: {page, behind: true}, child: {page, behind: true}}
  store.askUnpublish('parent')
  globalThis.publicationServices.Unpublish = () => CancellablePromise.resolve()
  globalThis.publicationServices.List = () => CancellablePromise.resolve([])
  globalThis.publicationServices.Changes = () => CancellablePromise.resolve({behind: false})
  assert.equal(await store.confirmUnpublish(), true)
  assert.equal(store.hasChanges, false)
  for (const inactive of [{...page, status: 'revoked'}, {...page, pendingUnpublish: true}]) {
    store.marks = {parent: {page: inactive, behind: true}}
    assert.equal(store.hasChanges, false)
  }
})
