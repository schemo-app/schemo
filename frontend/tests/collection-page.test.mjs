import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createRenderer, h, nextTick, reactive } from 'vue'
import { CancellablePromise } from '@wailsio/runtime/cancellable'
import { clientComponent, vite } from './vite.mjs'

const load = await vite({
  '/src/stores/collections.ts': 'export const useCollectionsStore = () => globalThis.collectionPage.collections',
  '/src/stores/publications.ts': 'export const usePublicationsStore = () => ({markOf:()=>null,mark(){}})',
  '/src/stores/tabs.ts': 'export const useTabsStore = () => ({})',
  '/src/stores/transfer.ts': 'export const useTransferStore = () => ({})',
  '/src/stores/workspaces.ts': 'export const useWorkspacesStore = () => ({canWrite:false})',
  '/src/i18n/index.ts': 'export const t = key => key; export const describeFailure = String',
  '/src/composables/useToast.ts': 'export const useToast = () => ({show:message=>globalThis.collectionPage.errors.push(message)})',
  '/src/lib/protocol.ts': 'export const protocolOf = () => ({})',
  '/bindings/schemo/internal/workspace/index.ts': 'export const Collections = {Contents:id=>globalThis.collectionPage.contents(id)}',
  '/bindings/schemo/internal/script/index.ts': 'export const Scripts = {Scripts:id=>globalThis.collectionPage.scripts(id)}',
  '/bindings/schemo/internal/request/index.ts': 'export const Runs = {}',
  'reka-ui': `import {h} from 'vue';
    const box = {setup(_, {slots}) {return () => h('box',null,slots.default?.())}};
    export const DropdownMenuRoot = box, DropdownMenuTrigger = box, TabsRoot = box;`,
}, {
  other(path) {
    if (path.endsWith('/collections/CollectionPage.vue')) return clientComponent(path)
    if (path.endsWith('/collections/CollectionOverview.vue')) return `
      import {h} from 'vue'; export default {props:['rows','loading'],setup:props=>()=>h('overview',{rows:props.rows,loading:props.loading})};`
    if (path.endsWith('/collections/CollectionScripts.vue')) return `
      import {h} from 'vue'; export default {props:['scripts'],setup:props=>()=>h('scripts',{scripts:props.scripts})};`
    if (path.endsWith('.vue')) return 'export default {render(){return null}}'
  },
})
const {default: CollectionPage} = await load('/src/components/collections/CollectionPage.vue')
const settle = async () => { for (let turn = 0; turn < 20; turn++) await nextTick() }
const row = (id, name = id) => ({id,name,method:'GET',url:`https://example.test/${id}`})

function mount(context) {
  const collections = reactive({collectionId:'catalog', tree:[{id:'catalog',name:'Catalog',items:[],children:[]}],pendingSection:null})
  const state = {
    collections, errors:[], rows:[], script:null,
    contents:() => CancellablePromise.resolve(state.rows),
    scripts:() => CancellablePromise.resolve(state.script),
  }
  globalThis.collectionPage = state
  const root = {children:[]}
  const renderer = createRenderer({
    createElement:type=>({type,props:{},children:[]}),
    createText:text=>({text,children:[]}), createComment:()=>({children:[]}),
    setText(node,text){node.text=text}, setElementText(node,text){node.text=text},
    patchProp(node,key,previous,value){node.props[key]=value},
    insert(node,parent,anchor){
      if (node.parent) node.parent.children.splice(node.parent.children.indexOf(node),1)
      const at = anchor ? parent.children.indexOf(anchor) : parent.children.length
      parent.children.splice(at,0,node)
      node.parent=parent
    },
    remove(node){node.parent?.children.splice(node.parent.children.indexOf(node),1)},
    parentNode:node=>node.parent,
    nextSibling:node=>node.parent?.children[node.parent.children.indexOf(node)+1],
  })
  const app = renderer.createApp({render:()=>h(CollectionPage)})
  app.mount(root)
  context.after(()=>app.unmount())
  const nodes = (type,node=root) => [...(node.type === type ? [node] : []),...node.children.flatMap(child=>nodes(type,child))]
  return {
    state, collections,
    rows:()=>nodes('overview')[0].props.rows.map(item=>item.row),
    scripts:()=>nodes('scripts')[0].props.scripts,
    refresh(rows) {
      state.rows=rows
      collections.tree=[{...collections.tree[0],items:rows}]
    },
  }
}

test('an open collection follows synced additions, edits, reordering and removals without changing its ID', async context => {
  const view = mount(context)
  await settle()
  assert.deepEqual(view.rows(),[])
  for (const rows of [[row('first')], [row('first'),row('second')], [row('second'),row('first','Renamed')], [row('first','Renamed')], []]) {
    view.refresh(rows)
    await settle()
    assert.deepEqual(view.rows(),rows)
    assert.equal(view.collections.collectionId,'catalog')
  }
  assert.deepEqual(view.state.errors,[])
})

test('an older contents response cannot replace a newer synced snapshot', async context => {
  const view = mount(context)
  await settle()
  const older = CancellablePromise.withResolvers()
  view.state.contents=()=>older.promise
  view.refresh([row('older')])
  await settle()
  view.state.contents=()=>CancellablePromise.resolve([row('newer')])
  view.refresh([row('newer')])
  await settle()
  assert.deepEqual(view.rows(),[row('newer')])
  older.resolve([row('older')])
  await settle()
  assert.deepEqual(view.rows(),[row('newer')])
  assert.deepEqual(view.state.errors,[])
})

test('moving a nested folder refreshes both open collections', async context => {
  const view = mount(context)
  await settle()
  const nested = {id:'folder',name:'Folder',items:[row('nested')],children:[]}
  view.state.rows=[row('nested')]
  view.collections.tree=[{id:'catalog',name:'Catalog',items:[],children:[nested]}, {id:'billing',name:'Billing',items:[],children:[]}]
  await settle()
  assert.deepEqual(view.rows(),[row('nested')])
  view.state.rows=[]
  view.collections.tree=[{id:'catalog',name:'Catalog',items:[],children:[]}, {id:'billing',name:'Billing',items:[],children:[nested]}]
  await settle()
  assert.deepEqual(view.rows(),[])
  view.state.rows=[row('nested')]
  view.collections.collectionId='billing'
  await settle()
  assert.deepEqual(view.rows(),[row('nested')])
  assert.deepEqual(view.state.errors,[])
})

test('synced collection scripts refresh while the collection stays open', async context => {
  const view = mount(context)
  view.collections.pendingSection='scripts'
  await settle()
  assert.equal(view.scripts(),null)
  view.state.script={pre:'pm.variables.set("ready", true)',post:'',preOff:false,postOff:false}
  view.refresh([])
  await settle()
  assert.deepEqual(view.scripts(),view.state.script)
  assert.deepEqual(view.state.errors,[])
})
