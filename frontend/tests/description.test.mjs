import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { after, test } from 'node:test'
import { setImmediate } from 'node:timers/promises'
import { createRenderer, h, nextTick, reactive } from 'vue'
import { clientComponent, vite } from './vite.mjs'

const catalogs = await Promise.all(['en', 'ru'].map(async locale =>
  JSON.parse(await readFile(new URL(`../src/i18n/locales/${locale}.json`, import.meta.url), 'utf8')),
))
const message = key => key.split('.').reduce((value, part) => value[part], catalogs[0])
globalThis.descriptionCatalog = message
globalThis.window = { addEventListener() {}, removeEventListener() {} }
globalThis.descriptionStore = reactive({
  tree: [{ id: 'collection', description: 'Existing description.', items: [], children: [] }],
  saved: [],
  async setDescription(id, text) { this.saved.push({ id, text }); this.tree[0].description = text },
})
globalThis.descriptionEditor = { opened: [], scrolled: [], text: '' }

const load = await vite({
  '@milkdown/kit/': 'export const Editor = {make() {throw new Error("A closed editor must not be created")}}',
  '/src/stores/collections.ts': 'export const useCollectionsStore = () => globalThis.descriptionStore',
  '/src/i18n/index.ts': 'export const t = (...args) => globalThis.descriptionCatalog(...args)',
  '/src/composables/useDeferredWrite.ts': `import {ref} from 'vue';
    export function useDeferredWrite(write) {
      const dirty = ref(false);
      return {dirty, touch() {dirty.value = true}, flush: write, cancel() {dirty.value = false}};
    }`,
  '/src/composables/useMarkdownEditor.ts': `import {reactive} from 'vue';
    export function useMarkdownEditor() {
      const held = globalThis.descriptionEditor;
      return {
        standing: reactive({block: 'p'}),
        async open(host, text) {held.opened.push(text); held.text = text}, close() {},
        markdown: () => held.text, setText(text) {held.text = text},
      };
    }`,
}, {
  other: (path) => {
    if (path.endsWith('/ui/DocEditor.vue') || path.endsWith('/collections/DescriptionEditor.vue')) return clientComponent(path)
    if (path.endsWith('.vue')) return 'export default {render() {return null}}'
  },
})
const { missingDescriptionSections } = await load('/src/lib/descriptionSections.ts')
const { default: DescriptionEditor } = await load('/src/components/collections/DescriptionEditor.vue')
const { useMarkdownEditor } = await load('/src/composables/useMarkdownEditor.ts?actual')

test('closing during editor loading preserves the document and cancels editor creation', async () => {
  const editor = useMarkdownEditor(() => {assert.fail('A closed editor must not emit changes')})
  const opening = editor.open({}, 'Description with an appended section.')
  assert.equal(editor.markdown(), 'Description with an appended section.')
  await editor.close()
  await opening
})

test('only actual H2 headings satisfy description suggestions in every supported language', () => {
  for (const catalog of catalogs) {
    const titles = ['docsAuthorization', 'docsErrors', 'docsLimits'].map(key => catalog.collections[key])
    const document = titles.map(title => `## ${title}\nContent.`).join('\n\n')
    assert.equal(missingDescriptionSections(document).length, 0)
    assert.equal(missingDescriptionSections(`\`\`\`md\n${document}\n\`\`\``).length, 3)
    assert.equal(missingDescriptionSections(titles.map(title => `### ${title}`).join('\n')).length, 3)
  }
})

test('adding a missing section edits and saves the complete description once, then removes its suggestion', async context => {
  const editor = globalThis.descriptionEditor
  const store = globalThis.descriptionStore
  const nodes = []
  const renderer = createRenderer({
    createElement(type) {
      const node = {type, props: {}, children: [], scrollHeight: 600,
        scrollTo(options) {editor.scrolled.push(options.top)},
      }
      nodes.push(node)
      return node
    },
    createText: text => ({text}), createComment: () => ({}),
    setText(node, text) {node.text = text}, setElementText(node, text) {node.text = text},
    patchProp(node, key, previous, value) {node.props[key] = value},
    insert(node, parent, anchor) {
      if (node.parent) node.parent.children.splice(node.parent.children.indexOf(node), 1)
      node.parent = parent
      const index = anchor ? parent.children.indexOf(anchor) : -1
      parent.children.splice(index < 0 ? parent.children.length : index, 0, node)
    },
    remove(node) {
      if (!node) return
      if (node.parent) node.parent.children.splice(node.parent.children.indexOf(node), 1)
      const remove = held => {held.removed = true; held.children?.forEach(remove)}
      remove(node)
    },
    parentNode: node => node?.parent,
    nextSibling: node => node?.parent?.children[node.parent.children.indexOf(node) + 1] ?? null,
  })
  const app = renderer.createApp({render: () => h(DescriptionEditor, {
    id: 'collection', emptyKey: 'request.docs.emptyCollection', suggestSections: true,
  })})
  app.mount({children: []})
  context.after(() => app.unmount())
  const buttons = () => nodes.filter(node => !node.removed && node.props.class?.includes('doc-toc-suggestion'))
  assert.equal(buttons().length, 3)
  buttons()[0].props.onClick()
  await nextTick()
  await setImmediate()
  const title = catalogs[0].collections.docsAuthorization
  const hint = catalogs[0].collections.docsAuthorizationHint
  const expected = `Existing description.\n\n## ${title}\n\n${hint}\n`
  assert.equal(editor.opened.at(-1), expected)
  assert.deepEqual(editor.scrolled, [600])
  const done = nodes.findLast(node => !node.removed && node.props.class === 'doc-done')
  done.props.onClick()
  await nextTick()
  assert.equal(store.saved.length, 1)
  assert.equal(store.saved[0].id, 'collection')
  assert.equal(store.saved[0].text, expected.trim())
  assert.equal(missingDescriptionSections(store.tree[0].description).length, 2)
  assert.equal(buttons().length, 2)
})
