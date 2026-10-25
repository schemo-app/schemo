import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'
import { createRenderer, nextTick, reactive } from 'vue'
import { clientComponent, vite } from './vite.mjs'

globalThis.importDrop = { listeners: new Map() }
const ports = globalThis.importDrop
const load = await vite({
    '@vueuse/core': 'export function useFocus() {}; export const useTimeoutFn = () => ({ start() {}, stop() {} });',
    '/src/i18n/index.ts': 'export const t = key => key; export const formatBytes = String;',
    '/src/stores/transfer.ts': 'export const useTransferStore = () => globalThis.importDrop.transfer;',
    '/src/stores/collections.ts': 'export const useCollectionsStore = () => ({ tree: [] });',
    '/src/composables/useWailsEvent.ts': `import { onScopeDispose } from 'vue';
        export function useWailsEvent(name, handler) {
            globalThis.importDrop.listeners.set(name, handler);
            onScopeDispose(() => globalThis.importDrop.listeners.delete(name));
        }`,
}, {
    other(path) {
        if (!path.endsWith('.vue')) return
        if (path.endsWith('/ImportDialog.vue')) return clientComponent(path)
        const name = path.split('/').at(-1).slice(0, -4)
        return `import { h } from 'vue'; export default { inheritAttrs: false,
            setup(_, { attrs, slots }) {
                return () => h('stub', { ...attrs, 'data-component': '${name}' }, [slots.default?.(), slots.foot?.()]);
            }
        };`
    },
})
const { default: ImportDialog } = await load('/src/components/transfer/ImportDialog.vue')

function node(type, text = '') {
    return { type, text, props: {}, children: [], parent: null, addEventListener() {}, removeEventListener() {} }
}

function remove(child) {
    if (child.parent) child.parent.children.splice(child.parent.children.indexOf(child), 1)
    child.parent = null
}

const renderer = createRenderer({
    createElement: node,
    createText: text => node('text', text),
    createComment: text => node('comment', text),
    setText: (element, text) => { element.text = text },
    setElementText: (element, text) => { element.text = text; element.children = [] },
    parentNode: element => element.parent,
    nextSibling: element => element.parent?.children[element.parent.children.indexOf(element) + 1] ?? null,
    patchProp: (element, key, previous, value) => { element.props[key] = value },
    remove,
    insert(child, parent, anchor = null) {
        remove(child)
        child.parent = parent
        const index = anchor ? parent.children.indexOf(anchor) : -1
        parent.children.splice(index < 0 ? parent.children.length : index, 0, child)
    },
})
let app
afterEach(() => app?.unmount())

function find(root, predicate) {
    if (predicate(root)) return root
    for (const child of root.children) {
        const match = find(child, predicate)
        if (match) return match
    }
}

async function mount() {
    ports.calls = []
    ports.transfer = reactive({
        importOpen: true, importParent: '', linkedPage: '', sources: [], busy: false,
        loadWorkspaces() {},
        async previewPath(...args) {
            ports.calls.push(args)
            return { token: 'drop', file: 'collection.json', requests: 3 }
        },
    })
    const root = node('root')
    app = renderer.createApp(ImportDialog)
    app.mount(root)
    find(root, element => element.props.name === 'transfer.source.postman').props.onClick()
    await nextTick()
    return root
}

async function drop(files = ['/tmp/collection.json'], target = 'collection-import-drop') {
    ports.listeners.get('files-dropped')({ data: { files, target } })
    await Promise.resolve()
    await nextTick()
}

test('the native drop zone previews the first file and shows its name', async () => {
    const root = await mount()
    const zone = find(root, element => element.props.id === 'collection-import-drop')
    assert.equal(zone.props['data-file-drop-target'], '')

    await drop(['/tmp/collection.json', '/tmp/other.json'])
    assert.deepEqual(ports.calls, [['postman', '/tmp/collection.json']])
    assert.ok(find(root, element => element.text === 'collection.json'))
})

test('empty drops, other targets, URL input and a closed dialog do not read files', async () => {
    const root = await mount()
    await drop([])
    await drop(null)
    await drop(['/tmp/other.json'], 'another-zone')
    const segment = find(root, element => element.props['data-component'] === 'Segment')
    segment.props.onPick('url')
    await nextTick()
    await drop()
    segment.props.onPick('file')
    ports.transfer.importOpen = false
    await nextTick()
    await drop()
    assert.deepEqual(ports.calls, [])
})

test('a pending drop cannot populate another input mode or a reopened dialog', async () => {
    const root = await mount()
    const answers = []
    ports.transfer.previewPath = () => new Promise(resolve => answers.push(resolve))
    await drop()
    const segment = find(root, element => element.props['data-component'] === 'Segment')
    segment.props.onPick('url')
    answers.shift()({ file: 'stale.json', token: 'stale' })
    await nextTick()
    assert.equal(find(root, element => element.text === 'stale.json'), undefined)

    segment.props.onPick('file')
    await nextTick()
    await drop()
    ports.transfer.importOpen = false
    await nextTick()
    ports.transfer.importOpen = true
    await nextTick()
    find(root, element => element.props.name === 'transfer.source.postman').props.onClick()
    answers.shift()({ file: 'stale.json', token: 'stale' })
    await nextTick()
    assert.ok(find(root, element => element.props.id === 'collection-import-drop'))
    assert.equal(find(root, element => element.text === 'stale.json'), undefined)
})

test('unmount removes the file drop listener', async () => {
    await mount()
    app.unmount()
    app = null
    assert.equal(ports.listeners.has('files-dropped'), false)
})
