import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createRenderer, h, markRaw, nextTick, reactive } from 'vue'
import { clientComponent, vite } from './vite.mjs'

globalThis.window = new EventTarget()
globalThis.Document = class Document { activeElement = null }
globalThis.document = new Document()
const load = await vite({
    '/src/i18n/index.ts': 'export const t = key => key',
    '/src/lib/varPeek.ts': 'export const vVarPeek = {}',
    '/bindings/schemo/internal/request/index.ts': 'export const Drafts = {Paste: text => globalThis.pasteRowsCommand(text)}',
    '/src/composables/usePlatform.ts': `import {ref} from 'vue';
        export const usePlatform = () => ({isMac: ref(false), shortcut: key => 'Ctrl+' + key});`,
    '/src/composables/useSettings.ts': `import {ref} from 'vue';
        export const useSetting = key => ref(key === 'requests' ? {showAuto: true} : 0);
        export const useSettings = () => ({setLayout() {}});`,
    '/src/stores/collections.ts': 'export const useCollectionsStore = () => ({tree: []})',
    '/src/composables/useSlidingPill.ts': `import {ref} from 'vue'; export const useSlidingPill = () => ({style: ref({}), ready: ref(true)});`,
    '/src/composables/useResizeDrag.ts': `import {ref} from 'vue'; export const useResizeDrag = () => ({startDrag() {}, dragging: ref(false)});`,
    '/src/composables/useRowOrder.ts': `import {computed} from 'vue';
        export const useRowOrder = ({rows}) => ({press() {}, states: computed(() => rows().map(() => ({class: {}, style: {}})))});`,
}, {
    other(path) {
        if (['RequestPane', 'RowsPane', 'BulkRowsEditor', 'KvRow'].some(name => path.endsWith(`/request/${name}.vue`))) return clientComponent(path)
        if (path.endsWith('.vue')) return 'export default {render() {return null}}'
    },
})
const { default: RowsPane } = await load('/src/components/request/RowsPane.vue')
const { default: RequestPane } = await load('/src/components/request/RequestPane.vue')

function mount(context, kind = 'headers', requestPane = false) {
    const nodes = []
    const writes = []
    const renderer = createRenderer({
        createElement(type) {
            const node = markRaw({type, tagName: type.toUpperCase(), props: {}, children: [], value: '',
                addEventListener() {}, removeEventListener() {}, setSelectionRange() {},
                focus() {globalThis.document.activeElement = this},
                querySelector(selector) {
                    const id = selector.match(/^\[data-row="(.+)"\]$/)?.[1]
                    const find = held => held.props?.['data-row'] === id ? held : held.children?.map(find).find(Boolean)
                    return find(this)
                },
                getRootNode() {return globalThis.document},
                closest() {return ['input', 'textarea'].includes(type) ? this : null},
            })
            nodes.push(node)
            return node
        },
        createText: text => ({text}), createComment: () => ({}),
        setText(node, text) {node.text = text}, setElementText(node, text) {node.text = text},
        patchProp(node, key, previous, value) {node.props[key] = value},
        insert(node, parent, anchor) {
            if (node.parent) node.parent.children.splice(node.parent.children.indexOf(node), 1)
            node.parent = parent
            const index = anchor ? parent.children.indexOf(anchor) : -1
            parent.children.splice(index < 0 ? parent.children.length : index, 0, node)
        },
        remove(node) {
            if (node.parent) node.parent.children.splice(node.parent.children.indexOf(node), 1)
            const remove = held => {held.removed = true; held.children?.forEach(remove)}
            remove(node)
        },
        parentNode: node => node?.parent,
        nextSibling: node => node?.parent?.children[node.parent.children.indexOf(node) + 1] ?? null,
    })
    const source = reactive({
        headers: [{id: 'header', name: 'Accept', value: 'application/json', enabled: true, description: 'Keep the note'}],
        params: [], pathParams: [], projected: [], auto: [], url: 'https://original.test', method: 'GET', body: '',
        openChip: kind, nodeId: null, responses: [], enabledHeadersCount: 1, enabledParamsCount: 0,
        setOpenChip(chip) {this.openChip = chip},
        replaceRows(kind, rows) {writes.push(rows); this[kind] = rows},
        async addRow() {
            const id = `header-${this.headers.length}`
            this.headers.push({id, name: '', value: '', enabled: true})
            return id
        },
    })
    const toolbar = {children: []}
    const app = renderer.createApp({render: () => requestPane ? h(RequestPane, {source}) : h(RowsPane, {source, kind, toolbar})})
    app.mount({children: []})
    context.after(() => app.unmount())
    const mode = () => nodes.find(node => !node.removed && node.props.class?.includes('rows-mode'))
    const editor = () => nodes.find(node => !node.removed && node.type === 'textarea')
    const table = () => nodes.find(node => !node.removed && node.props.class?.split(' ').includes('kv'))
    return {source, writes, toolbar, mode, editor, table}
}

test('text mode applies valid edits immediately and refuses to discard invalid input when switching to the table', async context => {
    const {source, writes, toolbar, mode, editor} = mount(context)
    assert.equal(mode('table').parent, toolbar)
    mode('text').props.onClick()
    await nextTick()
    assert.equal(editor().value, 'Accept: application/json')
    editor().props['onUpdate:modelValue']('Accept: text/plain')
    await nextTick()
    assert.equal(writes.length, 1)
    assert.equal(source.headers[0].id, 'header')
    assert.equal(source.headers[0].description, 'Keep the note')
    editor().props['onUpdate:modelValue']('Accept: text/plain\nmissing separator')
    await nextTick()
    assert.equal(editor().props['aria-invalid'], true)
    assert.equal(writes.length, 1)
    mode('table').props.onClick()
    await nextTick()
    assert.ok(editor())
    assert.equal(mode('text').props['aria-pressed'], true)
    editor().props['onUpdate:modelValue']('Accept: text/plain\nX-Debug: 1')
    await nextTick()
    assert.equal(writes.length, 2)
    mode('table').props.onClick()
    await nextTick()
    assert.equal(editor(), undefined)
    assert.equal(source.headers[1].name, 'X-Debug')
})

test('the request remembers a shared row editing mode across parameters, headers and other sections', async context => {
    const {source, mode, editor} = mount(context, 'headers', true)
    await nextTick()
    assert.equal(editor(), undefined)
    mode().props.onClick()
    await nextTick()
    assert.ok(editor())
    source.setOpenChip('params')
    await nextTick()
    assert.ok(editor())
    assert.equal(mode().props['aria-pressed'], true)
    source.setOpenChip('auth')
    await nextTick()
    source.setOpenChip('headers')
    await nextTick()
    assert.ok(editor())
    mode().props.onClick()
    await nextTick()
    assert.equal(editor(), undefined)
    source.setOpenChip('params')
    await nextTick()
    assert.equal(editor(), undefined)
    assert.equal(mode().props['aria-pressed'], false)
})

for (const prefix of ['#', '//']) {
    test(`commenting an invalid row with ${prefix} clears the error and allows table mode`, async context => {
        const {source, mode, editor} = mount(context)
        mode('text').props.onClick()
        await nextTick()
        editor().props['onUpdate:modelValue']('Accept: application/json\nmissing separator')
        await nextTick()
        assert.equal(editor().props['aria-invalid'], true)
        editor().props['onUpdate:modelValue'](`Accept: application/json\n${prefix} missing separator`)
        await nextTick()
        assert.equal(editor().props['aria-invalid'], false)
        assert.equal(source.headers.length, 1)
        assert.equal(source.headers[0].id, 'header')
        mode('table').props.onClick()
        await nextTick()
        assert.equal(editor(), undefined)
    })
}

test('adding a header replaces the add-row position and focuses its name before automatic headers', async context => {
    const {source, table} = mount(context)
    source.auto = [{name: 'User-Agent', value: 'Schemo/2.0'}]
    await nextTick()
    const rows = () => table().children.filter(node => node.props?.class?.split(' ').includes('kv-row'))
    for (let index = 0; index < 2; index++) {
        const before = rows()
        const position = before.findIndex(node => node.props.class.includes('kv-add'))
        assert.ok(before[position + 1].props.class.includes('header-auto-row'))
        await before[position].props.onClick()
        await nextTick()
        const after = rows()
        const added = source.headers.at(-1)
        assert.equal(after[position].props['data-kv'], added.id)
        assert.ok(after[position + 1].props.class.includes('kv-add'))
        assert.ok(after[position + 2].props.class.includes('header-auto-row'))
        assert.equal(globalThis.document.activeElement.props['data-row'], added.id)
    }
})

async function paste(table, text, target = table) {
    let prevented = false
    await table.props.onPaste({target, clipboardData: {getData: () => text}, preventDefault() {prevented = true}})
    return prevented
}

test('table paste replaces matching headers and leaves automatic rows and the request intact', async context => {
    const {source, table} = mount(context)
    source.auto = [{name: 'User-Agent', value: 'Schemo/2.0'}]
    assert.equal(await paste(table(), 'accept: text/plain\nX-Tag: two'), true)
    await nextTick()
    assert.deepEqual(source.headers.map(row => [row.name, row.value]), [['accept', 'text/plain'], ['X-Tag', 'two']])
    assert.deepEqual(source.auto, [{name: 'User-Agent', value: 'Schemo/2.0'}])
    assert.equal(source.url, 'https://original.test')
    assert.equal(source.method, 'GET')
    assert.equal(source.body, '')
})

test('parameter paste appends query and cURL parameters without replacing the request', async context => {
    const {source, table} = mount(context, 'params')
    await paste(table(), '?page=2&tag=a&tag=b')
    globalThis.pasteRowsCommand = async () => ({reading: {kind: 'ok'}, draft: {url: 'https://pasted.test/?limit=20', method: 'POST', body: 'ignored'}})
    await paste(table(), 'curl https://pasted.test/?limit=20')
    assert.deepEqual(source.params.map(row => [row.name, row.value]), [['page', '2'], ['tag', 'a'], ['tag', 'b'], ['limit', '20']])
    assert.equal(source.url, 'https://original.test')
    assert.equal(source.method, 'GET')
    assert.equal(source.body, '')
})

test('ordinary input paste stays native and invalid table text never partially replaces rows', async context => {
    const {source, writes, table} = mount(context)
    assert.equal(await paste(table(), 'https://value.test', {closest: () => ({})}), false)
    await paste(table(), 'X-Tag: valid\ninvalid')
    assert.deepEqual(writes, [])
    assert.equal(source.headers[0].name, 'Accept')
})

test('a slow cURL paste cannot overwrite a later table edit', async context => {
    const {source, table} = mount(context)
    let resolve
    globalThis.pasteRowsCommand = () => new Promise(done => {resolve = done})
    const pending = paste(table(), 'curl https://pasted.test -H "X-Tag: stale"')
    await paste(table(), 'X-Tag: fresh')
    resolve({reading: {kind: 'ok'}, draft: {headers: [{id: '', name: 'X-Tag', value: 'stale', enabled: true}]}})
    await pending
    assert.equal(source.headers.find(row => row.name === 'X-Tag').value, 'fresh')
})
