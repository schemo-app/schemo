import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createRenderer, h, nextTick, reactive } from 'vue'
import { clientComponent, vite } from './vite.mjs'

const load = await vite({
    '/src/i18n/index.ts': 'export const t = key => key; export const describeFailure = String',
    '/src/stores/collections.ts': 'export const useCollectionsStore = () => globalThis.collectionVariables.collections',
    '/src/stores/environments.ts': 'export const useEnvironmentsStore = () => globalThis.collectionVariables.environments',
    '/src/stores/workspaces.ts': 'export const useWorkspacesStore = () => ({canWrite:true})',
    '/src/composables/useToast.ts': 'export const useToast = () => ({show(){}})',
    'reka-ui': `import {h} from 'vue'; const wrapper = {setup(_, {slots}) {return () => h('wrapper', null, slots.default?.())}}; export const DropdownMenuRoot = wrapper; export const DropdownMenuTrigger = wrapper;`,
}, {
    other(path) {
        if (path.endsWith('/collections/CollectionVariables.vue')) return clientComponent(path)
        if (path.endsWith('/environments/VariableRow.vue')) return `
            import {h} from 'vue';
            export default {props:['variable','scope','editor','locked'], emits:['patch','remove'], setup(props,{emit,slots}) {
                return () => h('variable-row', {...props, onPatch:patch=>emit('patch',patch), onRemove:()=>emit('remove')}, slots.default?.())
            }};`
        if (path.endsWith('/ui/RowCreate.vue')) return `
            import {h} from 'vue';
            export default {props:['create'], setup(props) {return () => h('variable-create', {create:props.create})}};`
        if (path.endsWith('/environments/EnvironmentMenu.vue')) return `
            import {h} from 'vue';
            export default {props:['selected'], emits:['select'], setup(props,{emit}) {
                return () => h('environment-menu', {...props, onSelect:id=>emit('select',id)})
            }};`
        if (path.endsWith('/ui/Modal.vue')) return `
            import {h} from 'vue';
            export default {props:['title','open','variant'], emits:['close'], setup(props,{emit,slots}) {
                return () => h('modal', {...props, onClose:()=>emit('close')}, [slots.default?.(),slots.foot?.()])
            }};`
        if (path.endsWith('/ui/DialogButton.vue')) return `
            import {h} from 'vue';
            export default {props:{danger:Boolean}, setup(props,{attrs,slots}) {
                return () => h('dialog-button', {...attrs, danger:props.danger}, slots.default?.())
            }};`
        if (path.endsWith('.vue')) return 'export default {render(){return null}}'
    },
})
const { default: CollectionVariables } = await load('/src/components/collections/CollectionVariables.vue')

const variable = (id, name, value = '') => ({id, name, value, kind:'text', enabled:true})
const copy = variables => variables.map(variable => ({...variable}))
const settle = async () => { for (let turn = 0; turn < 12; turn++) await nextTick() }

function mount(context) {
    const writes = []
    const collection = {id:'catalog', variables:[variable('base', 'host', 'default.test')], children:[], items:[]}
    const collections = reactive({collectionId:'catalog', varsFilter:'', tree:[collection], async load(){}})
    const environment = reactive({id:'local', name:'Local', collections:{catalog:[variable('block', 'token', 'local-token')]}})
    let created = 0
    let beforeSave = async () => {}
    const environments = {
        environments:[environment], envOf:id=>id === 'local' ? environment : null,
        blankVar:name=>variable(`new-${++created}`, name), effectiveValue:variable=>variable.value,
        isRevealed:()=>false,
        async moveVariable(collection, id, from, into) {
            const source = from ? environment.collections[collection] : collections.tree[0].variables
            const moved = source.find(variable => variable.id === id)
            writes.push({move:{...moved}, into})
            const target = into ? environment.collections[collection] : collections.tree[0].variables
            const replaced = target.some(variable=>variable.name === moved.name)
            const kept = source.filter(variable=>variable.id !== id)
            const saved = [...target.filter(variable=>variable.name !== moved.name), {...moved,id:'moved'}]
            if (from) environment.collections[collection] = kept
            else collections.tree[0].variables = kept
            if (into) environment.collections[collection] = saved
            else collections.tree[0].variables = saved
            return replaced
        },
    }
    collections.saveVariables = async (id, variables) => {
        writes.push({id, variables:copy(variables)})
        await beforeSave()
        collections.tree.find(collection => collection.id === id).variables = copy(variables)
    }
    globalThis.collectionVariables = {collections, environments}
    const root = {children:[]}
    const renderer = createRenderer({
        createElement:type=>({type, props:{}, children:[]}),
        createText:text=>({text, children:[]}), createComment:()=>({children:[]}),
        setText(node,text){node.text=text}, setElementText(node,text){node.text=text},
        patchProp(node,key,previous,value){node.props[key]=value},
        insert(node,parent,anchor){
            if(node.parent) node.parent.children.splice(node.parent.children.indexOf(node),1)
            const index = anchor ? parent.children.indexOf(anchor) : parent.children.length
            parent.children.splice(index,0,node)
            node.parent=parent
        },
        remove(node){node.parent?.children.splice(node.parent.children.indexOf(node),1)},
        parentNode:node=>node.parent, nextSibling:node=>node.parent?.children[node.parent.children.indexOf(node)+1],
    })
    const app = renderer.createApp({render:()=>h(CollectionVariables)})
    app.mount(root)
    context.after(()=>app.unmount())
    function nodes(type, node=root) {
        return [...(node.type === type ? [node] : []), ...node.children.flatMap(child=>nodes(type,child))]
    }
    return {
        writes, collections, environment, nodes, app,
        rows:()=>nodes('variable-row'),
        holdSave() {
            let release
            const gate = new Promise(resolve=>{release=resolve})
            beforeSave=()=>gate
            return release
        },
        async add() {
            await nodes('variable-create')[0].props.create('newVariable','')
            await settle()
            const added = nodes('variable-row').at(-1).props
            await added.editor.start(added.variable,'name','')
            return added
        },
    }
}

test('new collection rows stay after existing environment rows across saves and reloads', async context => {
    const view = mount(context)
    assert.deepEqual(view.rows().map(row=>row.props.variable.id), ['base','block'])
    const added = await view.add()
    assert.deepEqual(view.rows().map(row=>row.props.variable.id), ['base','block','new-1'])
    added.editor.draft.value='apiUrl'
    added.editor.commitFrom(added.variable,'name')
    await settle()
    assert.deepEqual(view.rows().map(row=>row.props.variable.id), ['base','block','new-1'])
    view.collections.tree = [{...view.collections.tree[0], variables:copy(view.collections.tree[0].variables)}]
    await settle()
    assert.deepEqual(view.rows().map(row=>row.props.variable.id), ['base','block','new-1'])
})

test('empty names do not create collection variables', async context => {
    const view = mount(context)
    assert.equal(await view.nodes('variable-create')[0].props.create('   ',''),false)
    assert.deepEqual(view.writes,[])
    assert.equal(view.rows().length,2)
})

test('a repeated name and a fast value edit both reach the newly selected environment', async context => {
    const view = mount(context)
    const added = await view.add()
    const release = view.holdSave()
    added.editor.draft.value='host'
    added.editor.onCellKeydown({key:'Enter',preventDefault(){}}, added.variable,'name','')
    assert.equal(added.editor.isEditingCell(added.variable,'value',''), true)
    added.editor.draft.value='local.test'
    added.editor.commitFrom(added.variable,'value')
    await settle()
    const row = view.rows().at(-1)
    view.nodes('environment-menu',row)[0].props.onSelect('local')
    await settle()
    assert.equal(view.writes.length,2, 'the value and move wait for the name save')
    release()
    await settle()
    assert.deepEqual(view.writes.at(-1), {move:{...variable('new-1','host','local.test')},into:'local'})
    assert.deepEqual(view.collections.tree[0].variables, [variable('base','host','default.test')])
    assert.equal(view.environment.collections.catalog.at(-1).name,'host')
    assert.equal(view.environment.collections.catalog.at(-1).value,'local.test')
})

test('queued edits finish in their original collection after navigation', async context => {
    const view = mount(context)
    const added = await view.add()
    const release = view.holdSave()
    added.editor.draft.value='apiUrl'
    added.editor.onCellKeydown({key:'Enter',preventDefault(){}}, added.variable,'name','')
    added.editor.draft.value='local.test'
    added.editor.commitFrom(added.variable,'value')
    await settle()
    view.collections.tree.push({id:'other', variables:[], children:[], items:[]})
    view.collections.collectionId='other'
    release()
    await settle()
    assert.equal(view.writes.length,3)
    assert.equal(view.collections.tree[0].variables.at(-1).value,'local.test')
    assert.deepEqual(view.collections.tree[1].variables,[])
})

test('a duplicate destination asks before replacing and cancellation preserves both values', async context => {
    const view = mount(context)
    const added = await view.add()
    const release = view.holdSave()
    added.editor.draft.value='token'
    added.editor.onCellKeydown({key:'Enter',preventDefault(){}}, added.variable,'name','')
    added.editor.draft.value='new-token'
    added.editor.commitFrom(added.variable,'value')
    const row = view.rows().at(-1)
    view.nodes('environment-menu',row)[0].props.onSelect('local')
    release()
    await settle()
    assert.equal(view.nodes('modal').length,1)
    assert.equal(view.writes.some(write=>write.move),false)
    assert.equal(view.environment.collections.catalog[0].value,'local-token')
    assert.equal(view.collections.tree[0].variables.at(-1).value,'new-token')
    view.nodes('modal')[0].props.onClose()
    await settle()
    assert.equal(view.nodes('modal').length,0)
    assert.equal(view.rows().length,3)
    view.nodes('environment-menu',view.rows().at(-1))[0].props.onSelect('local')
    await settle()
    view.nodes('dialog-button').find(node=>node.props.danger).props.onClick()
    await settle()
    assert.equal(view.nodes('modal').length,0)
    assert.deepEqual(view.collections.tree[0].variables,[variable('base','host','default.test')])
    assert.deepEqual(view.environment.collections.catalog,[variable('moved','token','new-token')])
    assert.equal(view.writes.filter(write=>write.move).length,1)
})

test('moving into All also requires confirmation when that name already exists', async context => {
    const view = mount(context)
    view.environment.collections.catalog = [variable('block','host','local.test')]
    await settle()
    view.nodes('environment-menu',view.rows()[1])[0].props.onSelect(null)
    await settle()
    assert.equal(view.nodes('modal').length,1)
    assert.equal(view.writes.length,0)
    view.nodes('dialog-button').find(node=>node.props.danger).props.onClick()
    await settle()
    assert.deepEqual(view.collections.tree[0].variables,[variable('moved','host','local.test')])
    assert.deepEqual(view.environment.collections.catalog,[])
})
