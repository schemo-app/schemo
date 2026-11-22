import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createRenderer, h } from 'vue'
import { clientComponent, vite } from './vite.mjs'

const load = await vite({
    '/src/i18n/index.ts': 'export const t = key => key; export const formatBytes = String; export const describeFailure = String',
    '/src/composables/useIndent.ts': 'export const useIndent = () => ({indent:{value:2}})',
    '/src/composables/useRowOrder.ts': `import {computed} from 'vue'; export const useRowOrder = ({rows}) => ({press(){},states:computed(()=>rows().map(()=>({class:{},style:{}})))});`,
    '/bindings/schemo/internal/request/index.ts': 'export const Drafts = {}',
}, {other(path) {
    if (path.endsWith('/request/ExampleForm.vue')) return clientComponent(path)
    if (path.endsWith('/ui/RowCreate.vue')) return `import {h} from 'vue'; export default {props:['create'],setup(props){return ()=>h('row-create',{create:props.create})}}`
    if (path.endsWith('.vue')) return 'export default {render(){return null}}'
}})
const {default: ExampleForm} = await load('/src/components/request/ExampleForm.vue')

test('response example headers are added with their name and value, never as empty rows', async context => {
    const nodes = []
    const patches = []
    const renderer = createRenderer({
        createElement(type){const node={type,props:{},querySelector(){return null}};nodes.push(node);return node},
        createText:()=>({}),createComment:()=>({}),setText(){},setElementText(){},
        patchProp(node,key,previous,value){node.props[key]=value},
        insert(){},remove(){},parentNode:()=>null,nextSibling:()=>null,
    })
    const app = renderer.createApp({render:()=>h(ExampleForm,{
        example:{name:'Example',status:200,headers:[],body:''},pane:'headers',onPatch:patch=>patches.push(patch),
    })})
    app.mount({})
    context.after(()=>app.unmount())
    const create = nodes.find(node=>node.type === 'row-create').props.create
    assert.equal(await create('   ',''),false)
    assert.deepEqual(patches,[])
    assert.equal(await create(' Content-Type ','application/json'),true)
    assert.deepEqual(patches,[{headers:[{name:'Content-Type',value:'application/json'}]}])
})
