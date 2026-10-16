import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { readFileSync } from 'node:fs'
import { after, test } from 'node:test'
import { setImmediate } from 'node:timers/promises'
import { createRenderer, h, nextTick, reactive } from 'vue'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import { createServer } from 'vite'

const catalogs = await Promise.all(['en', 'ru'].map(async locale =>
  JSON.parse(await readFile(new URL(`../src/locales/${locale}.json`, import.meta.url), 'utf8')),
))
const message = key => key.split('.').reduce((value, part) => value[part], catalogs[0])
globalThis.descriptionCatalog = message
globalThis.window = { addEventListener() {}, removeEventListener() {} }
globalThis.descriptionStore = reactive({
  tree: [{ id: 'collection', description: 'Existing description.', items: [], children: [] }],
  saved: [],
  async describe(id, text) { this.saved.push({ id, text }); this.tree[0].description = text },
})
globalThis.descriptionEditor = { opened: [], scrolled: [], text: '' }

const vite = await createServer({
  configFile: false,
  optimizeDeps: { noDiscovery: true, include: [] },
  server: { middlewareMode: true, watch: null, hmr: false, ws: false },
  appType: 'custom',
  plugins: [{
    name: 'description-ports', enforce: 'pre',
    resolveId(id) {
      if (id.startsWith('@milkdown/kit/')) return '\0description:' + id
    },
    load(id) {
      if (id.startsWith('\0description:')) {
        return 'export const Editor = {make() {throw new Error("A closed editor must not be created")}}'
      }
      const path = id.replaceAll('\\', '/')
      if (path.endsWith('/src/stores/collections.ts')) return 'export const useCollectionsStore = () => globalThis.descriptionStore'
      if (path.endsWith('/src/i18n.ts')) return 'export const useMessages = () => ({t: globalThis.descriptionCatalog})'
      if (path.endsWith('/src/composables/useDeferredWrite.ts')) return `import {ref} from 'vue';
        export function useDeferredWrite(write) {
          const dirty = ref(false);
          return {dirty, touch() {dirty.value = true}, flush: write, cancel() {dirty.value = false}};
        }`
      if (path.endsWith('/src/composables/useMarkdownEditor.ts')) return `import {reactive} from 'vue';
        export function useMarkdownEditor() {
          const held = globalThis.descriptionEditor;
          return {
            standing: reactive({block: 'p'}),
            async open(host, text) {held.opened.push(text); held.text = text}, close() {},
            markdown: () => held.text, setText(text) {held.text = text},
          };
        }`
      if (path.endsWith('/ui/DocEditor.vue') || path.endsWith('/collections/DescriptionEditor.vue')) {
        const { descriptor } = parse(readFileSync(path, 'utf8'), { filename: path })
        const script = compileScript(descriptor, { id: path, inlineTemplate: true })
        return ts.transpileModule(script.content, {
          compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
        }).outputText
      }
      if (path.endsWith('.vue')) {
        return 'export default {render() {return null}}'
      }
    },
  }],
})
after(() => vite.close())
const { missingDescriptionSections } = await vite.ssrLoadModule('/src/lib/descriptionSections.ts')
const { default: DescriptionEditor } = await vite.ssrLoadModule('/src/components/collections/DescriptionEditor.vue')
const { useMarkdownEditor } = await vite.ssrLoadModule('/src/composables/useMarkdownEditor.ts?actual')

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
