import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createRenderer, h, nextTick, reactive, toRaw } from 'vue'
import { clientComponent, vite } from './vite.mjs'

globalThis.window = new EventTarget()
globalThis.Document = class Document {activeElement = null}
globalThis.document = new Document()
globalThis.Element = class Element extends EventTarget {
    contains(target) {
        for (let current = target; current; current = current.parent) if (current === this) return true
        return false
    }
}
globalThis.switcherPorts = {
    reka: await import('reka-ui'),
    tabs: reactive({activeId: 'one', activated: [], activate(id) {this.activeId = id; this.activated.push(id)}}),
}
const ports = globalThis.switcherPorts
const load = await vite({
    'reka-ui': `import {h} from 'vue';
        const library = globalThis.switcherPorts.reka;
        export const PopoverRoot = library.PopoverRoot;
        export const PopoverTrigger = {inheritAttrs: false, setup(_, {attrs, slots}) {
            globalThis.switcherPorts.context = library.injectPopoverRootContext();
            return () => h(library.PopoverTrigger, attrs, slots);
        }};`,
    '/src/stores/tabs.ts': 'export const useTabsStore = () => globalThis.switcherPorts.tabs',
    '/src/composables/useWailsEvent.ts': 'export const useWailsEvent = () => {}',
    '/src/i18n/index.ts': 'export const t = key => key',
}, {
    other(path) {
        if (path.endsWith('/layout/AppTabSwitcher.vue')) return clientComponent(path)
        if (path.endsWith('/ui/PopoverContent.vue')) return `import {h} from 'vue';
            export default {inheritAttrs: false, setup(_, {attrs, slots}) {
                const context = globalThis.switcherPorts.reka.injectPopoverRootContext();
                return () => context.open.value ? h('div', attrs, slots.default?.()) : null;
            }};`
        if (path.endsWith('.vue')) return 'export default {render() {return null}}'
    },
})
const { default: AppTabSwitcher } = await load('/src/components/layout/AppTabSwitcher.vue')

async function settle() {
    for (let index = 0; index < 8; index++) await Promise.resolve()
    await nextTick()
}

test('overflow that appears after mounting registers its button and toggles like a rail popover', async context => {
    const nodes = []
    const activated = ports.tabs.activated
    activated.length = 0
    function node(type) {
        const element = Object.assign(new Element(), {type, tagName: type.toUpperCase(), props: {}, children: [], parent: null,
            focus() {}, querySelector() {return null}, scrollIntoView() {},
            getRootNode() {return globalThis.document},
        })
        nodes.push(element)
        return element
    }
    function remove(element) {
        if (element.parent) element.parent.children.splice(element.parent.children.indexOf(element), 1)
        element.parent = null
    }
    const renderer = createRenderer({
        createElement: node, createText: () => node('text'), createComment: () => node('comment'),
        insert(element, parent, anchor) {
            remove(element)
            element.parent = parent
            const index = anchor ? parent.children.indexOf(anchor) : -1
            parent.children.splice(index < 0 ? parent.children.length : index, 0, element)
        },
        remove, parentNode: element => element.parent,
        nextSibling: element => element.parent?.children[element.parent.children.indexOf(element) + 1] ?? null,
        setText(element, text) {element.text = text}, setElementText(element, text) {element.text = text},
        patchProp(element, key, previous, value) {element.props[key] = value},
    })
    const props = reactive({
        hiddenCount: 0,
        entries: ['one', 'two'].map(id => ({id, title: id, method: 'GET', preview: false, dirty: false, pinned: false, sub: ''})),
    })
    const app = renderer.createApp({render: () => h(AppTabSwitcher, props)})
    const root = node('root')
    app.mount(root)
    context.after(() => app.unmount())
    const find = className => nodes.find(element => element.parent && element.props.class?.split(' ').includes(className))
    assert.equal(find('tab-overflow'), undefined)
    props.hiddenCount = 1
    await settle()
    const button = find('tab-overflow')
    assert.equal(toRaw(ports.context.triggerElement.value), button)
    for (let index = 0; index < 2; index++) {
        button.props.onClick()
        await settle()
        assert.equal(ports.context.open.value, true)
        assert.equal(button.props['aria-expanded'], true)
        button.props.onClick()
        await settle()
        assert.equal(ports.context.open.value, false)
        assert.equal(button.props['aria-expanded'], false)
    }
    props.hiddenCount = 0
    await settle()
    assert.equal(find('tab-overflow'), undefined)
    props.hiddenCount = 1
    await settle()
    const restored = find('tab-overflow')
    assert.notEqual(restored, button)
    assert.equal(toRaw(ports.context.triggerElement.value), restored)
    restored.props.onClick()
    await settle()
    find('switcher-row').props.onClick()
    await settle()
    assert.equal(ports.context.open.value, false)
    assert.deepEqual(activated, ['one'])
    props.hiddenCount = 0
    await settle()
    window.dispatchEvent(Object.assign(new Event('keydown', {cancelable: true}), {
        key: 'Tab', ctrlKey: true, shiftKey: false, altKey: false, metaKey: false,
    }))
    await settle()
    assert.equal(ports.context.open.value, true)
    assert.equal(toRaw(ports.context.triggerElement.value), find('tab-overflow'))
    window.dispatchEvent(Object.assign(new Event('keyup'), {key: 'Control'}))
    await settle()
    assert.equal(ports.context.open.value, false)
    assert.deepEqual(activated, ['one', 'two'])
})
