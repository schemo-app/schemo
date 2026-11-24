import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createRenderer, h, nextTick, reactive } from 'vue'
import { clientComponent, vite } from './vite.mjs'

const box = `import {h} from 'vue'; const Box = {setup(_, {slots}) {return () => h('div', slots.default?.())}};`
const load = await vite({
  'reka-ui': box + `export const DropdownMenuRoot = Box, DropdownMenuTrigger = Box;
    export const DropdownMenuCheckboxItem = {
      props: ['modelValue', 'disabled'],
      setup(props, {emit, slots}) {return () => h('button', {
        'data-checkbox': true, 'aria-checked': props.modelValue, disabled: props.disabled,
        onClick() {const event = {preventDefault() {globalThis.publicationSelectionPrevented = true}}; emit('select', event)},
      }, slots.default?.())},
    };`,
  '/src/i18n/index.ts': 'export const t = key => key',
  '/ui/Icon.vue': 'export default {render(){return null}}',
}, {
  other: path => path.endsWith('/PublicationFields.vue') ? clientComponent(path)
    : /\/ui\/DropdownMenu(Content|Item)\.vue$/.test(path) ? box + 'export default Box' : undefined,
})
const {default: Fields} = await load('/src/components/cloud/PublicationFields.vue')

test('publication environments are independently selectable without closing the menu', async () => {
  const nodes = []
  function node(tag, text = '') {
    const element = {tag, text, props: {}, children: [], parent: null}
    nodes.push(element)
    return element
  }
  function remove(child) {
    if (child.parent) child.parent.children.splice(child.parent.children.indexOf(child), 1)
    child.parent = null
  }
  const renderer = createRenderer({
    createElement: node, createText: text => node('text', text), createComment: () => node('comment'),
    setText(element, text) {element.text = text},
    setElementText(element, text) {element.text = text; element.children = []},
    parentNode: element => element.parent,
    nextSibling: element => element.parent?.children[element.parent.children.indexOf(element) + 1] ?? null,
    patchProp(element, key, old, value) {element.props[key] = value},
    remove,
    insert(child, parent, anchor = null) {
      remove(child)
      child.parent = parent
      const index = anchor ? parent.children.indexOf(anchor) : -1
      parent.children.splice(index < 0 ? parent.children.length : index, 0, child)
    },
  })
  const form = reactive({ids: ['staging']})
  const environments = [{id: 'local', name: 'Local'}, {id: 'staging', name: 'Staging'}, {id: 'production', name: 'Production'}]
  const app = renderer.createApp({render: () => h(Fields, {
    visibility: 'unlisted', environmentIds: form.ids, environments,
    onEnvironment: ids => {form.ids = ids},
  })})
  app.mount(node('root'))
  try {
    const choices = nodes.filter(node => node.props['data-checkbox'])
    assert.equal(choices.length, 3)
    choices[2].props.onClick()
    await nextTick()
    assert.deepEqual(form.ids, ['staging', 'production'])
    assert.equal(choices[1].props['aria-checked'], true)
    assert.equal(choices[2].props['aria-checked'], true)
    assert.equal(choices[0].props['aria-checked'], false)
    assert.equal(globalThis.publicationSelectionPrevented, true)
    assert.ok(nodes.some(node => node.text === 'Staging, Production'))
    choices[1].props.onClick()
    await nextTick()
    assert.deepEqual(form.ids, ['production'])
    assert.equal(choices[2].props.disabled, true)
    choices[2].props.onClick()
    assert.deepEqual(form.ids, ['production'])
  } finally {
    app.unmount()
  }
})
