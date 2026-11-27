import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createRenderer, h, nextTick, ref } from 'vue'
import { clientComponent, vite } from './vite.mjs'

globalThis.Element = class extends EventTarget {
  closest() { return null }
}
globalThis.document = {activeElement:null}

const load = await vite({
  '/src/i18n/index.ts': 'export const t = key => key',
  '/bindings/schemo/internal/domain/index.ts': `
    export const ProxyMode = {ProxyManual:'manual', ProxySystem:'system', ProxyOff:'off'};
    export const ProxyProtocol = {ProxyHTTP:'http', ProxyHTTPS:'https', ProxySOCKS5:'socks5'};`,
  '/bindings/schemo/internal/workspace/index.ts':
    'export const Settings = {ProxyEnvironment:async()=>[], TestProxy:()=>globalThis.testProxy()}',
  '/src/composables/useSettings.ts': `
    export const useSetting = () => globalThis.proxySettings.proxy;
    export const useSettings = () => globalThis.proxySettings;`,
  '/src/composables/useSheetNotice.ts': 'export const useSheetNotice = () => ({withNotice:action=>action()})',
}, {
  other(path) {
    if (['/settings/ProxyPane.vue', '/ui/Switch.vue', '/settings/SettingsSection.vue'].some(name=>path.endsWith(name))) {
      return clientComponent(path)
    }
    if (path.endsWith('/ui/Input.vue')) return `
      import {h} from 'vue';
      export default {props:['modelValue'], emits:['update:modelValue'], setup(props,{attrs,emit}) {
        return () => h('input', {...attrs,value:props.modelValue,onInput:event=>emit('update:modelValue',event.target.value)})
      }};`
    if (path.endsWith('/ui/Button.vue')) return `
      import {h} from 'vue';
      export default {setup(props,{slots}) { return () => h('button', slots.default?.()) }};`
    if (path.endsWith('.vue')) return 'export default {render(){return null}}'
  },
})
const { default: ProxyPane } = await load('/src/components/settings/ProxyPane.vue')

const settle = async () => { for (let turn = 0; turn < 8; turn++) await nextTick() }

function mount(context, credentials = {}) {
  const proxy = ref({mode:'manual', protocol:'http', host:'proxy.example.test', port:3128, user:'', hasPassword:false, ...credentials})
  const writes = []
  globalThis.proxySettings = {
    proxy,
    async loadSettings() {},
    async save(patch) {
      writes.push(patch)
      const {password, ...fields} = patch.proxy
      proxy.value = {...proxy.value, ...fields, ...(password === undefined ? {} : {hasPassword:password !== ''})}
    },
  }
  const node = (type, text = '') => Object.assign(new Element(), {type, tagName:type.toUpperCase(), text, props:{}, children:[]})
  const root = node('root')
  const renderer = createRenderer({
    createElement:node, createText:text=>node('text',text), createComment:()=>node('comment'),
    setText(child,text){child.text=text}, setElementText(child,text){child.text=text},
    patchProp(child,key,previous,value){child.props[key]=value},
    insert(child,parent,anchor){
      if (child.parent) child.parent.children.splice(child.parent.children.indexOf(child),1)
      const at = anchor ? parent.children.indexOf(anchor) : parent.children.length
      parent.children.splice(at,0,child)
      child.parent=parent
    },
    remove(child){child.parent?.children.splice(child.parent.children.indexOf(child),1)},
    parentNode:child=>child.parent,
    nextSibling:child=>child.parent?.children[child.parent.children.indexOf(child)+1],
  })
  const app = renderer.createApp({render:()=>h(ProxyPane)})
  app.mount(root)
  context.after(()=>app.unmount())
  const all = node => [node,...node.children.flatMap(all)]
  const says = (node, text) => all(node).some(child=>child.text === text)
  return {
    proxy, writes,
    toggle:()=>all(root).find(node=>node.props.role === 'switch'),
    credentials:()=>all(root).find(node=>node.props.class === 'pair'),
    runTest:()=>all(root).find(node=>node.type === 'button' && says(node, 'settings.proxy.probeRun')).props.onClick(),
    pickProtocol:value=>all(root).find(node=>node.type === 'select').props.onChange({target:{value}}),
    answer:()=>all(root).find(node=>node.props.class === 'answer-text')?.text,
  }
}

test('proxy authentication can be enabled to show credentials and disabled again', async context => {
  const view = mount(context)
  await settle()
  assert.equal(view.toggle().props['aria-checked'], false)
  assert.equal(view.credentials(), undefined)
  view.toggle().props.onClick()
  await settle()
  assert.equal(view.toggle().props['aria-checked'], true)
  assert.ok(view.credentials())
  assert.deepEqual(view.writes, [])
  view.toggle().props.onClick()
  await settle()
  assert.equal(view.toggle().props['aria-checked'], false)
  assert.equal(view.credentials(), undefined)
  assert.deepEqual(view.writes, [{proxy:{user:'',password:''}}])
})

test('the answer of a proxy test goes when the settings it was asked with change', async context => {
  globalThis.testProxy = async () => ({ok:false, needsTls:true})
  const view = mount(context)
  await settle()
  view.runTest()
  await settle()
  assert.equal(view.answer(), 'settings.proxy.probeTls')
  view.pickProtocol('https')
  await settle()
  assert.equal(view.answer(), undefined)
})

test('an answer that arrives after the settings changed is not shown', async context => {
  let arrive
  globalThis.testProxy = () => new Promise(resolve => { arrive = resolve })
  const view = mount(context)
  await settle()
  view.runTest()
  view.pickProtocol('https')
  await settle()
  arrive({ok:false, needsTls:true})
  await settle()
  assert.equal(view.answer(), undefined)
})

test('disabling saved proxy authentication clears the username and password', async context => {
  const view = mount(context, {user:'proxy-user', hasPassword:true})
  await settle()
  assert.equal(view.toggle().props['aria-checked'], true)
  assert.ok(view.credentials())
  view.toggle().props.onClick()
  await settle()
  assert.equal(view.proxy.value.user, '')
  assert.equal(view.proxy.value.hasPassword, false)
  assert.equal(view.toggle().props['aria-checked'], false)
  assert.equal(view.credentials(), undefined)
  assert.deepEqual(view.writes, [{proxy:{user:'',password:''}}])
})
