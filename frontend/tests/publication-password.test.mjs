import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { createRenderer, h, nextTick, reactive } from 'vue'
import { clientComponent, vite } from './vite.mjs'

const messages = JSON.parse(readFileSync(new URL('../src/i18n/locales/en.json', import.meta.url), 'utf8'))
globalThis.passwordMessages = key => key.split('.').reduce((value, part) => value[part], messages)
globalThis.Document = class { activeElement = null }
globalThis.ShadowRoot = class {}
globalThis.document = new Document()
const load = await vite({
  '/src/i18n/index.ts': 'export const t = (...args) => globalThis.passwordMessages(...args)',
  '/ui/Icon.vue': 'export default {render(){return null}}',
  '/lib/clipboard.ts': 'export const copyToClipboard = async value => { globalThis.copiedPassword = value; return true }',
  '/composables/useToast.ts': 'export const useToast = () => ({show() {}})',
}, {
  other: (path) => path.endsWith('/cloud/PublicationPassword.vue') ? clientComponent(path) : undefined,
})
const { default: Password } = await load('/src/components/cloud/PublicationPassword.vue')

test('generated passwords update the form and can be hidden', async () => {
  const nodes = []
  const renderer = createRenderer({
    createElement: tag => { const node = { tag, tagName: tag.toUpperCase(), props: {}, addEventListener() {}, getRootNode: () => document }; nodes.push(node); return node },
    createText: () => ({}), createComment: () => ({}),
    insert() {}, remove() {}, setText() {}, setElementText() {},
    parentNode: () => null, nextSibling: () => null,
    patchProp(node, key, old, value) { node.props[key] = value },
  })
  const form = reactive({ password: '' })
  const app = renderer.createApp({ render: () => h(Password, {
    modelValue: form.password, 'onUpdate:modelValue': value => { form.password = value },
    placeholder: '', tools: true,
  }) })
  app.mount({})
  const generate = nodes.find(node => node.props['aria-label'] === messages.publications.generatePassword)
  generate.props.onClick()
  await nextTick()
  assert.match(form.password, /^[0-9a-f]{32}$/)
  const input = nodes.find(node => node.tag === 'input')
  assert.equal(input.props.type, 'text')
  const toggle = nodes.find(node => node.props['aria-label'] === messages.publications.hidePassword)
  toggle.props.onClick()
  await nextTick()
  assert.equal(input.props.type, 'password')
  const copy = nodes.find(node => node.props['aria-label'] === messages.publications.copyPassword)
  await copy.props.onClick()
  assert.equal(globalThis.copiedPassword, form.password)
  const first = form.password
  generate.props.onClick()
  await nextTick()
  assert.notEqual(form.password, first)
  app.unmount()
})
