import assert from 'node:assert/strict'
import { after, afterEach, test } from 'node:test'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { compileScript, parse } from '@vue/compiler-sfc'
import { createRenderer, effectScope, nextTick, reactive, ref, shallowReactive } from 'vue'
import { createServer, transformWithEsbuild } from 'vite'

globalThis.window = new EventTarget()
globalThis.document = Object.assign(new EventTarget(), { createElement: () => ({}) })
globalThis.vuePorts = {
  services: Object.fromEntries(['Collections', 'Draft', 'Records', 'Command', 'Scripting', 'Inspect'].map(name => [name, {}])),
  notices: [],
}
const ports = globalThis.vuePorts
const runtimePath = resolve('node_modules/@wailsio/runtime/dist').replaceAll('\\', '/')
const runtime = `export { CancellablePromise, CancelError } from '${runtimePath}/cancellable.js';
  export * as Create from '${runtimePath}/create.js';`
const stub = `import { defineComponent, h } from 'vue';
  const stub = name => defineComponent({ inheritAttrs: false,
    setup(_, { attrs, slots }) { return () => h('stub', { ...attrs, 'data-component': name }, slots.default?.()) }
  });`
const componentNames = ['TabStrip', 'Workspace', 'CollectionPage']
const vite = await createServer({
  configFile: false,
  ssr: { noExternal: ['@wailsio/runtime'] },
  optimizeDeps: { noDiscovery: true, include: [] },
  server: { middlewareMode: true, watch: null, hmr: false, ws: false },
  appType: 'custom',
  plugins: [{
    name: 'vue-lifecycle-ports',
    resolveId(id) {
      if (id === '@wailsio/runtime' || id === 'test:wails') return '\0test:wails'
    },
    async load(id) {
      if (id === '\0test:wails') return runtime
      const path = id.replaceAll('\\', '/')
      if (path.endsWith('/@wailsio/runtime/dist/index.js')) return runtime
      if (path.endsWith('/bindings/schemo/internal/transport/wails/index.ts')) {
        return Object.keys(ports.services).map(name =>
          `export const ${name}Service = globalThis.vuePorts.services.${name};`
        ).join('\n')
      }
      for (const name of ['tabs', 'records', 'collections', 'publications', 'transfer', 'workspaces']) {
        if (path.endsWith(`/src/stores/${name}.ts`)) {
          const title = name[0].toUpperCase() + name.slice(1)
          return `export const use${title}Store = () => globalThis.vuePorts.${name};`
        }
      }
      if (path.endsWith('/src/i18n.ts')) {
        return `export const t = key => key; export const useMessages = () => ({ t });
          export const describeFailure = String; export const formatMicros = String;`
      }
      if (path.endsWith('/src/composables/useToast.ts')) {
        return 'export const useToast = () => ({ show: message => globalThis.vuePorts.notices.push(message) })'
      }
      if (path.endsWith('/src/composables/useCommands.ts')) {
        return 'export const useCommands = () => ({ hint: () => "", matches: () => false })'
      }
      if (path.endsWith('/src/composables/useGettingStarted.ts')) return 'export function markStep() {}'
      if (path.endsWith('/src/composables/intents.ts')) {
        return 'export const saveRequest = { trigger() {} }; export function focusUrlField() {}'
      }
      if (path.endsWith('/src/composables/useSlotDrag.ts')) {
        return `import { ref } from 'vue'; export const useSlotDrag = () => ({
          drag: ref(null), settle: ref(null), noAnim: ref(false), press() {}, offsetOf: () => 0
        });`
      }
      if (path.endsWith('/components/ui/context-menu/index.ts')) {
        return stub + ['ContextMenu', 'ContextMenuContent', 'ContextMenuItem', 'ContextMenuSeparator', 'ContextMenuTrigger']
          .map(name => `export const ${name} = stub('${name}');`).join('\n')
      }
      if (path.endsWith('/components/ui/tabs/index.ts')) {
        return stub + ['Tabs', 'TabsList', 'TabsTrigger'].map(name => `export const ${name} = stub('${name}');`).join('\n')
      }
      if (path.endsWith('.vue')) {
        const name = path.split('/').at(-1).slice(0, -4)
        if (componentNames.includes(name)) {
          const { descriptor } = parse(await readFile(path, 'utf8'), { filename: path })
          const script = compileScript(descriptor, { id: name, inlineTemplate: true }).content
          return (await transformWithEsbuild(script, path, { loader: 'ts' })).code
        }
        return stub + `export default stub('${name}');`
      }
    },
  }],
})
after(() => vite.close())
const { CancellablePromise } = await vite.ssrLoadModule('test:wails')
const { RecordSource, TabKind } = await vite.ssrLoadModule('/bindings/schemo/internal/domain/models.ts')
const content = await vite.ssrLoadModule('/src/stores/content.ts')
const { useInspection } = await vite.ssrLoadModule('/src/composables/response/useInspection.ts')
const components = Object.fromEntries(await Promise.all(componentNames.map(async name => [
  name, (await vite.ssrLoadModule(`/src/components/${name === 'CollectionPage' ? 'collections' : 'layout'}/${name}.vue`)).default,
])))
afterEach(() => {
  content.keepOnly([])
  ports.notices.length = 0
  for (const service of Object.values(ports.services)) for (const key of Object.keys(service)) delete service[key]
})

function deferred() {
  let resolve
  const promise = new CancellablePromise(done => { resolve = done })
  return { promise, resolve }
}

async function settle() {
  for (let i = 0; i < 8; i++) await Promise.resolve()
  await nextTick()
}

function node(type, text = '') { return { type, text, props: {}, children: [], parent: null } }
function remove(child) {
  if (child.parent) child.parent.children.splice(child.parent.children.indexOf(child), 1)
  child.parent = null
}
function mount(context, component) {
  const root = node('root')
  const renderer = createRenderer({
    createElement: type => node(type), createText: text => node('text', text), createComment: text => node('comment', text),
    insert(child, parent, anchor) {
      remove(child)
      child.parent = parent
      const at = anchor ? parent.children.indexOf(anchor) : -1
      parent.children.splice(at < 0 ? parent.children.length : at, 0, child)
    },
    remove, parentNode: child => child.parent,
    nextSibling: child => child.parent?.children[child.parent.children.indexOf(child) + 1] ?? null,
    setText: (child, text) => { child.text = text },
    setElementText: (child, text) => { child.children = []; child.text = text },
    patchProp: (child, key, _, value) => { child.props[key] = value },
  })
  const app = renderer.createApp(component)
  app.mount(root)
  context.after(() => app.unmount())
  return root
}
function all(root) { return [root, ...root.children.flatMap(all)] }
function rendered(root, name) { return all(root).find(child => child.props['data-component'] === name) }

test('closing an inactive tab does not activate it on pointerdown', (context) => {
  const activated = []
  const closed = []
  ports.tabs = reactive({
    tabs: [{ id: 'active', kind: TabKind.TabKindRequest }, { id: 'other', kind: TabKind.TabKindRequest }],
    activeId: 'active', activate: id => activated.push(id), close: id => closed.push(id),
  })
  ports.records = { records: [] }
  ports.collections = { tree: [] }
  const root = mount(context, components.TabStrip)
  const other = all(root).filter(child => child.props.class?.split(' ').includes('tab'))[1]
  other.props.onMouseenter()
  return nextTick().then(() => {
    const button = all(other).find(child => child.type === 'button')
    assert.ok(button)
    let stopped = false
    const event = { button: 0, stopPropagation: () => { stopped = true } }
    for (let target = button; target && !stopped; target = target.parent) target.props.onPointerdown?.(event)
    button.props.onClick(event)
    assert.deepEqual(activated, [])
    assert.deepEqual(closed, ['other'])
  })
})

test('request editor is not mounted with placeholder values before its draft arrives', async (context) => {
  ports.tabs = shallowReactive({
    activeKind: TabKind.TabKindRequest, activeId: 'request', activeTab: { kind: TabKind.TabKindRequest },
    activeInstance: { kind: TabKind.TabKindRequest, reading: ref(true), node: ref(null) },
    pane: reactive({ draftId: null, blank: true }),
  })
  const root = mount(context, components.Workspace)
  assert.equal(rendered(root, 'RequestBuilder'), undefined)
  assert.equal(rendered(root, 'BlankRequest'), undefined)
  ports.tabs.pane.draftId = 'loaded'
  await nextTick()
  assert.ok(rendered(root, 'RequestBuilder'))
})

test('repeating a captured request opens its editable copy', async (context) => {
  const edited = []
  ports.tabs = shallowReactive({
    activeKind: TabKind.TabKindRequest, activeId: 'capture',
    activeTab: { kind: TabKind.TabKindRequest, draftId: 'capture-draft' },
    activeInstance: { kind: TabKind.TabKindRequest, reading: ref(false), node: ref(null) },
    pane: { draftId: 'capture-draft', blank: false, record: { id: 'record', source: RecordSource.SourceBrowser } },
    async editRecord(id) {
      edited.push(id)
      ports.tabs.activeId = 'copy'
      ports.tabs.activeTab = { kind: TabKind.TabKindRequest, draftId: 'copy-draft' }
      ports.tabs.pane = { draftId: 'copy-draft', blank: false }
    },
  })
  const root = mount(context, components.Workspace)
  const captured = rendered(root, 'CapturedRequestBar')
  assert.ok(captured)
  assert.equal(rendered(root, 'RequestBuilder'), undefined)
  await captured.props.onOpenInRequest()
  await settle()
  assert.deepEqual(edited, ['record'])
  assert.equal(rendered(root, 'CapturedRequestBar'), undefined)
  assert.equal(rendered(root, 'RequestBuilder').props.source.draftId, 'copy-draft')
})

test('saving a draft as a collection request replaces the editor that owned the old draft', async (context) => {
  ports.tabs = shallowReactive({
    activeKind: TabKind.TabKindRequest, activeId: 'request',
    activeTab: { kind: TabKind.TabKindRequest, draftId: 'old-draft' },
    activeInstance: { kind: TabKind.TabKindRequest, reading: ref(false), node: ref(null) },
    pane: { draftId: 'old-draft', blank: false },
  })
  const root = mount(context, components.Workspace)
  const original = rendered(root, 'RequestBuilder')
  ports.tabs.activeTab = { kind: TabKind.TabKindRequest, draftId: 'saved-node' }
  ports.tabs.pane = { draftId: 'saved-node', blank: false }
  await nextTick()
  assert.notEqual(rendered(root, 'RequestBuilder'), original)
})

test('closing a tab discards late reads and disposes its Vue watchers', async () => {
  const reading = deferred()
  ports.services.Collections.OpenNode = () => reading.promise
  const warming = content.warmContent({ id: 'closing', kind: TabKind.TabKindRequest, ref: 'saved' })
  const instance = content.contentOf('closing')
  content.dropContent('closing')
  reading.resolve({ node: { responses: [{ id: 'late' }] }, state: { draft: { id: 'saved', url: 'late', body: '' } } })
  await warming
  assert.equal(instance.node.value, null)
  instance.node.value = { node: { responses: [{ id: 'orphan' }] } }
  await nextTick()
  assert.deepEqual(instance.responses.value, [])
})

test('newer history responses cannot be overwritten by an older read', async () => {
  ports.services.Draft.Snapshot = () => CancellablePromise.resolve({ draft: { id: 'draft', url: '', body: '' } })
  const instance = await content.warmContent({ id: 'request', kind: TabKind.TabKindRequest, draftId: 'draft' })
  const older = deferred()
  ports.services.Records.Record = id => id === 'old' ? older.promise : CancellablePromise.resolve({
    id, responseBody: { inline: 'new body' },
  })
  const previous = instance.showRecord({ id: 'old' })
  await instance.showRecord({ id: 'new' })
  older.resolve({ id: 'old', responseBody: { inline: 'old body' } })
  await previous
  assert.equal(instance.record.value.id, 'new')
  assert.equal(instance.response.value.responseBody, 'new body')
})

test('all callers wait until the active tab has finished loading its response', async () => {
  ports.services.Draft.Snapshot = () => CancellablePromise.resolve({ draft: { id: 'draft', url: '', body: '' } })
  const reading = deferred()
  ports.services.Records.Record = () => reading.promise
  const tab = { id: 'request', kind: TabKind.TabKindRequest, draftId: 'draft', record: 'record' }
  const first = content.ensureContent(tab)
  await settle()
  let ready = false
  const second = content.ensureContent(tab).then(() => { ready = true })
  await settle()
  assert.equal(ready, false)
  reading.resolve({ id: 'record' })
  await Promise.all([first, second])
  assert.equal(ready, true)
})

test('a failed save in the unsaved dialog refuses closing the tab', async (context) => {
  context.mock.timers.enable({ apis: ['setTimeout'] })
  ports.services.Collections.OpenNode = () => CancellablePromise.resolve({
    node: { id: 'saved', name: 'Saved' }, state: { draft: { id: 'saved', url: '', body: '' } },
  })
  ports.services.Draft.SetText = (id, part) => CancellablePromise.resolve({
    field: part.field, rev: part.rev, draft: { id, url: part.text, body: '' },
  })
  ports.services.Collections.SaveNode = () => CancellablePromise.reject(new Error('write failed'))
  const instance = await content.warmContent({ id: 'request', kind: TabKind.TabKindRequest, ref: 'saved' })
  instance.draft.setUrl('https://edited.test')
  const leaving = content.askUnsaved('request')
  await content.answerUnsaved('save')
  assert.equal(await leaving, false)
  assert.equal(instance.dirty.value, true)
})

test('another close cannot replace an unanswered dialog, and dropping the tab cancels it', async (context) => {
  context.mock.timers.enable({ apis: ['setTimeout'] })
  ports.services.Collections.OpenNode = () => CancellablePromise.resolve({
    node: { id: 'saved', name: 'Saved' }, state: { draft: { id: 'saved', url: '', body: '' } },
  })
  const instance = await content.warmContent({ id: 'request', kind: TabKind.TabKindRequest, ref: 'saved' })
  instance.draft.setUrl('https://edited.test')
  const first = content.askUnsaved('request')
  assert.equal(await content.askUnsaved('request'), false)
  content.dropContent('request')
  assert.equal(await first, false)
  assert.equal(content.leavePending.value, null)
})

test('closing during a draft flush prevents the following collection save', async (context) => {
  context.mock.timers.enable({ apis: ['setTimeout'] })
  ports.services.Collections.OpenNode = () => CancellablePromise.resolve({
    node: { id: 'saved' }, state: { draft: { id: 'saved', url: '', body: '' } },
  })
  const writing = deferred()
  let pending
  let saved = false
  ports.services.Draft.SetText = (_, part) => { pending = part; return writing.promise }
  ports.services.Collections.SaveNode = () => { saved = true; return CancellablePromise.resolve({}) }
  const instance = await content.warmContent({ id: 'request', kind: TabKind.TabKindRequest, ref: 'saved' })
  instance.draft.setUrl('https://edited.test')
  const saving = instance.saveNode()
  content.dropContent('request')
  writing.resolve({ field: pending.field, rev: pending.rev, draft: { id: 'saved', url: pending.text, body: '' } })
  assert.equal(await saving, false)
  assert.equal(saved, false)
})

test('collection changes cancel old reads instead of rendering their rows', async (context) => {
  const older = deferred()
  ports.collections = reactive({ collectionId: 'old', tree: [{ id: 'old' }, { id: 'new' }], lastRun: null })
  ports.publications = { mark() {}, markOf: () => null }
  ports.transfer = {}
  ports.workspaces = {}
  ports.services.Collections.Contents = id => id === 'old' ? older.promise : CancellablePromise.resolve([{ id: 'new-row' }])
  ports.services.Scripting.Scripts = () => CancellablePromise.resolve(null)
  const root = mount(context, components.CollectionPage)
  ports.collections.collectionId = 'new'
  await settle()
  older.resolve([{ id: 'old-row' }])
  await settle()
  assert.deepEqual(rendered(root, 'CollectionOverview').props.rows.map(item => item.row.id), ['new-row'])
  assert.deepEqual(ports.notices, [])
})

test('an inherited publication notice opens the parent settings and follows publication state', async (context) => {
  const publication = ref(null)
  let opened = 0
  ports.collections = reactive({collectionId: 'folder', tree: [{id: 'folder', parentId: 'catalog'}]})
  ports.publications = {
    mark() {},
    markOf: () => publication.value,
    selectedCollection: '',
  }
  ports.tabs = {openPublications() { opened++ }}
  ports.transfer = {}
  ports.workspaces = {}
  ports.services.Collections.Contents = () => CancellablePromise.resolve([])
  ports.services.Scripting.Scripts = () => CancellablePromise.resolve(null)

  const root = mount(context, components.CollectionPage)
  assert.equal(rendered(root, 'SharePopover'), undefined)
  assert.equal(all(root).find(child => child.props.class === 'publication-notice'), undefined)

  publication.value = {inheritedFrom: 'Catalog', page: {collectionId: 'catalog', status: 'active'}}
  await settle()
  const notice = all(root).find(child => child.props.class === 'publication-notice')
  assert.ok(notice)
  notice.props.onClick()
  assert.equal(ports.publications.selectedCollection, 'catalog')
  assert.equal(opened, 1)
  assert.equal(rendered(root, 'SharePopover'), undefined)

  publication.value.page.pendingUnpublish = true
  await settle()
  assert.equal(all(root).find(child => child.props.class === 'publication-notice'), undefined)
})

function inspectionFor(context, body) {
  const scope = effectScope()
  context.after(() => scope.stop())
  return scope.run(() => useInspection(() => ({ text: body.value, contentType: 'application/json' })))
}

test('inspector shares document opening and ignores answers for the previous body', async (context) => {
  const older = deferred()
  let opened = 0
  ports.services.Inspect.Open = text => {
    opened++
    return text === 'old' ? older.promise : CancellablePromise.resolve({ id: 'new', kind: 'json' })
  }
  const body = ref('old')
  const inspection = inspectionFor(context, body)
  const first = inspection.ensure()
  const same = inspection.ensure()
  assert.equal(opened, 1)
  body.value = 'new'
  await inspection.ensure()
  older.resolve({ id: '', kind: '' })
  await Promise.all([first, same])
  assert.equal(inspection.readable.value, true)
  assert.equal(inspection.kind.value, 'json')
})

test('inspector keeps the latest selection when node reads finish out of order', async (context) => {
  ports.services.Inspect.Open = () => CancellablePromise.resolve({ id: 'document', kind: 'json' })
  const older = deferred()
  ports.services.Inspect.Node = (_, path) => path === 'old' ? older.promise : CancellablePromise.resolve({ path: 'new' })
  const inspection = inspectionFor(context, ref('{}'))
  const first = inspection.pickPath('old')
  await settle()
  await inspection.pickPath('new')
  older.resolve({ path: 'old' })
  await first
  assert.equal(inspection.node.value.path, 'new')
})
