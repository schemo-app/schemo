import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createRenderer, h, nextTick } from 'vue'
import { clientComponent, vite } from './vite.mjs'

const load = await vite({
    '/src/i18n/index.ts': 'export const t = key => key; export const describeFailure = String',
}, {other:path=>path.endsWith('/RowCreate.vue') ? clientComponent(path) : undefined})
const {default: RowCreate} = await load('/src/components/ui/RowCreate.vue')

function mount(context, create, validateName) {
    const writes = []
    const nodes = []
    const renderer = createRenderer({
        createElement(type){const node={type,props:{},focus(){node.focused=true}}; nodes.push(node); return node},
        createText:()=>({}), createComment:()=>({}), setText(){}, setElementText(){},
        patchProp(node,key,previous,value){node.props[key]=value},
        insert(){}, remove(node){nodes.splice(nodes.indexOf(node),1)}, parentNode:()=>null, nextSibling:()=>null,
    })
    const app = renderer.createApp({render:()=>h(RowCreate, {namePlaceholder:'Name',valuePlaceholder:'Value',validateName,create:async (name,value)=>{
        writes.push({name,value})
        return create ? create(name,value) : true
    }})})
    app.mount({})
    context.after(()=>app.unmount())
    return {
        writes,
        buttons:()=>nodes.filter(node=>node.type === 'button'),
        input:()=>nodes.find(node=>node.type === 'input'),
        async start(field=0){nodes.filter(node=>node.type === 'button')[field].props.onClick(); await nextTick()},
    }
}

test('clicking an empty placeholder, leaving it and pressing Escape never creates a variable', async context => {
    const view = mount(context)
    await view.start()
    await view.input().props.onBlur()
    await nextTick()
    assert.deepEqual(view.writes,[])
    await view.start()
    view.input().props.onInput({target:{value:'   '}})
    await view.input().props.onBlur()
    await nextTick()
    assert.deepEqual(view.writes,[])
    await view.start()
    view.input().props.onInput({target:{value:'host'}})
    for (const keydown of view.input().props.onKeydown) {
        keydown({key:'Escape',stopPropagation(){},preventDefault(){}})
    }
    await nextTick()
    assert.deepEqual(view.writes,[])
})

test('entering a value first keeps it locally until a valid name is submitted', async context => {
    const view = mount(context)
    await view.start(1)
    view.input().props.onInput({target:{value:'api.test'}})
    await view.input().props.onBlur()
    await nextTick()
    assert.deepEqual(view.writes,[])
    await view.start()
    view.input().props.onInput({target:{value:' host '}})
    await view.input().props.onBlur()
    await nextTick()
    assert.deepEqual(view.writes,[{name:'host',value:'api.test'}])
})

test('invalid names are retained for correction without creating a variable', async context => {
    const view = mount(context,undefined,name=>name.includes(' ') ? 'Invalid name' : '')
    await view.start()
    view.input().props.onInput({target:{value:'not a name'}})
    await view.input().props.onBlur()
    await nextTick()
    assert.deepEqual(view.writes,[])
    assert.equal(view.input().props.value,'not a name')
})

test('Enter from the value placeholder opens the name input without submitting an unnamed row', async context => {
    const view = mount(context)
    await view.start(1)
    const valueInput = view.input()
    valueInput.props.onInput({target:{value:'api.test'}})
    valueInput.props.onKeydown[0]({key:'Enter',preventDefault(){}})
    await valueInput.props.onBlur()
    await nextTick()
    assert.equal(view.input().props['aria-label'],'Name')
    assert.equal(view.input().focused,true)
    assert.deepEqual(view.writes,[])
})

test('table names may contain spaces when the table does not restrict them', async context => {
    const view = mount(context)
    await view.start()
    view.input().props.onInput({target:{value:'user name'}})
    await view.input().props.onBlur()
    assert.deepEqual(view.writes,[{name:'user name',value:''}])
})

test('blur while creation is pending does not create the row twice', async context => {
    let release
    const view = mount(context, ()=>new Promise(resolve=>{release=resolve}))
    await view.start()
    view.input().props.onInput({target:{value:'host'}})
    const pending = view.input().props.onBlur()
    await view.input().props.onBlur()
    assert.deepEqual(view.writes,[{name:'host',value:''}])
    release(true)
    await pending
})
