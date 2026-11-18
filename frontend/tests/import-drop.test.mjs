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
    '/src/stores/collections.ts': 'export const useCollectionsStore = () => ({ tree: globalThis.importDrop.tree });',
    '/src/composables/useWailsEvent.ts': `import { onScopeDispose } from 'vue';
        export function useWailsEvent(name, handler) {
            globalThis.importDrop.listeners.set(name, handler);
            onScopeDispose(() => globalThis.importDrop.listeners.delete(name));
        }`,
}, {
    other(path) {
        if (!path.endsWith('.vue')) return
        if (path.endsWith('/ImportDialog.vue') || path.endsWith('/FileDrop.vue')) return clientComponent(path)
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

async function mount(format = 'postman', parent = '') {
    ports.calls = []
    ports.tree = parent ? [{ id: parent, name: 'Example' }] : []
    ports.transfer = reactive({
        importOpen: true, importParent: parent, linkedPage: '', sources: [], busy: false,
        loadWorkspaces() {},
        async previewPath(...args) {
            ports.calls.push(args)
            return { token: 'drop', file: 'collection.json', requests: 3 }
        },
    })
    const root = node('root')
    app = renderer.createApp(ImportDialog)
    app.mount(root)
    find(root, element => element.props.name === `transfer.source.${format}`).props.onClick()
    await nextTick()
    ports.root = root
    return root
}

function zone(root) {
    return find(root, element => element.props['data-file-drop-target'] !== undefined)
}

async function drop(files = ['/tmp/collection.json'], target = zone(ports.root)?.props.id ?? 'removed-zone') {
    ports.listeners.get('files-dropped')?.({ data: { files, target } })
    await Promise.resolve()
    await nextTick()
}

test('the native drop zone previews the first file and shows its name', async () => {
    const root = await mount()
    assert.equal(zone(root).props['data-file-drop-target'], '')

    await drop(['/tmp/collection.json', '/tmp/other.json'])
    assert.deepEqual(ports.calls, [['postman', '/tmp/collection.json']])
    assert.ok(find(root, element => element.text === 'collection.json'))
})

test('the Insomnia drop zone previews a folder in the same dialog', async () => {
    const root = await mount('insomnia')
    ports.transfer.previewPath = async (...args) => {
        ports.calls.push(args)
        return { token: 'drop', file: 'insomnia-exports', requests: 3, collections: 2 }
    }
    await drop(['/tmp/insomnia-exports'])
    assert.deepEqual(ports.calls, [['insomnia', '/tmp/insomnia-exports']])
    assert.ok(find(root, element => element.text === 'insomnia-exports'))
})

test('choosing an Insomnia source calls the system picker and shows its preview', async () => {
    const root = await mount('insomnia')
    ports.transfer.pickImport = async (...args) => {
        ports.calls.push(args)
        return { token: 'picked', file: 'insomnia-exports', requests: 3, collections: 2 }
    }
    await zone(root).props.onClick()
    await nextTick()
    assert.deepEqual(ports.calls, [['insomnia', 'transfer.from']])
    assert.ok(find(root, element => element.text === 'insomnia-exports'))
})

test('the import button spins while the confirmed import is pending and recovers on failure', async () => {
    const root = await mount('insomnia')
    let finishImport
    ports.transfer.apply = () => new Promise(resolve => { finishImport = resolve })
    const button = find(root, element => element.props['data-component'] === 'DialogButton' && 'primary' in element.props)
    const spinner = () => find(button, element => element.props['data-component'] === 'Spinner')

    await drop(['/tmp/insomnia-exports'])
    assert.equal(spinner(), undefined)

    const importing = button.props.onClick()
    await nextTick()
    assert.equal(button.props.disabled, true)
    assert.equal(button.props['aria-busy'], true)
    assert.ok(spinner())

    finishImport(false)
    await importing
    await nextTick()
    assert.equal(button.props.disabled, false)
    assert.equal(spinner(), undefined)
})

test('an environment import opened from a collection defaults to that collection', async () => {
    const root = await mount('postman', 'example-collection')
    ports.transfer.previewPath = async () => ({
        token: 'env', file: 'environment.json', collections: 0, requests: 0,
        environments: 1, looseEnvironments: 1,
    })
    ports.transfer.apply = async (...args) => { ports.calls.push(args); return false }
    await drop()
    const destination = find(root, element => element.props.label === 'transfer.envDestination')
    const button = find(root, element => element.props['data-component'] === 'DialogButton' && 'primary' in element.props)
    assert.equal(destination.props.modelValue, 'example-collection')
    await button.props.onClick()
    assert.equal(ports.calls[0][1], 'example-collection')
    assert.equal(ports.calls[0][3].environmentTarget, 'collection')
})

test('legacy bundled environments can be attached to a chosen imported collection', async () => {
    const root = await mount('schemo')
    ports.transfer.previewPath = async () => ({
        token: 'legacy', file: 'legacy.json', collections: 2, requests: 2,
        environments: 1, looseEnvironments: 1, collectionNames: ['First', 'Second'],
    })
    ports.transfer.apply = async (...args) => { ports.calls.push(args); return false }
    await drop()
    const destination = find(root, element => element.props.label === 'transfer.looseEnvironments')
    assert.equal(destination.props.modelValue, 'skip')
    destination.props['onUpdate:modelValue']('collection')
    await nextTick()
    const owner = find(root, element => element.props.label === 'transfer.collection')
    owner.props['onUpdate:modelValue']('1')
    await nextTick()
    const button = find(root, element => element.props['data-component'] === 'DialogButton' && 'primary' in element.props)
    await button.props.onClick()
    assert.equal(ports.calls[0][3].environmentTarget, 'collection')
    assert.equal(ports.calls[0][3].environmentCollection, 1)
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
    assert.ok(zone(root))
    assert.equal(find(root, element => element.text === 'stale.json'), undefined)
})

test('unmount removes the file drop listener', async () => {
    await mount()
    app.unmount()
    app = null
    assert.equal(ports.listeners.has('files-dropped'), false)
})
