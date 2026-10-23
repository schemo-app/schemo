import assert from 'node:assert/strict'
import { after, afterEach, test } from 'node:test'
import { resolve } from 'node:path'
import { pathToFileURL } from 'node:url'
import { createRenderer, createSSRApp, effectScope, nextTick, reactive, ref, shallowReactive, toRaw } from 'vue'
import { renderToString } from '@vue/server-renderer'
import { clientComponent, vite } from './vite.mjs'

globalThis.window = Object.assign(new EventTarget(), { setTimeout, clearTimeout })
globalThis.document = Object.assign(new EventTarget(), { createElement: () => ({}) })
const resizeObservers = new Set()
class ResizeObserverFake {
  constructor(callback) {
    this.callback = callback
    this.targets = new Set()
    resizeObservers.add(this)
  }
  observe(target) { this.targets.add(toRaw(target)) }
  disconnect() { this.targets.clear(); resizeObservers.delete(this) }
}
globalThis.ResizeObserver = window.ResizeObserver = ResizeObserverFake
function resize(target) {
  for (const observer of resizeObservers) {
    if (observer.targets.has(toRaw(target))) observer.callback([{ target }])
  }
}
globalThis.vuePorts = {
  services: Object.fromEntries(['Collections', 'Draft', 'Records', 'Scripting', 'Inspect', 'Publications', 'System'].map(name => [name, {}])),
  notices: [],
}
const ports = globalThis.vuePorts
ports.reka = await import('reka-ui')
ports.injectPopper = (await import(pathToFileURL(resolve('node_modules/reka-ui/dist/Popper/PopperRoot.js')).href)).injectPopperRootContext
const runtimePath = resolve('node_modules/@wailsio/runtime/dist').replaceAll('\\', '/')
const runtime = `export { CancellablePromise, CancelError } from '${runtimePath}/cancellable.js';
  export * as Create from '${runtimePath}/create.js'; export const Browser = {OpenURL() {}};
  export const Window = { async IsMaximised() { return false } }; export const Application = { Quit() {} };`
const stub = `import { defineComponent, h } from 'vue';
  const stub = name => defineComponent({ inheritAttrs: false,
    setup(_, { attrs, slots }) {
      if (globalThis.vuePorts.rekaMenus && name === 'TreeRowMenu')
        globalThis.vuePorts.menuPosition = globalThis.vuePorts.injectPopper();
      return () => h('stub', { ...attrs, 'data-component': name }, slots.default?.())
    }
  });
  const library = (name, actual = name) => defineComponent({ inheritAttrs: false,
    setup(_, { attrs, slots }) {
      if (globalThis.vuePorts.rekaMenus && name === 'ContextMenuTrigger')
        globalThis.vuePorts.triggerPosition = globalThis.vuePorts.injectPopper();
      if (globalThis.vuePorts.rekaMenus && name === 'PopoverTrigger')
        globalThis.vuePorts.popoverContext = globalThis.vuePorts.reka.injectPopoverRootContext();
      const component = globalThis.vuePorts.rekaMenus ? globalThis.vuePorts.reka[actual] : stub(name);
      return () => h(component, attrs, slots);
    }
  });`
const componentPaths = {
  AppTabStrip: 'layout', AppContent: 'layout', CollectionPage: 'collections', PublicationsPage: 'cloud',
  AppHeader: 'layout', AppSidebarRail: 'layout',
}
const componentNames = Object.keys(componentPaths)
const storeNames = ['tabs', 'records', 'collections', 'publications', 'transfer', 'workspaces', 'search', 'capture']
const stores = Object.fromEntries(storeNames.map((name) => [
  `/src/stores/${name}.ts`,
  `export const use${name[0].toUpperCase() + name.slice(1)}Store = () => globalThis.vuePorts.${name};` +
    (name === 'publications' ? 'export const hostOf = () => "share.example.com";' : ''),
]))
const load = await vite({
  '@wailsio/runtime': runtime,
  '/@wailsio/runtime/dist/index.js': runtime,
  // The window's own reka parts: stubs, or the library itself when a test turns menus on.
  'reka-ui': stub + `export const ContextMenuRoot = library('ContextMenu', 'ContextMenuRoot');
    export const ContextMenuTrigger = library('ContextMenuTrigger');
    export const PopoverRoot = library('Popover', 'PopoverRoot');
    export const PopoverTrigger = library('PopoverTrigger');` +
    [['TabsRoot', 'Tabs'], ['DropdownMenuRoot', 'DropdownMenu'], ['DropdownMenuTrigger', 'DropdownMenuTrigger']]
      .map(([name, shown]) => `export const ${name} = stub('${shown}');`).join('\n'),
  ...stores,
  '/bindings/schemo/internal/app/index.ts': 'export const System = globalThis.vuePorts.services.System',
  '/bindings/schemo/internal/workspace/index.ts': 'export const Collections = globalThis.vuePorts.services.Collections',
  '/bindings/schemo/internal/request/index.ts': `export const Drafts = globalThis.vuePorts.services.Draft;
    export const Inspector = globalThis.vuePorts.services.Inspect;
    export const History = globalThis.vuePorts.services.Records;
    export const Cookies = globalThis.vuePorts.services.Cookies;`,
  '/bindings/schemo/internal/script/index.ts': 'export const Scripts = globalThis.vuePorts.services.Scripting',
  '/bindings/schemo/internal/cloud/index.ts': 'export const Publications = globalThis.vuePorts.services.Publications',
  '/src/i18n/index.ts': `export const t = key => key;
    export const describeFailure = String; export const formatMicros = String; export const formatAgo = String;`,
  '/src/composables/useToast.ts': 'export const useToast = () => ({ show: message => globalThis.vuePorts.notices.push(message) })',
  '/src/composables/usePlatform.ts': `import { computed } from 'vue'; export const usePlatform = () => ({
      isMac: computed(() => globalThis.vuePorts.isMac !== false),
      customTitlebar: computed(() => globalThis.vuePorts.isMac === false)
    });`,
  '/src/composables/useSidebarToggle.ts': `import { ref } from 'vue'; export const useSidebarToggle = () => ({ hidden: ref(false), toggle() {} });`,
  '/src/composables/useCommands.ts': 'export const useCommands = () => ({ hint: () => "", matches: () => false })',
  '/src/composables/useGettingStarted.ts': 'export function markStep() {}',
  '/src/lib/intents.ts': 'export const saveRequest = { trigger() {} }; export function focusUrlField() {}',
  '/src/composables/useSlotDrag.ts': `import { ref } from 'vue'; export const useSlotDrag = () => ({
      drag: ref(null), settle: ref(null), noAnim: ref(false), press() {}, offsetOf: () => 0
    });`,
  '/src/components/ui/Input.vue': `import { defineComponent, h } from 'vue';
    export default defineComponent({ setup(_, { attrs, expose }) {
      expose({ focus() {}, select() {} });
      return () => h('stub', { ...attrs, 'data-component': 'Input' });
    } });`,
}, {
  // The components under test are real; every other one is a stub that only names itself.
  other: (path) => {
    if (!path.endsWith('.vue')) return undefined
    const name = path.split('/').at(-1).slice(0, -4)
    return componentNames.includes(name) ? clientComponent(path) : stub + `export default stub('${name}');`
  },
})
const { CancellablePromise } = await load('@wailsio/runtime')
const { RecordSource, TabKind } = await load('/bindings/schemo/internal/domain/models.ts')
const content = await load('/src/stores/content.ts')
const { useInspection } = await load('/src/composables/useInspection.ts')
const components = Object.fromEntries(await Promise.all(componentNames.map(async name => [
  name, (await load(`/src/components/${componentPaths[name]}/${name}.vue`)).default,
])))
afterEach(() => {
  content.keepOnly([])
  ports.notices.length = 0
  ports.isMac = true
  for (const service of Object.values(ports.services)) for (const key of Object.keys(service)) delete service[key]
  ports.services.Draft.Preview = async () => ({ missing: [], projected: [], auto: [] })
})
ports.services.Draft.Preview = async () => ({ missing: [], projected: [], auto: [] })

function deferred() {
  let resolve
  const promise = new CancellablePromise(done => { resolve = done })
  return { promise, resolve }
}

async function settle() {
  for (let i = 0; i < 8; i++) await Promise.resolve()
  await nextTick()
}

function node(type, text = '') {
  const element = ports.rekaMenus ? new Element() : {}
  return Object.assign(element, { type, nodeName: type.toUpperCase(), text, props: {}, style: {}, children: [], parent: null })
}
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

function withClass(root, name) {
  return all(root).find(child => typeof child.props.class === 'string' && child.props.class.split(' ').includes(name))
}

test('rail collection previews ignore hover and close for the context menu', async (context) => {
  const actions = []
  ports.tabs = {}
  ports.capture = {}
  ports.workspaces = reactive({ activeId: 'workspace', canWrite: true })
  ports.collections = reactive({
    tree: [{ id: 'collection', name: 'Collection', children: [{ id: 'folder', name: 'Folder' }] }],
    expanded: {},
    async select(id) { actions.push(['open', id]) },
    async duplicate(id) { actions.push(['duplicate', id]) },
    async remove(id) { actions.push(['remove', id]) },
  })
  ports.publications = {
    mark(id) { actions.push(['mark', id]) },
    openShare(id) { actions.push(['share', id]) },
  }
  ports.transfer = {
    askImport(id) { actions.push(['import', id]) },
    askExport(id) { actions.push(['export', id]) },
  }
  const root = mount(context, components.AppSidebarRail)
  assert.equal(all(root).filter(child => child.props['data-component'] === 'TreeRowMenu').length, 1)
  const trigger = all(root).find(child => child.type === 'button' && child.props.title === 'Collection')
  const preview = rendered(root, 'Popover')
  trigger.props.onMouseenter?.()
  await settle()
  assert.equal(preview.props.open, false)
  preview.props['onUpdate:open'](true)
  await settle()
  assert.equal(preview.props.open, true)
  trigger.props.onMouseleave?.()
  await settle()
  assert.equal(preview.props.open, true)
  preview.props['onUpdate:open'](false)
  await settle()
  assert.equal(preview.props.open, false)
  preview.props['onUpdate:open'](true)
  await settle()
  assert.equal(preview.props.open, true)
  const menu = rendered(root, 'ContextMenu')
  menu.props['onUpdate:open'](true)
  await settle()
  assert.equal(preview.props.open, false)
  trigger.props.onMouseenter?.()
  await settle()
  assert.equal(preview.props.open, false)
  const items = rendered(root, 'TreeRowMenu')
  await items.props.onSection('vars')
  assert.equal(ports.collections.pendingSection, 'vars')
  await items.props.onShare()
  items.props.onImport()
  items.props.onExport()
  await items.props.onDuplicate()
  await items.props.onRemove()
  assert.deepEqual(actions, [
    ['mark', 'collection'], ['open', 'collection'], ['open', 'collection'],
    ['share', 'collection'], ['import', 'collection'], ['export', 'collection'],
    ['duplicate', 'collection'], ['remove', 'collection'],
  ])
  menu.props['onUpdate:open'](false)
  trigger.props.onMouseenter?.()
  await settle()
  assert.equal(preview.props.open, false)
  preview.props['onUpdate:open'](true)
  await settle()
  assert.equal(preview.props.open, true)
  ports.workspaces.activeId = 'other-workspace'
  await settle()
  assert.equal(preview.props.open, false)
  ports.rekaMenus = true
  const previousElement = globalThis.Element
  globalThis.Element = class {
    contains(target) {
      for (let current = target; current; current = current.parent) if (current === this) return true
      return false
    }
  }
  try {
    const actual = mount(context, components.AppSidebarRail)
    await settle()
    const button = all(actual).find(child => child.type === 'button' && child.props.title === 'Collection')
    assert.equal(toRaw(ports.popoverContext.triggerElement.value), button)
    button.props.onClick()
    await settle()
    assert.equal(button.props['aria-expanded'], true)
    const menuTrigger = button.parent
    await menuTrigger.props.onPointerdown({ pointerType: 'mouse', button: 0 })
    await settle()
    assert.equal(button.props['aria-expanded'], true)
    button.props.onClick()
    await settle()
    assert.equal(button.props['aria-expanded'], false)
    button.props.onClick()
    await settle()
    const rightClick = { clientX: 20, clientY: 80, defaultPrevented: false, preventDefault() { this.defaultPrevented = true } }
    await menuTrigger.props.onContextmenu(rightClick)
    await settle()
    assert.equal(rightClick.defaultPrevented, true)
    assert.equal(button.props['aria-expanded'], false)
    await renderToString(createSSRApp(components.AppSidebarRail))
    assert.ok(ports.triggerPosition)
    assert.equal(ports.triggerPosition, ports.menuPosition)
  } finally {
    ports.rekaMenus = false
    globalThis.Element = previousElement
  }
})

test('rail name edits validate, save and cancel without opening the sidebar', async (context) => {
  const previousFrame = globalThis.requestAnimationFrame
  globalThis.requestAnimationFrame = callback => { callback(); return 0 }
  context.after(() => { globalThis.requestAnimationFrame = previousFrame })
  const saved = []
  ports.tabs = {}
  ports.capture = {}
  ports.publications = {}
  ports.transfer = {}
  ports.workspaces = { activeId: 'workspace', canWrite: true }
  ports.collections = reactive({
    tree: [{ id: 'collection', name: 'Collection' }], expanded: {},
    async rename(id, name) { saved.push(['rename', id, name]) },
    async createNode(id, name, method) { saved.push(['create', id, name, method]) },
  })
  const root = mount(context, components.AppSidebarRail)
  const menu = rendered(root, 'TreeRowMenu')
  const dialog = rendered(root, 'Dialog')
  const input = rendered(root, 'Input')
  const form = withClass(root, 'rail-name-form')
  const submit = () => form.props.onSubmit({ preventDefault() {} })
  menu.props.onRename()
  await settle()
  assert.equal(dialog.props.open, true)
  assert.equal(input.props.modelValue, 'Collection')
  input.props['onUpdate:modelValue']('  ')
  submit()
  await settle()
  assert.equal(input.props['aria-invalid'], true)
  assert.equal(dialog.props.open, true)
  input.props['onUpdate:modelValue'](' Renamed ')
  submit()
  await settle()
  assert.equal(dialog.props.open, false)
  assert.equal(dialog.props.title, 'collections.rename')
  assert.equal(input.props.modelValue, ' Renamed ')
  menu.props.onNewRequest()
  await settle()
  input.props['onUpdate:modelValue']('New request')
  submit()
  await settle()
  menu.props.onRename()
  await settle()
  dialog.props['onUpdate:open'](false)
  await settle()
  assert.equal(dialog.props.open, false)
  assert.deepEqual(saved, [
    ['rename', 'collection', 'Renamed'],
    ['create', 'collection', 'New request', 'GET'],
  ])
})

test('native controls receive the measured header height and follow arbitrary layout changes', async (context) => {
  ports.isMac = true
  ports.search = { openPalette() {} }
  ports.tabs = reactive({ tabs: [], activeId: '' })
  ports.records = { records: [] }
  ports.collections = { tree: [] }
  const heights = []
  ports.services.System.SetHeaderHeight = async (height) => heights.push(height)
  const root = mount(context, components.AppHeader)
  await settle()
  const header = withClass(root, 'app-header')
  let height = 43.75
  header.getBoundingClientRect = () => ({ height })
  resize(header)
  await settle()
  resize(header)
  await settle()
  height = 57.125
  resize(header)
  await settle()
  height = 0
  resize(header)
  await settle()
  assert.deepEqual(heights, [43.75, 57.125])
})

test('custom titlebars do not send native macOS geometry', async (context) => {
  ports.isMac = false
  ports.search = { openPalette() {} }
  ports.tabs = reactive({ tabs: [], activeId: '' })
  ports.records = { records: [] }
  ports.collections = { tree: [] }
  const heights = []
  ports.services.System.SetHeaderHeight = async (height) => heights.push(height)
  const root = mount(context, components.AppHeader)
  await settle()
  const header = withClass(root, 'app-header')
  header.getBoundingClientRect = () => ({ height: 57.125 })
  resize(header)
  await settle()
  assert.deepEqual(heights, [])
})

test('opening publications waits for its editor and details and keeps content visible during refresh', async (context) => {
  const listing = deferred()
  const editor = deferred()
  const details = deferred()
  const page = {collectionId: 'collection', status: 'active', visibility: 'password', revision: 1, canManage: true}
  let edits = 0
  ports.collections = {tree: [{id: 'collection', name: 'Collection'}]}
  ports.workspaces = {activeId: 'workspace', canWrite: true}
  ports.tabs = {}
  ports.publications = reactive({
    pages: [], selectedCollection: '', collection: '', loading: false, reading: false,
    visibility: 'unlisted', password: '', environments: [], changes: [], options: null,
    async load() { await listing.promise; this.pages = [page] },
    async refresh() {}, async markAll() {}, markOf() { return null }, closeShare() {},
    async edit(id) {
      edits++
      this.collection = id
      await editor.promise
      this.visibility = 'password'
      this.password = 'saved-password'
      this.options = {docs: true, examples: true}
    },
  })
  ports.services.Publications.Details = () => details.promise
  const root = mount(context, components.PublicationsPage)
  assert.equal(withClass(root, 'empty-card'), undefined)
  listing.resolve()
  await settle()
  const pane = withClass(root, 'pane-inner')
  assert.equal(pane.style.display, 'none')
  details.resolve({views: 17, imports: 3, versions: []})
  await settle()
  assert.equal(pane.style.display, 'none')
  editor.resolve()
  await settle()
  assert.notEqual(pane.style.display, 'none')
  assert.equal(rendered(root, 'PublicationPassword').props.modelValue, 'saved-password')
  const newer = deferred()
  ports.services.Publications.Details = () => newer.promise
  ports.publications.pages = [{...page, revision: 2}]
  await settle()
  assert.equal(withClass(root, 'pane-inner'), pane)
  assert.notEqual(pane.style.display, 'none')
  assert.equal(edits, 1)
  newer.resolve({views: 19, imports: 4, versions: []})
  await settle()
  assert.notEqual(pane.style.display, 'none')
})

test('an empty local publication list does not show the empty state while the server refresh is pending', async (context) => {
  const refresh = deferred()
  ports.collections = {tree: []}
  ports.workspaces = {canWrite: true}
  ports.tabs = {}
  ports.publications = reactive({
    pages: [], selectedCollection: '', loading: false,
    async load() {}, refresh: () => refresh.promise, async markAll() {}, closeShare() {},
  })
  const root = mount(context, components.PublicationsPage)
  await settle()
  assert.equal(withClass(root, 'empty-card'), undefined)
  refresh.resolve()
  await settle()
  assert.ok(withClass(root, 'empty-card'))
})

test('closing an inactive tab does not activate it on pointerdown', (context) => {
  const activated = []
  const closed = []
  ports.tabs = reactive({
    tabs: [{ id: 'active', kind: TabKind.TabKindRequest }, { id: 'other', kind: TabKind.TabKindRequest }],
    activeId: 'active', activate: id => activated.push(id), close: id => closed.push(id),
  })
  ports.records = { records: [] }
  ports.collections = { tree: [] }
  const root = mount(context, components.AppTabStrip)
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
  const root = mount(context, components.AppContent)
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
  const root = mount(context, components.AppContent)
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
  const root = mount(context, components.AppContent)
  const original = rendered(root, 'RequestBuilder')
  ports.tabs.activeTab = { kind: TabKind.TabKindRequest, draftId: 'saved-node' }
  ports.tabs.pane = { draftId: 'saved-node', blank: false }
  await nextTick()
  assert.notEqual(rendered(root, 'RequestBuilder'), original)
})

test('closing a tab discards late reads and disposes its Vue watchers', async () => {
  const reading = deferred()
  ports.services.Draft.OpenNode = () => reading.promise
  const warming = content.warmContent({ id: 'closing', kind: TabKind.TabKindRequest, ref: 'saved' })
  const instance = content.contentOf('closing')
  content.dropContent('closing')
  reading.resolve({ node: { responses: [{ id: 'late' }] }, draft: { id: 'saved', url: 'late', body: '' } })
  await warming
  assert.equal(instance.node.value, null)
  instance.node.value = { node: { responses: [{ id: 'orphan' }] } }
  await nextTick()
  assert.deepEqual(instance.responses.value, [])
})

test('newer history responses cannot be overwritten by an older read', async () => {
  ports.services.Draft.Open = () => CancellablePromise.resolve({ id: 'draft', url: '', body: '' })
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
  ports.services.Draft.Open = () => CancellablePromise.resolve({ id: 'draft', url: '', body: '' })
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
  ports.services.Draft.OpenNode = () => CancellablePromise.resolve({
    node: { id: 'saved', name: 'Saved' }, draft: { id: 'saved', url: '', body: '' },
  })
  ports.services.Draft.SaveNode = () => CancellablePromise.reject(new Error('write failed'))
  const instance = await content.warmContent({ id: 'request', kind: TabKind.TabKindRequest, ref: 'saved' })
  instance.draft.setUrl('https://edited.test')
  const leaving = content.askUnsaved('request')
  await content.answerUnsaved('save')
  assert.equal(await leaving, false)
  assert.equal(instance.dirty.value, true)
})

test('another close cannot replace an unanswered dialog, and dropping the tab cancels it', async (context) => {
  context.mock.timers.enable({ apis: ['setTimeout'] })
  ports.services.Draft.OpenNode = () => CancellablePromise.resolve({
    node: { id: 'saved', name: 'Saved' }, draft: { id: 'saved', url: '', body: '' },
  })
  const instance = await content.warmContent({ id: 'request', kind: TabKind.TabKindRequest, ref: 'saved' })
  instance.draft.setUrl('https://edited.test')
  const first = content.askUnsaved('request')
  assert.equal(await content.askUnsaved('request'), false)
  content.dropContent('request')
  assert.equal(await first, false)
  assert.equal(content.leavePending.value, null)
})

test('a node saved while its tab closes is not adopted by the closed tab', async (context) => {
  context.mock.timers.enable({ apis: ['setTimeout'] })
  ports.services.Draft.OpenNode = () => CancellablePromise.resolve({
    node: { id: 'saved' }, draft: { id: 'saved', url: '', body: '' },
  })
  const writing = deferred()
  ports.services.Draft.SaveNode = () => writing.promise
  const instance = await content.warmContent({ id: 'request', kind: TabKind.TabKindRequest, ref: 'saved' })
  instance.draft.setUrl('https://edited.test')
  const saving = instance.saveNode()
  content.dropContent('request')
  writing.resolve({ node: { id: 'saved' }, draft: { id: 'saved', url: 'https://edited.test', body: '' } })
  assert.equal(await saving, false)
  assert.equal(instance.node.value.draft.url, '')
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
