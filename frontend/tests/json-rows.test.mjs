import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'
import { createRenderer, h, nextTick, ref, toRaw } from 'vue'
import { clientComponent, vite } from './vite.mjs'

const observers = new Set()
class ResizeObserverFake {
    constructor(callback) { this.callback = callback; this.targets = new Set(); observers.add(this) }
    observe(target) { this.targets.add(toRaw(target)); this.resize() }
    disconnect() { this.targets.clear(); observers.delete(this) }
    resize() {
        this.callback([...this.targets].map(target => ({
            target, contentRect: { width: target.offsetWidth, height: target.offsetHeight },
        })))
    }
}

globalThis.window = Object.assign(new EventTarget(), {
    ResizeObserver: ResizeObserverFake,
    getComputedStyle: () => new Proxy({}, { get: () => '0' }),
})
globalThis.document = { createElement: () => ({}) }
globalThis.ResizeObserver = ResizeObserverFake

const load = await vite({
    '/src/i18n/index.ts': 'export const t = key => key; export const textOf = key => key;',
}, {
    other(path) {
        if (!path.endsWith('.vue')) return
        if (/\/Json(Rows|Tree|Map)\.vue$/.test(path)) return clientComponent(path)
        return 'export default { render: () => null };'
    },
})
const { default: JsonRows } = await load('/src/components/json/JsonRows.vue')
const { default: JsonTree } = await load('/src/components/json/JsonTree.vue')
const { default: JsonMap } = await load('/src/components/json/JsonMap.vue')
const { buildJsonTree } = await load('/src/lib/jsonTree.ts')

function node(type, text = '') {
    return {
        type, text, props: {}, children: [], parent: null, scrollTop: 0, clientHeight: 260, offsetWidth: 800,
        get offsetHeight() {
            return this.props.class === 'row-measure' ? Number.parseFloat(this.props.style.height) * 16 : 260
        },
    }
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
    assert.equal(observers.size, 0)
})

function findAll(root, predicate) {
    return [...(predicate(root) ? [root] : []), ...root.children.flatMap(child => findAll(child, predicate))]
}

async function mount(render) {
    const root = node('root')
    app = renderer.createApp({ render })
    app.mount(root)
    await nextTick()
    await nextTick()
    return root
}

const photos = Array.from({ length: 5000 }, (_, index) => ({
    albumId: 1, id: index + 1, title: `Photo ${index + 1}`, url: `/photos/${index + 1}`, thumbnailUrl: `/thumbs/${index + 1}`,
}))

for (const [component, className] of [[JsonTree, 'tree-row'], [JsonMap, 'map-row']]) {
    test(`${className} renders a viewport of a 5000-item response, and scrolling reaches the last item`, async () => {
        const root = await mount(() => h(component, { value: photos }))
        const rendered = () => findAll(root, element => element.props.class === className)
        assert.ok(rendered().length > 0)
        assert.ok(rendered().length < 60)

        const [viewport] = findAll(root, element => element.props.class === 'json-rows')
        viewport.scrollTop = 30000 * (className === 'tree-row' ? 26 : 32) - viewport.clientHeight
        viewport.props.onScroll()
        await nextTick()
        assert.ok(rendered().length < 60)
        const text = findAll(root, element => element.text).map(element => element.text).join('')
        assert.ok(text.includes(className === 'tree-row' ? '/thumbs/5000' : '$[4999].thumbnailUrl'))
    })
}

test('a shorter response clamps a previous bottom scroll position instead of leaving an empty viewport', async () => {
    const rows = ref(buildJsonTree(photos).get('$').children)
    const root = await mount(() => h(JsonRows, { rows: rows.value, rowHeight: 1.625 }, {
        default: ({ node }) => h('span', { 'data-path': node.path }),
    }))
    const [viewport] = findAll(root, element => element.props.class === 'json-rows')
    viewport.scrollTop = 5000 * 26 - viewport.clientHeight
    viewport.props.onScroll()
    await nextTick()

    rows.value = rows.value.slice(0, 3)
    await nextTick()
    assert.equal(viewport.scrollTop, 0)
    assert.deepEqual(findAll(root, element => element.props['data-path']).map(element => element.props['data-path']),
        ['$[0]', '$[1]', '$[2]'])
})

test('row heights follow interface size changes while the viewer is open', async () => {
    const rows = buildJsonTree(photos).get('$').children
    const height = ref(1.625)
    const root = await mount(() => h(JsonRows, { rows, rowHeight: height.value }))
    height.value = 2
    await nextTick()
    for (const observer of observers) observer.resize()
    await nextTick()
    const [wrapper] = findAll(root, element => element.props.style?.marginTop !== undefined)
    assert.equal(Number.parseFloat(wrapper.props.style.height) + Number.parseFloat(wrapper.props.style.marginTop), 5000 * 32)
})
