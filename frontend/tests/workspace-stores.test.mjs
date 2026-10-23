import assert from 'node:assert/strict'
import { after, afterEach, test } from 'node:test'
import { resolve } from 'node:path'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { createRenderer } from 'vue'
import { vite } from './vite.mjs'

globalThis.window = new EventTarget()
globalThis.document = Object.assign(new EventTarget(), { createElement: () => ({}) })
globalThis.settingsPort = { settings: { value: null } }
globalThis.environmentServices = {}
globalThis.cookieServices = {}
globalThis.runServices = {}
globalThis.draftServices = {}
globalThis.tabContent = {}
globalThis.collectionServices = {}
globalThis.recordServices = {}
globalThis.workspaceServices = {}
globalThis.publicationServices = {}
globalThis.notices = []

const runtimePath = resolve('node_modules/@wailsio/runtime/dist').replaceAll('\\', '/')
const runtime = `export { CancellablePromise, CancelError } from '${runtimePath}/cancellable.js';
export * as Create from '${runtimePath}/create.js';
export const Events = { On: () => () => {} };`
const load = await vite({
  '@wailsio/runtime': runtime,
  '/@wailsio/runtime/dist/index.js': runtime,
  '/bindings/schemo/internal/agent/index.ts': 'export const Extension = {}',
  '/bindings/schemo/internal/cloud/index.ts': 'export const Publications = globalThis.publicationServices',
  '/bindings/schemo/internal/request/index.ts': `export const History = globalThis.recordServices
    export const Drafts = globalThis.draftServices
    export const Cookies = globalThis.cookieServices
    export const Runs = globalThis.runServices`,
  '/bindings/schemo/internal/script/index.ts': 'export const Scripts = {}',
  '/bindings/schemo/internal/workspace/index.ts': `export const Collections = globalThis.collectionServices
    export const Workspaces = globalThis.workspaceServices
    export const Environments = globalThis.environmentServices`,
  '/src/stores/content.ts': `export const askUnsaved = id => globalThis.tabContent.askUnsaved(id);
    export const draftById = id => ({ id, method: 'GET', url: '', body: '' });
    export const contentOf = id => globalThis.tabContent.instances.get(id) ?? null;
    export const keepOnly = ids => globalThis.tabContent.calls.push(['keep', ids]);
    export const warmContent = tab => globalThis.tabContent.calls.push(['warm', tab.id]);
    export const ensureContent = tab => globalThis.tabContent.calls.push(['ensure', tab.id]);
    export const markBornEmpty = id => globalThis.tabContent.calls.push(['born', id]);
    export const dropContent = id => globalThis.tabContent.calls.push(['drop', id]);
    export const recordInto = () => {};
    export const adoptRecord = record => globalThis.tabContent.calls.push(['record', record.id]);
    export const flushAll = async () => {};
    export const paneSource = instance => instance;`,
  '/src/composables/useSettings.ts': 'export const useSettings = () => globalThis.settingsPort',
  '/src/composables/useLocale.ts': 'export const useLocale = () => ({ locale: { value: "en" } })',
  '/src/lib/intents.ts': 'export const focusUrlField = () => {}',
  '/src/composables/useGettingStarted.ts': 'export function markStep() {}',
  '/src/i18n/index.ts': 'export const describeFailure = error => error.message; export const t = key => key;',
  '/src/composables/useToast.ts': 'export const useToast = () => ({ show: text => globalThis.notices.push(text) })',
})
const { CancellablePromise, CancelError } = await load('@wailsio/runtime')
const { VariableKind } = await load('/bindings/schemo/internal/domain/models.ts')
const { useEnvironmentsStore } = await load('/src/stores/environments.ts')
const { useCookiesStore } = await load('/src/stores/cookies.ts')
const { useTabsStore } = await load('/src/stores/tabs.ts')
const { useDraft } = await load('/src/composables/useDraft.ts')
const { useCollectionsStore } = await load('/src/stores/collections.ts')
const { useRecordsStore } = await load('/src/stores/records.ts')
const { useWorkspacesStore } = await load('/src/stores/workspaces.ts')
const { usePublicationsStore } = await load('/src/stores/publications.ts')
const { useSessionPersistence } = await load('/src/composables/useSessionPersistence.ts')
const { useSheetNotice } = await load('/src/composables/useSheetNotice.ts')
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
  globalThis.environmentServices.State = () => CancellablePromise.resolve(snapshot())
  globalThis.workspaceServices.Tabs = () => {
    calls.push('tabs')
    return CancellablePromise.resolve({ workspace: 'workspace', tabs: [], active: '' })
  }
  globalThis.workspaceServices.SaveTabs = () => CancellablePromise.resolve()
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
  globalThis.cookieServices.DomainOf = (address) => CancellablePromise.resolve(address)
  globalThis.cookieServices.Jar = () => CancellablePromise.resolve([])
  store = useCookiesStore()
  return store
}

function strip(id = 'current', tabs = [{ id, kind: 'request', draftId: `tab:${id}` }]) {
  return { workspace: 'workspace', tabs, active: id }
}

// What the store wrote: each saved strip as its tab ids, and each discarded draft.
let written = []

function setupTabs() {
  pinia = createPinia()
  setActivePinia(pinia)
  globalThis.notices.length = 0
  written = []
  globalThis.tabContent = { calls: [], instances: new Map(), askUnsaved: async () => true }
  globalThis.workspaceServices.Tabs = () => CancellablePromise.resolve(strip())
  globalThis.workspaceServices.SaveTabs = (saved) => {
    written.push(['save', saved.tabs.map(tab => tab.id), saved.active])
    return CancellablePromise.resolve()
  }
  globalThis.draftServices.Discard = (id) => { written.push(['discard', id]); return CancellablePromise.resolve() }
  store = useTabsStore()
  return store
}

const settle = () => new Promise(resolve => setTimeout(resolve, 0))

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
  globalThis.collectionServices.Update = () => {
    globalThis.collectionServices.Tree = () => CancellablePromise.resolve([collection('renamed')])
    return CancellablePromise.resolve()
  }
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
  globalThis.workspaceServices.Tabs = () => CancellablePromise.resolve(strip('active', [
    { id: 'active', kind: 'request', draftId: 'tab:active' },
    { id: 'removed', kind: 'request', ref: 'removed-node', draftId: 'removed-node' },
  ]))
  await tabs.load()
  await settle()
  globalThis.tabContent.calls.length = 0
  written.length = 0
  globalThis.collectionServices.Tree = () => CancellablePromise.resolve([collection('remaining')])
  await store.load()
  await settle()
  assert.deepEqual(tabs.tabs.map(tab => tab.id), ['active'])
  assert.deepEqual(globalThis.tabContent.calls, [['keep', ['active']], ['warm', 'active'], ['ensure', 'active']])
  assert.deepEqual(written, [['save', ['active'], 'active']])
  assert.deepEqual(globalThis.notices, [])
})

test('a failed collection refresh preserves tabs and a reset forgets the tree', async () => {
  setupCollections()
  const tabs = useTabsStore()
  await tabs.load()
  globalThis.collectionServices.Tree = () => CancellablePromise.reject(new Error('unavailable'))
  await store.load()
  assert.deepEqual(tabs.tabs.map(tab => tab.id), ['current'])
  const old = deferred()
  globalThis.collectionServices.Tree = () => old.promise
  const loading = store.load()
  store.reset()
  old.resolve([])
  await loading
  assert.deepEqual(tabs.tabs.map(tab => tab.id), ['current'])
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
  globalThis.collectionServices.Tree = () => CancellablePromise.resolve([collection('old')])
  old.resolve(collection('old'))
  await creating
  assert.deepEqual(store.tree, [])
  for (const key of ['pendingCreate', 'pendingRemove', 'pendingRename', 'pendingSection']) assert.equal(store[key], null)
  assert.deepEqual(globalThis.notices, [])
})

test('saving a draft adopts the original tab even if another tab becomes active', async () => {
  setupCollections()
  const tabs = useTabsStore()
  globalThis.workspaceServices.Tabs = () => CancellablePromise.resolve(strip('original', [
    { id: 'original', kind: 'request', draftId: 'tab:draft' },
  ]))
  await tabs.load()
  const old = deferred()
  globalThis.draftServices.SaveAsNode = () => old.promise
  const saving = store.saveDraft('folder', 'saved', 'tab:draft')
  tabs.newTab()
  globalThis.collectionServices.Tree = () => CancellablePromise.resolve([collection('folder')])
  old.resolve({ id: 'saved', collectionId: 'folder' })
  assert.equal(await saving, true)
  const original = tabs.tabs.find(tab => tab.id === 'original')
  assert.equal(original.ref, 'saved')
  assert.equal(original.draftId, 'saved')
  assert.notEqual(tabs.activeId, 'original')
  await settle()
  assert.ok(written.some(entry => entry[0] === 'discard' && entry[1] === 'tab:draft'))
})

test('finished collection runs supersede old reads and ignore other workspaces', async () => {
  setupCollections()
  store.tree = [collection('current')]
  const old = deferred()
  globalThis.runServices.Last = () => old.promise
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

test('deleting records says whether they are gone, and a refusal keeps them', async () => {
  setupRecords()
  store.prepend(record('kept'))
  globalThis.recordServices.Clear = () => CancellablePromise.reject(new Error('locked'))
  assert.equal(await store.clearRecords(['kept']), false)
  assert.deepEqual(store.records.map((one) => one.id), ['kept'])
  assert.equal(globalThis.notices.length, 1)

  globalThis.recordServices.Clear = () => CancellablePromise.resolve()
  assert.equal(await store.clearRecords(['kept']), true)
  assert.deepEqual(store.records, [])
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

test('a strip read with restore off starts fresh once per workspace and discards the old drafts', async () => {
  setupTabs()
  globalThis.settingsPort = { settings: { value: { restoreTabs: false } } }
  await store.load()
  assert.equal(store.tabs.length, 1)
  assert.notEqual(store.activeId, 'current')
  assert.ok(store.tabs[0].draftId.startsWith('tab:'))
  await settle()
  assert.deepEqual(written.filter(entry => entry[0] === 'discard'), [['discard', 'tab:current']])
  globalThis.workspaceServices.Tabs = () => CancellablePromise.resolve(strip('again'))
  await store.load()
  assert.equal(store.activeId, 'again')
})

test('workspace reset cancels tab reads and prevents old content warming', async () => {
  setupTabs()
  const old = deferred()
  globalThis.workspaceServices.Tabs = () => old.promise
  const loading = store.load()
  store.reset()
  old.resolve(strip('old'))
  await loading
  assert.deepEqual(store.tabs, [])
  assert.equal(store.activeId, '')
  assert.deepEqual(globalThis.tabContent.calls, [['keep', []]])
  assert.deepEqual(written, [])
  assert.deepEqual(globalThis.notices, [])
})

test('activating a tab counts every turn, even to the tab already shown', async () => {
  setupTabs()
  globalThis.workspaceServices.Tabs = () => CancellablePromise.resolve(strip('current', [
    { id: 'current', kind: 'request', draftId: 'tab:current' }, { id: 'other', kind: 'history' },
  ]))
  await store.load()
  store.activate('other')
  store.activate('current')
  assert.equal(store.activeId, 'current')
  assert.equal(store.activationTick, 2)
})

test('a strip that cannot be saved is reported and kept on screen', async () => {
  setupTabs()
  await store.load()
  await settle()
  globalThis.workspaceServices.SaveTabs = () => CancellablePromise.reject(new Error('write failed'))
  store.pin('current', true)
  await settle()
  assert.equal(store.tabs[0].pinned, true)
  assert.deepEqual(globalThis.notices, ['errors.tabsFailed'])
})

test('new tabs are marked blank before their content is warmed', async () => {
  setupTabs()
  await store.load()
  globalThis.tabContent.calls.length = 0
  store.newTab()
  const blank = store.activeId
  assert.deepEqual(globalThis.tabContent.calls, [
    ['born', blank], ['keep', ['current', blank]], ['warm', 'current'], ['warm', blank], ['ensure', blank],
  ])
})

test('a record opens as a request laid out from it, once', async () => {
  setupTabs()
  await store.load()
  const seeded = []
  globalThis.draftServices.FromRecord = (id, record) => { seeded.push([id, record]); return CancellablePromise.resolve() }
  await store.openRecord('rec-1')
  await store.openRecord('rec-1')
  assert.equal(seeded.length, 1)
  assert.equal(store.activeTab.record, 'rec-1')
  assert.equal(store.activeTab.draftId, seeded[0][0])
  assert.equal(store.tabs.length, 2)
})

test('adopting a saved node drops its previous content before warming the replacement', async () => {
  setupTabs()
  await store.load()
  globalThis.tabContent.calls.length = 0
  store.adoptNode('current', 'saved')
  assert.deepEqual(globalThis.tabContent.calls, [
    ['drop', 'current'], ['keep', ['current']], ['warm', 'current'], ['ensure', 'current'],
  ])
})

test('an empty stored strip opens on a blank request tab', async () => {
  setupTabs()
  globalThis.workspaceServices.Tabs = () => CancellablePromise.resolve({ workspace: 'workspace', tabs: null, active: '' })
  await store.load()
  assert.equal(store.tabs.length, 1)
  assert.equal(store.activeTab.kind, 'request')
  await settle()
  assert.deepEqual(written, [['save', [store.activeId], store.activeId]])
})

for (const operation of ['close', 'closeOthers', 'closeRight', 'openNode']) {
  test(`workspace reset while confirming ${operation} prevents the old command`, async () => {
    setupTabs()
    globalThis.workspaceServices.Tabs = () => CancellablePromise.resolve(strip('current', [
      { id: 'current', kind: 'request', preview: true, draftId: 'tab:current' },
      { id: 'other', kind: 'request', draftId: 'tab:other' },
    ]))
    await store.load()
    await settle()
    written.length = 0
    const confirmation = deferred()
    globalThis.tabContent.askUnsaved = () => confirmation.promise
    const pending = store[operation](operation === 'openNode' ? 'new-node' : 'current')
    store.reset()
    confirmation.resolve(true)
    await pending
    await settle()
    assert.deepEqual(written, [])
    assert.deepEqual(store.tabs, [])
  })
}

test('workspace reset while flushing a closing draft prevents closing', async () => {
  setupTabs()
  await store.load()
  await settle()
  written.length = 0
  const flushing = deferred()
  globalThis.tabContent.instances.set('current', { draft: {
    flush: () => { store.reset(); return flushing.promise },
  } })
  const pending = store.close('current')
  flushing.resolve(true)
  await pending
  await settle()
  assert.deepEqual(written, [])
})

test('a closed tab takes its draft with it once the strip is saved', async () => {
  setupTabs()
  await store.load()
  await settle()
  written.length = 0
  globalThis.tabContent.instances.set('current', { draft: { flush: async () => true } })
  await store.close('current')
  await settle()
  assert.deepEqual(store.tabs, [])
  assert.deepEqual(written, [['save', [], ''], ['discard', 'tab:current']])
})

test('failed draft flushing prevents a tab close', async () => {
  setupTabs()
  await store.load()
  globalThis.tabContent.instances.set('current', { draft: { flush: async () => false } })
  await store.close('current')
  assert.equal(store.activeId, 'current')
})

test('duplicating a tab waits for its buffered edits and copies its draft', async () => {
  setupTabs()
  await store.load()
  const flushing = deferred()
  const copies = []
  globalThis.tabContent.instances.set('current', { draft: { flush: () => flushing.promise } })
  globalThis.draftServices.Copy = (from, to) => { copies.push([from, to]); return CancellablePromise.resolve() }
  const copying = store.duplicate('current')
  assert.deepEqual(copies, [])
  flushing.resolve(true)
  await copying
  assert.equal(copies.length, 1)
  assert.equal(copies[0][0], 'tab:current')
  assert.equal(store.activeTab.draftId, copies[0][1])
  assert.equal(store.tabs[0].id, 'current')
})

test('failed draft flushing prevents duplication', async () => {
  setupTabs()
  await store.load()
  let copied = false
  globalThis.tabContent.instances.set('current', { draft: { flush: async () => false } })
  globalThis.draftServices.Copy = () => { copied = true; return CancellablePromise.resolve() }
  await store.duplicate('current')
  assert.equal(copied, false)
})

function draftForTest(persisted = true) {
  const draft = useDraft({ persisted: () => persisted, finished: async () => {}, scriptsLevel: () => null })
  globalThis.draftServices.Preview = () => CancellablePromise.resolve({ missing: [], projected: [], auto: [] })
  draft.load({ id: 'tab:draft', method: 'GET', url: '', body: '', params: [], headers: [], pathParams: [], form: [] })
  return draft
}

test('an edit stands at once and the whole draft is written a pause later', async (context) => {
  context.mock.timers.enable({ apis: ['setTimeout'] })
  const saved = []
  globalThis.draftServices.Save = (d) => { saved.push(d); return CancellablePromise.resolve() }
  const draft = draftForTest()
  draft.setUrl('https://api.example.test/users?page=2')
  await draft.setMethod('POST')
  assert.equal(draft.url.value, 'https://api.example.test/users?page=2')
  assert.equal(draft.changed.value, true)
  assert.deepEqual(saved, [])
  context.mock.timers.tick(400)
  await draft.flush()
  assert.equal(saved.length, 1)
  assert.equal(saved[0].method, 'POST')
  assert.equal(saved[0].params[0].name, 'page')
})

test('a failed save is written again by the next flush', async () => {
  globalThis.notices = []
  const draft = draftForTest()
  draft.setBody('{"a":1}')
  globalThis.draftServices.Save = () => CancellablePromise.reject(new Error('database write failed'))
  assert.equal(await draft.flush(), false)
  draft.setBody('{"a":2}')
  const saved = []
  globalThis.draftServices.Save = (d) => { saved.push(d.body); return CancellablePromise.resolve() }
  assert.equal(await draft.flush(), true)
  assert.deepEqual(saved, ['{"a":2}'])
})

test("a saved request's draft is never written down as a tab's", async () => {
  let saved = false
  globalThis.draftServices.Save = () => { saved = true; return CancellablePromise.resolve() }
  const draft = draftForTest(false)
  draft.setUrl('https://api.example.test')
  assert.equal(await draft.flush(), true)
  assert.equal(saved, false)
})

test('a parameter edited as a row writes the address', async () => {
  const draft = draftForTest(false)
  draft.setUrl('/a?include=x&page[number]=2')
  const [include, page] = draft.params.value
  await draft.patchRow('params', include.id, { value: 'author,comments' })
  assert.equal(draft.url.value, '/a?include=author,comments&page[number]=2')
  await draft.moveRow('params', page.id, 0)
  assert.equal(draft.url.value, '/a?page[number]=2&include=author,comments')
  await draft.toggleRow('params', page.id, false)
  assert.equal(draft.url.value, '/a?include=author,comments')
  draft.setUrl('/a?include=author,comments&w=1')
  assert.ok(draft.params.value.some(row => row.id === page.id && !row.enabled))
  assert.equal(draft.params.value[0].id, include.id)
})

test('sending sends the draft as it stands', async (context) => {
  const sent = []
  const original = globalThis.draftServices.Send
  context.after(() => { globalThis.draftServices.Send = original })
  globalThis.draftServices.Send = (d) => { sent.push(d.method); return CancellablePromise.resolve('sent') }
  const draft = draftForTest(false)
  await draft.setMethod('POST')
  await draft.send()
  assert.deepEqual(sent, ['POST'])
  assert.deepEqual(draft.sentIds.value, ['sent'])
})

test('a preview answer for a draft that has moved on is dropped', async () => {
  const draft = draftForTest(false)
  await settle()
  const first = deferred()
  globalThis.draftServices.Preview = () => first.promise
  const late = draft.refreshPreview()
  globalThis.draftServices.Preview = () => CancellablePromise.resolve({ missing: ['fresh'], projected: [], auto: [] })
  await draft.refreshPreview()
  first.resolve({ missing: ['stale'], projected: [], auto: [] })
  await late
  assert.deepEqual(draft.missing.value, ['fresh'])
})

test("a pasted command replaces the request and keeps the draft's id and environment", async () => {
  const draft = draftForTest(false)
  await draft.setEnvironmentOverride('env-1')
  globalThis.draftServices.Paste = () => CancellablePromise.resolve({
    reading: { kind: 'ok' },
    draft: { id: '', method: 'POST', url: 'https://api.example.test/a?x=1', body: 'b', headers: [], params: [], pathParams: [], form: [] },
  })
  const reading = await draft.pasteCommand('curl -X POST https://api.example.test/a?x=1')
  assert.equal(reading.kind, 'ok')
  assert.equal(draft.current.value.id, 'tab:draft')
  assert.equal(draft.environmentId.value, 'env-1')
  assert.equal(draft.method.value, 'POST')
  assert.equal(draft.params.value[0].name, 'x')
})

test('disposing the tab store clears content and cancels pending IPC', async () => {
  setupTabs()
  const old = deferred()
  globalThis.workspaceServices.Tabs = () => old.promise
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
  await settle()
  written.length = 0
  await store.closeOthers('missing')
  await store.closeRight('missing')
  await settle()
  assert.deepEqual(written, [])
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
  // The address is named by Go first; the read of the jar that follows is the one still in flight.
  await new Promise((resolve) => setTimeout(resolve, 0))
  globalThis.cookieServices.Jar = () => CancellablePromise.resolve([{ domain: 'new.example', cookies: [] }])
  await store.reset()
  old.resolve([{ domain: 'old.example', cookies: [] }])
  await previous
  assert.equal(store.domains[0].domain, 'new.example')
  assert.equal(store.domain, 'new.example')
})

test('the cookie manager opens on the domain Go names for the address', async () => {
  setupCookies()
  const asked = []
  globalThis.cookieServices.DomainOf = (address) => {
    asked.push(address)
    return CancellablePromise.resolve('localhost')
  }
  globalThis.cookieServices.Jar = () => CancellablePromise.resolve([{ domain: 'localhost', cookies: [] }])
  await store.openManager('https://localhost:8443/x')
  assert.deepEqual(asked, ['https://localhost:8443/x'])
  assert.equal(store.domain, 'localhost')
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
  globalThis.environmentServices.State = () => old.promise
  const previous = store.load()
  globalThis.environmentServices.State = () => CancellablePromise.resolve(snapshot('new'))
  await store.load()
  old.resolve(snapshot('old'))
  await previous
  assert.equal(store.activeId, 'new')
})

test('reset cancels an old workspace load without restoring state or showing an error', async () => {
  setup()
  const old = deferred()
  globalThis.environmentServices.State = () => old.promise
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
  globalThis.environmentServices.State = () => old.promise
  const previous = store.load()
  globalThis.environmentServices.Save = () => CancellablePromise.resolve()
  globalThis.environmentServices.State = () => CancellablePromise.resolve(snapshot('edited'))
  await store.renameEnv('current', 'edited')
  old.resolve(snapshot('old'))
  await previous
  assert.equal(store.activeId, 'edited')
})

test('a secret save after sheet closure does not restore its revealed value', async () => {
  setup()
  const variable = { id: 'secret', name: 'TOKEN', kind: VariableKind.VariableSecret, enabled: true }
  globalThis.environmentServices.State = () => CancellablePromise.resolve(snapshot('current', [variable]))
  await store.load()
  const edit = deferred()
  globalThis.environmentServices.SaveGlobals = () => edit.promise
  const pending = store.updateVar(null, 'secret', { value: 'new private value' })
  store.closeSheet()
  edit.resolve()
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
  globalThis.environmentServices.State = () => CancellablePromise.resolve(snapshot('current', [variable]))
  globalThis.environmentServices.Reveal = () => CancellablePromise.resolve('private value')
  globalThis.environmentServices.SaveGlobals = () => {
    globalThis.environmentServices.State = () => CancellablePromise.resolve(snapshot())
    return CancellablePromise.resolve()
  }
  await store.load()
  await store.reveal('secret')
  assert.equal(store.isRevealed('secret'), true)
  await store.clearGlobals()
  assert.equal(store.isRevealed('secret'), false)
})

test('a delayed reveal cannot restore a secret deleted while it was being read', async () => {
  setup()
  const variable = { id: 'secret', name: 'TOKEN', kind: VariableKind.VariableSecret, enabled: true }
  globalThis.environmentServices.State = () => CancellablePromise.resolve(snapshot('current', [variable]))
  globalThis.environmentServices.SaveGlobals = () => {
    globalThis.environmentServices.State = () => CancellablePromise.resolve(snapshot())
    return CancellablePromise.resolve()
  }
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
  globalThis.collectionServices.Update = () => CancellablePromise.resolve()
  globalThis.collectionServices.Tree = () => CancellablePromise.resolve([
    {...collection('parent'), children: [collection('child')]},
  ])
  await collections.setDescription('child', 'Updated folder description.')
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

test('a variable edit saves its whole scope and never sends a withheld secret as empty', async () => {
  setup()
  const variable = { id: 'secret', name: 'TOKEN', kind: VariableKind.VariableSecret, enabled: true, hasValue: true }
  globalThis.environmentServices.State = () => CancellablePromise.resolve(snapshot('current', [variable]))
  const sent = []
  globalThis.environmentServices.SaveGlobals = (vars) => {
    sent.push(vars)
    return CancellablePromise.resolve()
  }
  await store.load()
  await store.updateVar(null, 'secret', { name: 'API_TOKEN' })
  assert.equal(sent[0][0].name, 'API_TOKEN')
  assert.equal(sent[0][0].hasValue, true)
  await store.updateVar(null, 'secret', { value: '' })
  assert.equal(sent[1][0].hasValue, false)
  const added = await store.addVar(null, { name: 'HOST' })
  assert.deepEqual(sent[2].map((one) => one.name), ['TOKEN', 'HOST'])
  assert.equal(sent[2][1].id, added)
})
