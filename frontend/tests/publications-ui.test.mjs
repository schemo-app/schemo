import assert from 'node:assert/strict'
import { after, test } from 'node:test'
import { createSSRApp, reactive } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'

globalThis.publicationUi = reactive({})
globalThis.collectionUi = reactive({tree: [], collectionId: ''})
const vite = await createServer({
  configFile: false, optimizeDeps: { noDiscovery: true, include: [] },
  server: { middlewareMode: true, watch: null, hmr: false, ws: false }, appType: 'custom',
  plugins: [vue(), {
    name: 'publication-ui-ports', enforce: 'pre',
    resolveId(id) { if (id === 'reka-ui') return '\0publication-reka' },
    load(id) {
      const path = id.replaceAll('\\', '/')
      const slot = `import {h} from 'vue'; const Box = {setup(_, {slots}) {return () => h('div', slots.default?.())}};`
      if (id === '\0publication-reka') return slot + 'export const DialogTitle = Box;'
      if (path.endsWith('/ui/popover/index.ts')) return slot + 'export const Popover = Box, PopoverTrigger = Box, PopoverContent = Box;'
      if (path.endsWith('/ui/tabs/index.ts')) return slot + 'export const Tabs = Box, TabsList = Box, TabsTrigger = Box;'
      if (/\/collections\/(CollectionAuth|DescriptionEditor|CollectionOverview|CollectionRuns|CollectionScripts|CollectionVariables)\.vue$/.test(path)) {
        return '<script>export default {render() {return null}}</script>'
      }
      if (path.endsWith('/ui/dropdown-menu/index.ts')) return slot + 'export const DropdownMenu = Box, DropdownMenuTrigger = Box, DropdownMenuContent = Box, DropdownMenuItem = Box, DropdownMenuSeparator = Box;'
      if (path.endsWith('/ui/dialog/index.ts')) return `import {h} from 'vue'; import {DialogRoot} from 'reka-ui';
        export const Dialog = {setup(_, {slots}) {return () => h(DialogRoot, {open: true}, slots.default)}};`
      if (path.endsWith('/src/stores/publications.ts')) return 'export const usePublicationsStore = () => globalThis.publicationUi; export const hostOf = () => "share.example.com"'
      if (path.endsWith('/src/stores/workspaces.ts')) return 'export const useWorkspacesStore = () => ({activeId: "workspace", canWrite: true})'
      if (path.endsWith('/bindings/schemo/internal/transport/wails/index.ts')) return `
        export const PublicationsService = {Details: () => Promise.resolve(globalThis.publicationDetails)};
        export const CollectionsService = {Contents: () => Promise.resolve([])};
        export const ScriptingService = {Scripts: () => Promise.resolve(null)};`
      if (path.endsWith('/src/composables/workspace/useWorkspaceSnapshot.ts')) return `import {ref} from 'vue'; export const useWorkspaceSnapshot = initial => ({
        state: ref(Array.isArray(initial) ? initial : globalThis.publicationDetails), loading: ref(false), reset() {}, take: request => request, signal: () => new AbortController().signal,
      })`
      if (path.endsWith('/src/stores/collections.ts')) return 'export const useCollectionsStore = () => globalThis.collectionUi'
      if (path.endsWith('/src/stores/transfer.ts')) return 'export const useTransferStore = () => ({})'
      if (path.endsWith('/src/stores/calls.ts')) return 'export const callOrToast = request => request'
      if (path.endsWith('/src/composables/useLevelRun.ts')) return `import {ref} from 'vue';
        export const useLevelRun = () => ({run: ref(null), tableRows: ref([]), resultOf() {}})`
      if (path.endsWith('/src/stores/environments.ts')) return 'export const useEnvironmentsStore = () => ({})'
      if (path.endsWith('/src/stores/tabs.ts')) return 'export const useTabsStore = () => ({})'
      if (path.endsWith('/src/composables/usePlan.ts')) return 'export const usePlan = () => ({includes: () => true})'
      if (path.endsWith('/src/composables/useToast.ts')) return 'export const useToast = () => ({show() {}})'
      if (path.endsWith('/src/i18n.ts')) return `export const t = (key, args) => key + (args?.url ? ':' + args.url : '');
        export const te = () => true; export const currentLocale = () => 'en'; export const useMessages = () => ({t, te}); export const formatAgo = () => 'today';`
      if (path.endsWith('/ui/Icon.vue')) return '<script>export default {render() {return null}}</script>'
    },
  }],
})
after(() => vite.close())
const { default: SharePopover } = await vite.ssrLoadModule('/src/components/publications/SharePopover.vue')
const { default: UnpublishDialog } = await vite.ssrLoadModule('/src/components/publications/UnpublishDialog.vue')
const { default: PublicationsPage } = await vite.ssrLoadModule('/src/components/publications/PublicationsPage.vue')
const { default: CollectionPage } = await vite.ssrLoadModule('/src/components/collections/CollectionPage.vue')

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
  assert.match(html, /placeholder="publications.newPassword"/)
  const button = html.match(/<button[^>]*class="action-row update"[^>]*>/)?.[0]
  assert.ok(button)
  assert.doesNotMatch(button, /disabled/)
  assert.match(html, /class="save-password" disabled/)
  assert.match(html, /publications.generatePassword/)
  assert.match(html, /publications.showPassword/)
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
