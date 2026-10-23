import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createSSRApp, reactive } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { vite } from './vite.mjs'

globalThis.publicationUi = reactive({})
globalThis.collectionUi = reactive({tree: [], collectionId: ''})
// Wrappers that draw their slot and nothing else, standing in for reka parts and ui components.
const box = `import {h} from 'vue'; const Box = {setup(_, {slots}) {return () => h('div', slots.default?.())}};`
const boxComponent = `<script>${box} export default Box</script>`
const empty = '<script>export default {render() {return null}}</script>'
const boxedUi = /\/ui\/(PopoverContent|TabsList|TabsTrigger|DropdownMenuContent|DropdownMenuItem|DropdownMenuSeparator|Dialog)\.vue$/
const emptyTabs = /\/collections\/(CollectionAuth|DescriptionEditor|CollectionOverview|CollectionRuns|CollectionScripts|CollectionVariables)\.vue$/

const load = await vite({
  'reka-ui': box + 'export const DialogTitle = Box, PopoverRoot = Box, PopoverTrigger = Box, TabsRoot = Box, DropdownMenuRoot = Box, DropdownMenuTrigger = Box;',
  '/src/stores/publications.ts': 'export const usePublicationsStore = () => globalThis.publicationUi; export const hostOf = () => "share.example.com"',
  '/src/stores/workspaces.ts': 'export const useWorkspacesStore = () => ({activeId: "workspace", canWrite: true})',
  '/bindings/schemo/internal/cloud/index.ts': 'export const Publications = {Details: () => Promise.resolve(globalThis.publicationDetails)}',
  '/bindings/schemo/internal/script/index.ts': 'export const Scripts = {Scripts: () => Promise.resolve(null)}',
  '/bindings/schemo/internal/workspace/index.ts': 'export const Collections = {Contents: () => Promise.resolve([])}',
  '/src/composables/useWorkspaceSnapshot.ts': `import {ref} from 'vue'; export const useWorkspaceSnapshot = initial => ({
      state: ref(Array.isArray(initial) ? initial : globalThis.publicationDetails), loading: ref(false), reset() {}, take: request => request, signal: () => new AbortController().signal,
    })`,
  '/src/stores/collections.ts': 'export const useCollectionsStore = () => globalThis.collectionUi',
  '/src/stores/transfer.ts': 'export const useTransferStore = () => ({})',
  '/src/lib/calls.ts': 'export const callOrToast = request => request',
  '/src/composables/useLevelRun.ts': `import {ref} from 'vue';
    export const useLevelRun = () => ({run: ref(null), tableRows: ref([]), resultOf() {}})`,
  '/src/stores/environments.ts': 'export const useEnvironmentsStore = () => ({})',
  '/src/stores/tabs.ts': 'export const useTabsStore = () => ({})',
  '/src/composables/usePlan.ts': 'export const usePlan = () => ({includes: () => true})',
  '/src/composables/useToast.ts': 'export const useToast = () => ({show() {}})',
  '/src/i18n/index.ts': `export const t = (key, args) => key + (args?.url ? ':' + args.url : '');
    export const te = () => true; export const currentLocale = () => 'en'; export const formatAgo = () => 'today';`,
  '/ui/Icon.vue': '<script>export default {render() {return null}}</script>',
}, {
  vue: true,
  other: (path) => boxedUi.test(path) ? boxComponent : emptyTabs.test(path) ? empty : undefined,
})
const { default: SharePopover } = await load('/src/components/cloud/SharePopover.vue')
const { default: UnpublishDialog } = await load('/src/components/cloud/UnpublishDialog.vue')
const { default: PublicationsPage } = await load('/src/components/cloud/PublicationsPage.vue')
const { default: CollectionPage } = await load('/src/components/collections/CollectionPage.vue')

function setup(page = null, changes = []) {
  const mark = {page, behind: changes.length > 0, changes}
  Object.assign(globalThis.publicationUi, {
    open: true, collection: 'collection', publication: page, asking: '', pages: [],
    preview: {collectionId: 'collection'}, markOf: () => mark,
    options: {docs: true, examples: true}, environments: [], environment: '',
    report: {hidden: [], problems: []}, visibility: page?.visibility ?? 'unlisted',
    password: '', busy: false, reading: false, behind: changes.length > 0, changes,
    edit() {}, closeShare() {}, mark() {},
    selectedCollection: '',
  })
}
const live = {url: 'https://share.example.com/page', visibility: 'unlisted', canManage: true, revision: 2, status: 'active'}
const render = () => renderToString(createSSRApp(SharePopover, {collectionId: 'collection'}))

test('a folder shows its inherited publication notice instead of a share popover', async () => {
  const page = {...live, collectionId: 'collection'}
  setup(page)
  globalThis.publicationUi.markOf = () => ({page, inheritedFrom: 'Catalog', behind: false, changes: []})
  globalThis.collectionUi.tree = [{id: 'folder', parentId: 'collection', name: 'Orders'}]
  globalThis.collectionUi.collectionId = 'folder'

  const html = await renderToString(createSSRApp(CollectionPage))
  assert.match(html, /class="publication-notice"/)
  assert.match(html, /publications.visibleOnPage/)
  assert.match(html, /publications.inheritedPublication/)
  assert.doesNotMatch(html, /class="share-btn|class="share-pop/)
})

test('a folder without an active parent publication has no publication controls', async () => {
  setup()
  globalThis.collectionUi.tree = [{id: 'folder', parentId: 'collection', name: 'Orders'}]
  globalThis.collectionUi.collectionId = 'folder'

  for (const page of [null, {...live, pendingUnpublish: true}, {...live, status: 'taken_down'}]) {
    globalThis.publicationUi.markOf = () => ({page, inheritedFrom: page ? 'Catalog' : ''})
    const html = await renderToString(createSSRApp(CollectionPage))
    assert.doesNotMatch(html, /publication-notice|class="share-btn|class="share-pop/)
  }
})

test('the root collection retains its share popover', async () => {
  setup()
  globalThis.collectionUi.tree = [{id: 'collection', name: 'Catalog'}]
  globalThis.collectionUi.collectionId = 'collection'

  const html = await renderToString(createSSRApp(CollectionPage))
  assert.match(html, /class="share-btn/)
  assert.doesNotMatch(html, /publication-notice/)
})

test('the draft has one access and environment form and the initial publish action', async () => {
  setup()
  const html = await render()
  assert.equal((html.match(/class="publication-field"/g) ?? []).length, 2)
  assert.match(html, /publications.headline/)
  assert.match(html, /publications.hiddenNothing/)
  assert.doesNotMatch(html, /publications.showOnPage/)
})

test('the publications page shows changes, settings and versions in one column with two display options', async () => {
  const page = {...live, collectionId: 'collection', status: 'active'}
  setup(page)
  globalThis.publicationUi.pages = [page]
  globalThis.publicationDetails = {views: 17, imports: 3, versions: [
    {revision: 2, publishedAt: 2, requests: 4, environmentName: 'Production', restoredFrom: 0},
    {revision: 1, publishedAt: 1, requests: 3, environmentName: 'Stage', restoredFrom: 0},
  ]}
  const html = await renderToString(createSSRApp(PublicationsPage))
  assert.match(html, /publications.statistics/)
  assert.match(html, /publications.settings/)
  assert.match(html, /publications.versions/)
  assert.match(html, /publications.onPage/)
  assert.match(html, /publications.restore/)
  assert.match(html, /Production|Stage/)
  assert.equal((html.match(/class="publication-field"/g) ?? []).length, 2)
  assert.equal((html.match(/class="(?:selected )?option-chip"/g) ?? []).length, 2)
  assert.doesNotMatch(html, /publications.showTryIt|publications.showScripts/)
  assert.doesNotMatch(html, /pane-columns/)
  assert.ok(html.indexOf('class="pane-changes"') < html.indexOf('class="pane-settings"'))
  assert.ok(html.indexOf('class="pane-settings"') < html.indexOf('class="pane-versions"'))
  assert.match(html, /class="version-summary"/)
  assert.match(html, /publications.actions/)
  assert.match(html, /publications.copyLink/)
  assert.doesNotMatch(html, /class="pane-foot"/)
})

test('publication settings open on the collection selected by the share popover', async () => {
  const first = {...live, collectionId: 'first', status: 'active', url: 'https://share.example.com/first'}
  const chosen = {...live, collectionId: 'chosen', status: 'active', url: 'https://share.example.com/chosen'}
  setup(chosen)
  globalThis.publicationUi.pages = [first, chosen]
  globalThis.publicationUi.selectedCollection = 'chosen'
  const html = await renderToString(createSSRApp(PublicationsPage))
  assert.match(html, /https:\/\/share.example.com\/chosen/)
  assert.doesNotMatch(html, /https:\/\/share.example.com\/first/)
})

test('a reader sees publication settings and history without write actions', async () => {
  const page = {...live, collectionId: 'collection', status: 'active', canManage: false}
  setup(page)
  globalThis.publicationUi.pages = [page]
  const html = await renderToString(createSSRApp(PublicationsPage))
  assert.match(html, /class="field-value" disabled/)
  assert.doesNotMatch(html, /publications.restore|publications.unpublish|class="update"/)
})

test('a locally queued takedown cannot offer version restoration', async () => {
  const page = {...live, collectionId: 'collection', status: 'active', pendingUnpublish: true}
  setup(page)
  globalThis.publicationUi.pages = [page]
  const html = await renderToString(createSSRApp(PublicationsPage))
  assert.match(html, /publications.emptyTitle/)
  assert.doesNotMatch(html, /publications.restore|publications.settings/)
})

test('a live popover offers quick access and links to settings without duplicating the settings page', async () => {
  setup(live, [{sign: '~', method: 'GET', name: 'Updated request'}])
  const html = await render()
  assert.equal((html.match(/class="(?:quick )?publication-field(?: quick)?"/g) ?? []).length, 1)
  assert.doesNotMatch(html, /option-chip|publications.environment|publications.showDocs|publications.showExamples|chg-row/)
  assert.match(html, /publications.publicationSettings/)
  assert.doesNotMatch(html, /publications.showTryIt/)
  assert.match(html, /publications.update/)
  assert.doesNotMatch(html, /publications.showScripts/)
  assert.match(html, /share.example.com\/page/)
})

test('an existing password page can update without retyping its password and offers a separate password save', async () => {
  setup({...live, visibility: 'password'}, [{sign: '~', method: 'GET', name: 'Changed'}])
  const html = await render()
  assert.match(html, /placeholder="publications.passwordUnavailable"/)
  const button = html.match(/<button[^>]*class="action-row update"[^>]*>/)?.[0]
  assert.ok(button)
  assert.doesNotMatch(button, /disabled/)
  assert.match(html, /class="save-password" disabled/)
  assert.match(html, /publications.generatePassword/)
  assert.match(html, /publications.showPassword/)
})

test('saved passwords are masked and offer copy controls in both publication editors', async () => {
  const page = {...live, collectionId: 'collection', visibility: 'password'}
  setup(page)
  globalThis.publicationUi.pages = [page]
  globalThis.publicationUi.password = 'locally-saved-password'
  globalThis.publicationUi.passwordChanged = false
  for (const html of [await render(), await renderToString(createSSRApp(PublicationsPage))]) {
    const field = html.match(/<input[^>]*value="locally-saved-password"[^>]*>/)?.[0]
    assert.ok(field)
    assert.match(field, /type="password"/)
    assert.match(html, /publications.copyPassword/)
    assert.match(html, /class="save-password" disabled/)
  }
})

test('a closed share popover cannot render the reset initial publication form', async () => {
  setup(live)
  globalThis.publicationUi.open = false
  globalThis.publicationUi.collection = ''
  globalThis.publicationUi.publication = null
  const html = await render()
  assert.match(html, /class="share-btn/)
  assert.doesNotMatch(html, /class="share-pop|publications.headline|draft-fields|quick-fields/)
})

test('the takedown dialog retains the page URL after the share popover closes', async () => {
  setup(live)
  globalThis.publicationUi.collection = ''
  globalThis.publicationUi.asking = 'collection'
  globalThis.publicationUi.preview = null
  const html = await renderToString(createSSRApp(UnpublishDialog))
  assert.match(html, /publications.unpublishBody:https:\/\/share.example.com\/page/)
  assert.match(html, /unpublish-button/)
})
