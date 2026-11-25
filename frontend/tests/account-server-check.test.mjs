import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createRenderer, h, nextTick } from 'vue'
import { clientComponent, vite } from './vite.mjs'

globalThis.Element = class extends EventTarget {
  closest() { return null }
}
globalThis.document = {activeElement:null}

const load = await vite({
  '/src/i18n/index.ts': 'export const t = key => key; export const formatDate = () => ""',
  '@wailsio/runtime': 'export const Browser = {OpenURL:async()=>{}}',
  '/src/composables/usePlan.ts': 'export const usePlan = () => ({loadPlan:async()=>{}})',
  '/src/composables/useAccount.ts': `
    import {ref} from 'vue';
    const state = ref({signedIn:false, waiting:false, account:{server:'https://old.example.test'}});
    export const useAccount = () => ({
      state, sessions:ref([]), failure:ref(''), busy:ref(false), signOutOpen:ref(false), deleteOpen:ref(false),
      openSignOut(){}, openDelete(){}, closeDelete(){}, openPanel(){}, loadAccount:async()=>{}, loadSessions:async()=>{},
      checkServer:async()=>{}, beginSignIn:async()=>{}, cancelSignIn:async()=>{},
      checkAddress:server=>globalThis.answerFor(server),
    });`,
}, {
  other(path) {
    if (['/settings/AccountPane.vue', '/settings/SettingsSection.vue'].some(name=>path.endsWith(name))) {
      return clientComponent(path)
    }
    if (path.endsWith('/ui/Button.vue')) return `
      import {h} from 'vue';
      export default {props:['variant','size','disabled'], emits:['click'], setup(props,{slots,emit}) {
        return () => h('button', {disabled:props.disabled, onClick:()=>emit('click')}, slots.default?.())
      }};`
    if (path.endsWith('.vue')) return 'export default {render(){return null}}'
  },
})
const { default: AccountPane } = await load('/src/components/settings/AccountPane.vue')

const settle = async () => { for (let turn = 0; turn < 8; turn++) await nextTick() }

function mount(context) {
  const node = (type, text = '') => Object.assign(new Element(), {type, text, props:{}, children:[]})
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
  const app = renderer.createApp({render:()=>h(AccountPane)})
  app.mount(root)
  context.after(()=>app.unmount())
  const all = node => [node,...node.children.flatMap(all)]
  const textOf = node => node.text + node.children.map(textOf).join('')
  return {
    check:()=>all(root).find(node=>node.type === 'button' && textOf(node).startsWith('account.out.check')),
    field:()=>all(root).find(node=>node.props.class === 'addr-field'),
    answer:()=>{
      const held = all(root).find(node=>String(node.props.class).startsWith('answer '))
      return held && {tone:held.props.class, text:textOf(held)}
    },
    textOf,
  }
}

test('checking a server address says what the server answered', async context => {
  const asked = []
  let reply
  globalThis.answerFor = server => {
    asked.push(server)
    return new Promise(resolve => { reply = resolve })
  }
  const view = mount(context)
  await settle()

  view.check().props.onClick()
  await settle()
  assert.deepEqual(asked, ['https://old.example.test'])
  assert.equal(view.textOf(view.check()), 'account.out.checking')
  assert.equal(view.answer(), undefined)

  reply('')
  await settle()
  assert.deepEqual(view.answer(), {tone:'answer ok', text:'account.out.checkOk'})
  assert.equal(view.textOf(view.check()), 'account.out.check')
})

test('a refused address is answered beside the field, and a new address clears the answer', async context => {
  globalThis.answerFor = async () => 'The server could not be reached.'
  const view = mount(context)
  await settle()

  view.check().props.onClick()
  await settle()
  assert.deepEqual(view.answer(), {tone:'answer bad', text:'The server could not be reached.'})

  const field = view.field()
  field.value = 'https://new.example.test'
  field.dispatchEvent(new Event('input'))
  await settle()
  assert.equal(view.answer(), undefined)
})
