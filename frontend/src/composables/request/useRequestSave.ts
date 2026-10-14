import { computed, ref, useTemplateRef } from 'vue'
import type { PaneSource } from '../../stores/content'
import type SaveRequestPopover from '../../components/collections/SaveRequestPopover.vue'
import { collectionPlaces } from '../../lib/collectionPlaces'
import { useCollectionsStore } from '../../stores/collections'
import { useToast } from '../useToast'
import { useCommands } from '../useCommands'
import { useMessages } from '../../i18n'

// The bookmark of a request tab: the save panel for a draft, and the save of the edits themselves
// for a request that already lives in a collection. `can` is false for a card — a card is kept by
// its own Save.
export function useRequestSave(source: PaneSource, can: () => boolean) {
  const { t } = useMessages()
  const toast = useToast()
  const collections = useCollectionsStore()
  const { hint: combo } = useCommands()

  const open = ref(false)
  // The panel's own key belongs to the panel while it is open.
  const panel = useTemplateRef<InstanceType<typeof SaveRequestPopover>>('savePanel')

  // A draft is a line nothing has taken in yet; a bound request is one the tree already holds.
  const bound = computed(() => !can())

  // A line with no address is not a request yet.
  const canNow = computed(() => can() && Boolean(source.url.trim()))

  // The dot says the tab holds something unwritten; the draft's own answer, both ways round.
  const unsaved = computed(() => !bound.value && source.dirty)
  const marked = computed(() => source.dirty)

  const hint = computed(() => {
    if (bound.value) {
      return source.dirty
        ? `${t('request.saveDirtyTitle')} · ${combo('save')}`
        : t('request.saveCleanTitle')
    }
    if (!canNow.value) return t('request.saveNoAddressTitle')
    return `${t('request.saveDraftTitle')} · ${combo('save')}`
  })

  // Where a save could go — also how the shortcut learns its remembered place is still there.
  const places = computed(() => collectionPlaces(collections.tree))

  // The address without its scheme: the same endpoint is usually read and written in one collection,
  // so the verb is left out and two rows stay tellable apart.
  const defaultName = computed(() => {
    const raw = source.url.trim()
    if (!raw) return t('request.newRequestName')
    return raw.replace(/^[a-z][a-z0-9+.-]*:\/\//i, '')
  })

  // What the button and the menu row do: a bound request writes its edits where it lives, a draft
  // opens the panel — there is nothing to write a draft into until it is given a place.
  async function save() {
    if (bound.value) {
      if (!source.dirty) return
      if (!(await source.saveNode())) return
      toast.show(t('request.saved'))
      return
    }
    if (!canNow.value) return
    open.value = true
  }

  // Saves a draft to the place the last save went. A place the tree no longer has — or a line
  // nothing has been done to — opens the panel instead: that is where either is found out.
  async function saveShortcut() {
    if (bound.value) {
      await save()
      return
    }
    // The panel is up, and the last row of it says what the key does there: the key is the panel's
    // while the panel is open, or the row would be advertising a key that saves somewhere else.
    if (open.value) {
      panel.value?.onSaveKey()
      return
    }
    const place = places.value.find((p) => p.id === collections.lastSaveTarget)
    if (!unsaved.value || !place) {
      open.value = true
      return
    }
    const draftID = source.draftId
    if (!draftID) return
    if (!(await collections.saveDraft(place.id, defaultName.value, draftID))) return
    source.markClean()
    toast.show(t('collections.savedTo', { name: place.path.join(' / ') }))
  }

  return { open, panel, bound, canNow, unsaved, marked, hint, places, defaultName, save, saveShortcut }
}
