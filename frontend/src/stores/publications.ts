import { computed, onScopeDispose, ref } from 'vue'
import { defineStore } from 'pinia'
import type { Publication } from '../../bindings/schemo/internal/domain'
import type { Change, PublishPreview } from '../../bindings/schemo/internal/usecase/publication'
import { PublicationsService } from '../../bindings/schemo/internal/transport/wails'
import { useLocale } from '../composables/useLocale'
import { useToast } from '../composables/useToast'
import { t } from '../i18n'
import { isVisibility } from '../lib/publications'
import { callOrToast, runOrToast } from './calls'
import { useEnvironmentsStore } from './environments'
import { useWorkspacesStore } from './workspaces'
import { useWorkspaceSnapshot } from '../composables/workspace/useWorkspaceSnapshot'

/** The three accesses a page may have. The text the window shows for them is the catalogue's. */
export type Visibility = 'unlisted' | 'public' | 'password'

/** Where a page is: the server issues the address, and the only host this window names is it. */
export function hostOf(url: string): string {
  try {
    return new URL(url).host
  } catch {
    return ''
  }
}

/** The pages of a space, and the popover one collection is published from. Go answers both: this
 *  holds what it said, so a window draws a published collection as published before anybody is
 *  asked anything — and it asks nothing of the network to do it. */
export const usePublicationsStore = defineStore('publications', () => {
  const workspaces = useWorkspacesStore()
  const env = useEnvironmentsStore()
  const toast = useToast()
  const { locale } = useLocale()

  // The pages of the space, newest first, as this device last knew them.
  const pageSnapshot = useWorkspaceSnapshot<Publication[]>([])
  const pages = pageSnapshot.state
  const loading = pageSnapshot.loading

  // Whether each collection's page is behind what this device holds, keyed by collection: what a
  // collection's own head draws its button from, without opening the popover.
  const marks = ref<Record<string, Change>>({})

  // The page a person has asked to take down, asked about before it is.
  const asking = ref('')

  // The popover: one at a time, on the collection it was opened from, holding what Preview said.
  const open = ref(false)
  const collection = ref('')
  const environment = ref('')
  const visibility = ref<Visibility>('unlisted')

  // The password a page is about to stand behind. It lives in the window and nowhere else: Go hands
  // it to the server once and keeps no copy, so the only moment it exists is between the field and
  // the publish, and closing the popover is the end of it.
  const password = ref('')
  const previewSnapshot = useWorkspaceSnapshot<PublishPreview | null>(null)
  const preview = previewSnapshot.state
  const reading = previewSnapshot.loading
  const busy = ref(false)

  const space = () => workspaces.activeId

  // A page that answers at its address. A takedown this device is still holding is not one: the
  // row stays beside the address, and what a window draws is a collection nobody can read yet.
  //
  // The mark stands in until the preview answers, and the preview once it has: a published
  // collection's popover opens as the page it is, and a collection inside a published folder opens
  // as what holds it — the frame under the button would otherwise be one nobody has published.
  const publication = computed(() => {
    const held = preview.value?.publication ?? markOf(collection.value)?.page ?? null
    if (!held || held.status !== 'active' || held.pendingUnpublish) return null
    return held
  })
  const behind = computed(() => {
    const said = preview.value
      ? preview.value.behind === true
      : markOf(collection.value)?.behind === true
    return said && publication.value !== null
  })
  const environments = computed(() => preview.value?.environments ?? [])
  const report = computed(() => preview.value?.report ?? null)
  // What the page is missing, request by request: Go reads the document built now against the
  // outline the last publish put up, so this list and the page's own changelog say the same thing.
  // A folder's list is its own requests and not the page's, which Go decides.
  const changes = computed(
    () => preview.value?.changes ?? markOf(collection.value)?.changes ?? []
  )

  function markOf(collectionID: string): Change | null {
    return marks.value[collectionID] ?? null
  }

  // What a collection's head draws: whether it is published at all, and whether the page is behind
  // the collection. Everything the window says about a page it knows without asking the server.
  async function mark(collectionID: string): Promise<void> {
    if (!collectionID || !space()) return
    const signal = pageSnapshot.signal()
    const answer = await callOrToast(
      PublicationsService.Changes(space(), collectionID).cancelOn(signal),
      'publications.readFailed'
    )
    if (answer && !signal.aborted) marks.value = { ...marks.value, [collectionID]: answer }
  }

  // Every page the list holds is marked at once, so the rail and the sidebar say what is behind
  // without a popover having been opened on each of them. Nothing here asks the server.
  async function markAll(collectionIDs: string[]): Promise<void> {
    await Promise.all(collectionIDs.map((id) => mark(id)))
  }

  async function load(): Promise<void> {
    if (!space()) {
      pageSnapshot.reset()
      return
    }
    await callOrToast(pageSnapshot.take(PublicationsService.List(space()), (list) => list ?? []), 'publications.readFailed')
  }

  // Asking the server what it holds. What it answers about is the collections this device already
  // knows of: a device may not invent a page, so the list is what limits the question.
  async function refresh(): Promise<void> {
    if (!space()) return
    await callOrToast(pageSnapshot.take(PublicationsService.Refresh(space()), (list) => list ?? []), 'publications.readFailed')
  }

  // Reading a preview asks nobody anything: it is assembled here, so the popover opens offline. A
  // different environment is a different page, which is why the choice reads a new preview.
  async function readPreview(adopt = false): Promise<void> {
    if (!collection.value) return
    const signal = previewSnapshot.signal()
    const answer = await callOrToast(
      previewSnapshot.take(PublicationsService.Preview(space(), collection.value, environment.value)),
      'publications.readFailed'
    )
    if (!answer || signal.aborted || preview.value !== answer) return
    // A page that is already up opens on the access it has; a person who has not touched the field
    // is not silently asking for something else.
    const held = answer.publication?.visibility
    if (adopt && isVisibility(held)) visibility.value = held
    // An environment this device no longer holds cannot be named, and a field with nothing in it
    // would say the addresses are read against nothing while the page was built against the first.
    // The page's own environment is named first: it is the one the field is being asked about.
    const known = answer.environments ?? []
    if (known.length > 0 && !known.some((choice) => choice.id === environment.value)) {
      const carried = known.find((choice) => choice.name === answer.publication?.environmentName)
      environment.value =
        carried?.id ?? known.find((choice) => choice.id === env.activeId)?.id ?? known[0].id
    }
  }

  function openShare(collectionID: string) {
    // Publishing writes to the space, and a space this person may only read refuses every write.
    if (!workspaces.canWrite) {
      toast.show(t('errors.codes.workspaceReadOnly'))
      return
    }
    closeShare()
    collection.value = collectionID
    // The empty id means the environment the page went out with, or the one the app works in when
    // nobody has published it: Go answers which, so the popover opens on the page's own changes.
    environment.value = ''
    // The access the mark holds is the one the page has, and reading it here is what keeps the
    // popover from offering «По ссылке» for a moment to a page that is public.
    const known = marks.value[collectionID]?.page?.visibility
    visibility.value = isVisibility(known) ? known : 'unlisted'
    password.value = ''
    open.value = true
    void readPreview(true)
    void mark(collectionID)
  }

  function closeShare() {
    open.value = false
    collection.value = ''
    previewSnapshot.reset()
    busy.value = false
    password.value = ''
  }

  async function choose(environmentID: string): Promise<void> {
    environment.value = environmentID
    await readPreview()
  }

  // A publish answers with the row the server wrote, and everything the window draws afterwards is
  // read again rather than patched: a page that came back at a new revision is a new page.
  async function publish(confirmed: boolean): Promise<boolean> {
    if (!collection.value || busy.value) return false
    const signal = AbortSignal.any([pageSnapshot.signal(), previewSnapshot.signal()])
    busy.value = true
    try {
      const held = await callOrToast(
        PublicationsService.Publish({
          workspace: space(),
          collection: collection.value,
          environmentId: environment.value,
          visibility: visibility.value,
          locale: locale.value,
          confirmed,
          password: visibility.value === 'password' ? password.value : '',
        }).cancelOn(signal),
        'publications.publishFailed'
      )
      if (!held || signal.aborted) return false
      await readPreview()
      if (signal.aborted) return false
      await mark(collection.value)
      if (signal.aborted) return false
      await load()
      return true
    } finally {
      if (!signal.aborted) busy.value = false
    }
  }

  // Updating a page from the list of pages rather than from a collection's head: it keeps the
  // access and the environment it went out with, so an update carries the requests and nothing
  // else. A row names its environment rather than numbering it, and a name this device no longer
  // holds falls back to the one the app is working in — as every address in the window does.
  async function updatePage(collectionID: string): Promise<boolean> {
    if (busy.value) return false
    const held = pages.value.find((page) => page.collectionId === collectionID)
    if (!held) return false
    // A page behind a password cannot be updated blind: the server is told the text again or it
    // would put up a page with no lock at all, so the popover opens and the person is asked for it.
    if (held.visibility === 'password') {
      openShare(collectionID)
      return false
    }
    closeShare()
    const signal = AbortSignal.any([pageSnapshot.signal(), previewSnapshot.signal()])
    collection.value = collectionID
    visibility.value = held.visibility === 'public' ? 'public' : 'unlisted'
    environment.value = ''
    await readPreview()
    if (signal.aborted) return false
    const choices = preview.value?.environments ?? []
    environment.value = choices.find((choice) => choice.name === held.environmentName)?.id ?? ''
    return publish(true)
  }

  function askUnpublish(collectionID: string) {
    asking.value = collectionID
  }

  function cancelUnpublish() {
    asking.value = ''
  }

  // A takedown the network did not carry is kept and played back later, so this answers with
  // success either way: making a person try again in better weather is not what they asked for.
  async function confirmUnpublish(): Promise<boolean> {
    const signal = pageSnapshot.signal()
    const id = asking.value
    asking.value = ''
    if (!id) return false
    const done = await runOrToast(PublicationsService.Unpublish(id).cancelOn(signal), 'publications.unpublishFailed')
    if (!done || signal.aborted) return false
    if (open.value) await readPreview()
    if (signal.aborted) return false
    await mark(id)
    if (signal.aborted) return false
    await load()
    return true
  }

  function reset() {
    pageSnapshot.reset()
    marks.value = {}
    asking.value = ''
    closeShare()
  }

  onScopeDispose(reset)

  return {
    reset,
    pages,
    loading,
    marks,
    asking,
    open,
    collection,
    environment,
    visibility,
    password,
    preview,
    publication,
    behind,
    environments,
    report,
    changes,
    reading,
    busy,
    markOf,
    mark,
    markAll,
    load,
    refresh,
    openShare,
    closeShare,
    choose,
    publish,
    updatePage,
    askUnpublish,
    cancelUnpublish,
    confirmUnpublish,
  }
})
