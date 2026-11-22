import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createRenderer, h, nextTick, reactive } from 'vue'
import { clientComponent, vite } from './vite.mjs'

const load = await vite({
    '/src/i18n/index.ts': 'export const t = key => key; export const describeFailure = String',
    '/src/stores/environments.ts': 'export const useEnvironmentsStore = () => globalThis.environmentTableStore',
}, {
    other(path) {
        if (path.endsWith('/environments/VariablesTable.vue')) return clientComponent(path)
        if (path.endsWith('/request/BulkRowsEditor.vue')) return `
            import {h} from 'vue';
            export default {props: ['rows', 'readonly'], emits: ['apply'], setup(props, {emit}) {
                return () => h('textarea', {readonly: props.readonly, onInput: () => emit('apply', globalThis.environmentTextRows)})
            }};`
        if (path.endsWith('.vue')) return 'export default {render() {return null}}'
    },
})
const { default: VariablesTable } = await load('/src/components/environments/VariablesTable.vue')

function mount(context) {
    const saved = []
    globalThis.environmentTableStore = {
        sheetFocus: null,
        varsOf: () => [],
        async saveScope(id, vars) { saved.push({id, vars}) },
    }
    const nodes = []
    const renderer = createRenderer({
        createElement(type) {
            const node = {type, props: {}}
            nodes.push(node)
            return node
        },
        createText: () => ({}), createComment: () => ({}),
        setText() {}, setElementText() {},
        patchProp(node, key, previous, value) { node.props[key] = value },
        insert() {}, remove() {}, parentNode: () => null, nextSibling: () => null,
    })
    const props = reactive({envId: 'local', locked: false, textMode: true})
    const app = renderer.createApp({render: () => h(VariablesTable, props)})
    app.mount({})
    context.after(() => app.unmount())
    return {props, saved, app, input: () => nodes.at(-1)}
}

test('switching environments flushes text into the environment where it was entered', async context => {
    const {props, saved, input} = mount(context)
    globalThis.environmentTextRows = [{id: 'host', name: 'host', value: 'local.test', enabled: true}]
    input().props.onInput()
    props.envId = 'production'
    await nextTick()
    assert.equal(saved.length, 1)
    assert.equal(saved[0].id, 'local')
    assert.equal(saved[0].vars[0].value, 'local.test')
})

test('closing the table flushes pending text once and readonly mode reaches the editor', async context => {
    const {props, saved, app, input} = mount(context)
    props.locked = true
    await nextTick()
    assert.equal(input().props.readonly, true)
    props.locked = false
    await nextTick()
    globalThis.environmentTextRows = [{id: 'host', name: 'host', value: 'local.test', enabled: true}]
    input().props.onInput()
    app.unmount()
    assert.equal(saved.length, 1)
    assert.equal(saved[0].id, 'local')
})
