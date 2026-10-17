import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { after, test } from 'node:test'
import { createRenderer, h, nextTick, reactive } from 'vue'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import { createServer } from 'vite'

const messages = JSON.parse(readFileSync(new URL('../src/locales/en.json', import.meta.url), 'utf8'))
globalThis.passwordMessages = key => key.split('.').reduce((value, part) => value[part], messages)
globalThis.Document = class { activeElement = null }
globalThis.ShadowRoot = class {}
globalThis.document = new Document()
const vite = await createServer({ configFile: false,
  optimizeDeps: { noDiscovery: true, include: [] },
  server: { middlewareMode: true, watch: null, hmr: false, ws: false }, appType: 'custom',
  plugins: [{ name: 'password-component', enforce: 'pre', load(id) {
    const path = id.replaceAll('\\', '/')
    if (path.endsWith('/src/i18n.ts')) return 'export const useMessages=()=>({t:globalThis.passwordMessages});'
    if (path.endsWith('/ui/Icon.vue')) return 'export default {render(){return null}}'
    if (path.endsWith('/lib/clipboard.ts')) return 'export const copyToClipboard = async value => { globalThis.copiedPassword = value; return true }'
    if (path.endsWith('/composables/useToast.ts')) return 'export const useToast = () => ({show() {}})'
    if (path.endsWith('/publications/PublicationPassword.vue')) {
      const { descriptor } = parse(readFileSync(path, 'utf8'), { filename: path })
      const compiled = compileScript(descriptor, { id: path, inlineTemplate: true })
      return ts.transpileModule(compiled.content, { compilerOptions: {
        module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022,
      } }).outputText
    }
  } }],
})
after(() => vite.close())
const { default: Password } = await vite.ssrLoadModule('/src/components/publications/PublicationPassword.vue')

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
