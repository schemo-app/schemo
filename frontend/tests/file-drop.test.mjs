import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'
import { createRenderer, h, nextTick, reactive } from 'vue'
import { clientComponent, vite } from './vite.mjs'

globalThis.bodyDrop = { listeners: new Set() }
const ports = globalThis.bodyDrop
const load = await vite({
    '@wailsio/runtime': `export const Events = { On(name, handler) {
        if (name !== 'files-dropped') throw new Error('Unexpected event: ' + name);
        globalThis.bodyDrop.listeners.add(handler);
        return () => globalThis.bodyDrop.listeners.delete(handler);
    } };`,
    '/bindings/schemo/internal/request/index.ts': 'export const Drafts = {};',
    '/src/i18n/index.ts': 'export const t = key => key;',
    '/src/composables/useIndent.ts': 'export const useIndent = () => ({ indent: "two", insert() {} });',
    '/src/composables/useRowOrder.ts': `import { computed } from 'vue';
        export const useRowOrder = ({ rows }) => ({ press() {},
            states: computed(() => rows().map(() => ({ class: {}, style: {} }))) });`,
    '/src/lib/intents.ts': 'export const registerBodyEditor = () => () => {};',
    '/src/lib/calls.ts': 'export const callOrToast = call => call;',
}, {
    other(path) {
        if (!path.endsWith('.vue')) return
        if (path.endsWith('/BodyPane.vue') || path.endsWith('/FileDrop.vue')) return clientComponent(path)
        const name = path.split('/').at(-1).slice(0, -4)
        return `import { h } from 'vue'; export default { inheritAttrs: false,
            setup(_, { attrs, slots }) {
                return () => h('stub', { ...attrs, 'data-component': '${name}' }, [slots.default?.(), slots.tools?.()]);
            }
        };`
    },
})
const { default: BodyPane } = await load('/src/components/request/BodyPane.vue')
const { default: FileDrop } = await load('/src/components/ui/FileDrop.vue')
const { BodyKind, RowKind } = await load('/bindings/schemo/internal/domain/models.ts')

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
afterEach(() => {
    app?.unmount()
    app = null
    assert.equal(ports.listeners.size, 0)
})

function find(root, predicate) {
    if (predicate(root)) return root
    for (const child of root.children) {
        const match = find(child, predicate)
        if (match) return match
    }
}

function source(bodyKind = BodyKind.BodyBinary, form = []) {
    const writes = []
    return reactive({
        bodyKind, body: '', bodyFile: '', form, writes,
        async setBodyFile(path) { this.bodyFile = path; writes.push(['body', path]) },
        async patchRow(kind, id, patch) {
            Object.assign(this.form.find(row => row.id === id), patch)
            writes.push([kind, id, patch])
        },
        async pickBodyFile() { return '/tmp/picked.dat' },
        async flush() {},
    })
}

function mountContent(render) {
    const root = node('root')
    app = renderer.createApp({ render })
    app.mount(root)
    return root
}

function mount(...sources) {
    return mountContent(() => h('div', sources.map(source => h(BodyPane, { source }))))
}

async function drop(target, files = ['/tmp/upload.dat']) {
    for (const listener of ports.listeners) listener({ data: { target, files } })
    await nextTick()
}

function zone(root) {
    return find(root, element => element.props['data-file-drop-target'] !== undefined)
}

test('FileDrop preserves the root element, slot, attributes and clicks and supports multiple files', async () => {
    const options = reactive({ multiple: false })
    const received = []
    let clicks = 0
    const root = mountContent(() => h(FileDrop, {
        ...options, as: 'button', type: 'button', class: 'upload', 'aria-label': 'Upload', id: 'caller-id',
        onFiles: paths => received.push(paths), onClick: () => { clicks++ },
    }, { default: () => h('span', 'Upload files') }))
    const target = zone(root)
    assert.equal(target.type, 'button')
    assert.equal(target.props.type, 'button')
    assert.equal(target.props['aria-label'], 'Upload')
    assert.match(target.props.class, /\bupload\b/)
    assert.ok(find(target, element => element.text === 'Upload files'))
    assert.notEqual(target.props.id, 'caller-id')
    target.props.onClick()
    assert.equal(clicks, 1)

    await drop(target.props.id, ['/tmp/first.dat', '/tmp/second.dat'])
    assert.deepEqual(received, [['/tmp/first.dat']])
    options.multiple = true
    await nextTick()
    await drop(target.props.id, ['/tmp/first.dat', '/tmp/second.dat'])
    assert.deepEqual(received[1], ['/tmp/first.dat', '/tmp/second.dat'])
})

test('disabled FileDrop targets ignore native drops and can be enabled without remounting', async () => {
    const options = reactive({ disabled: true })
    const received = []
    const root = mountContent(() => h(FileDrop, { ...options, onFiles: paths => received.push(paths) }))
    const target = root.children[0]
    const id = target.props.id
    assert.equal(zone(root), undefined)
    await drop(id)
    assert.deepEqual(received, [])

    options.disabled = false
    await nextTick()
    assert.equal(zone(root).props.id, id)
    await drop(id)
    assert.deepEqual(received, [['/tmp/upload.dat']])

    options.disabled = true
    await nextTick()
    await drop(id)
    assert.equal(received.length, 1)
    app.unmount()
    app = null
    assert.equal(ports.listeners.size, 0)
    await drop(id)
    assert.equal(received.length, 1)
})

test('binary drops use the full native path, display its name and replace an existing file', async () => {
    const request = source()
    const root = mount(request)
    const target = zone(root).props.id

    await drop(target, ['C:\\uploads\\report.pdf', 'C:\\uploads\\ignored.pdf'])
    assert.equal(request.bodyFile, 'C:\\uploads\\report.pdf')
    assert.ok(find(root, element => element.text === 'report.pdf'))
    await drop(target, ['/tmp/replacement.dat'])
    assert.deepEqual(request.writes, [['body', 'C:\\uploads\\report.pdf'], ['body', '/tmp/replacement.dat']])
})

test('a multipart drop changes only the targeted file row and keeps its other fields', async () => {
    const request = source(BodyKind.BodyForm, [
        { id: 'text', name: 'title', value: 'Report', file: false, enabled: true },
        { id: 'first', name: 'attachment', src: '', file: true, enabled: false },
        { id: 'second', name: 'photo', src: '/tmp/previous.png', file: true, enabled: true },
    ])
    const root = mount(request)
    const targetRow = find(root, element => element.props['row-id'] === 'second')
    await drop(zone(targetRow).props.id, ['/tmp/photo.png'])

    assert.deepEqual(request.writes, [[RowKind.RowForm, 'second', { src: '/tmp/photo.png', file: true }]])
    assert.equal(request.form[0].value, 'Report')
    assert.equal(request.form[1].src, '')
    assert.equal(request.form[2].name, 'photo')
    assert.equal(request.form[2].enabled, true)
    assert.ok(find(root, element => element.text === 'photo.png'))
})

test('text bodies, empty drops and unrelated drop targets do not change the request', async () => {
    const request = source()
    const root = mount(request)
    const target = zone(root).props.id
    await drop(target, [])
    await drop(target, null)
    await drop('collection-import-drop')
    for (const kind of [BodyKind.BodyJSON, BodyKind.BodyXML, BodyKind.BodyRaw, BodyKind.BodyForm]) {
        request.bodyKind = kind
        await nextTick()
        await drop(target)
        assert.equal(zone(root), undefined)
    }
    assert.deepEqual(request.writes, [])
})

test('a drop for a deleted multipart row or a row switched to text is ignored', async () => {
    const request = source(BodyKind.BodyForm, [{ id: 'file', name: 'attachment', file: true }])
    const root = mount(request)
    const target = zone(root).props.id
    request.form[0].file = false
    await nextTick()
    await drop(target)
    request.form = []
    await nextTick()
    await drop(target)
    assert.deepEqual(request.writes, [])
})

test('mounted body panes have separate targets even when multipart row IDs match', async () => {
    const first = source(BodyKind.BodyForm, [{ id: 'same', name: 'file', file: true }])
    const second = source(BodyKind.BodyForm, [{ id: 'same', name: 'file', file: true }])
    const root = mount(first, second)
    const panes = root.children[0].children
    const firstTarget = zone(panes[0]).props.id
    const secondTarget = zone(panes[1]).props.id
    assert.notEqual(firstTarget, secondTarget)
    await drop(secondTarget)
    assert.deepEqual(first.writes, [])
    assert.equal(second.form[0].src, '/tmp/upload.dat')
})

test('the picker still works for binary bodies and multipart files', async () => {
    const request = source()
    const root = mount(request)
    find(root, element => element.props.class === 'drop-btn').props.onClick()
    await nextTick()
    assert.equal(request.bodyFile, '/tmp/picked.dat')
    request.bodyKind = BodyKind.BodyForm
    request.form = [{ id: 'file', name: 'attachment', file: true }]
    await nextTick()
    zone(root).props.onClick()
    await nextTick()
    assert.equal(request.form[0].src, '/tmp/picked.dat')
})
