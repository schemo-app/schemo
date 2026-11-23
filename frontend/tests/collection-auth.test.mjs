import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createSSRApp, h, reactive } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { clientComponent, vite } from './vite.mjs'

const load = await vite({
  '/src/stores/collections.ts': 'export const useCollectionsStore = () => globalThis.authCollections',
  '/src/i18n/index.ts': 'export const t = key => key; export const textOf = key => key',
  '/src/composables/useAuthSchemes.ts': `export const useAuthSchemes = () => ({
    menuFor: () => [], nameOf: type => type,
    schemeOf: type => type === 'bearer' ? {fields:[{key:'token'}]} : null,
  })`,
  '/src/components/request/PaneSelect.vue': 'export default {render(){return null}}',
  '/src/components/request/AuthFields.vue': `
    import {h} from 'vue';
    import {useVarContext} from '/src/composables/useVarContext';
    export default {setup() {
      const context = useVarContext();
      return () => h('auth-fields', {
        'data-collection':context.value.collection,
        'data-variables':context.value.variables.map(variable => variable.name).join(','),
      });
    }};`,
}, {other:path=>path.endsWith('/collections/CollectionAuth.vue') ? clientComponent(path) : undefined})
const {default: CollectionAuth} = await load('/src/components/collections/CollectionAuth.vue')
const {provideVarContext, useVarContext} = await load('/src/composables/useVarContext.ts')

const collection = (id, variables = [], children = [], items = []) => ({
  id, name:id, variables, children, items, auth:{type:'bearer', fields:{token:'{{tokenFront}}'}},
})
const variable = name => ({id:name, name, value:'', kind:'text', enabled:true})

test('collection auth fields receive their own collection context', async () => {
  globalThis.authCollections = reactive({collectionId:'catalog', tree:[collection('catalog', [variable('baseUrl')])]})
  const html = await renderToString(createSSRApp({render:()=>h(CollectionAuth, {levelId:'catalog'})}))
  assert.match(html, /data-collection="catalog"/)
  assert.match(html, /data-variables="baseUrl"/)
})

test('nested collection auth includes its own variables and keeps the root environment block', async () => {
  const folder = collection('folder', [variable('folderToken')])
  globalThis.authCollections = reactive({collectionId:'folder', tree:[collection('catalog', [variable('baseUrl')], [folder])]})
  const html = await renderToString(createSSRApp({render:()=>h(CollectionAuth, {levelId:'folder'})}))
  assert.match(html, /data-collection="catalog"/)
  assert.match(html, /data-variables="baseUrl,folderToken"/)
})

test('request fields keep collection context and update when their request changes', async () => {
  const first = collection('catalog', [variable('baseUrl')], [], [{id:'first'}])
  const second = collection('billing', [variable('billingToken')], [], [{id:'second'}])
  globalThis.authCollections = reactive({tree:[first, second]})
  const selected = reactive({id:'first'})
  let context
  const Child = {setup() {context = useVarContext(); return () => null}}
  const Parent = {setup() {provideVarContext(() => selected.id); return () => h(Child)}}
  await renderToString(createSSRApp(Parent))
  assert.equal(context.value.collection, 'catalog')
  assert.deepEqual(context.value.variables.map(variable=>variable.name), ['baseUrl'])
  selected.id = 'second'
  assert.equal(context.value.collection, 'billing')
  assert.deepEqual(context.value.variables.map(variable=>variable.name), ['billingToken'])
  selected.id = 'missing'
  assert.deepEqual(context.value, {collection:'', variables:[]})
})
