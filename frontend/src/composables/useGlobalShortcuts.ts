import { useEventListener } from '@vueuse/core'
import { SystemService, UpdateService } from '../../bindings/schemo/internal/transport/wails'
import { useCollectionsStore } from '../stores/collections'
import { useEnvironmentsStore } from '../stores/environments'
import { useSearchStore } from '../stores/search'
import { useTabsStore } from '../stores/tabs'
import { useTransferStore } from '../stores/transfer'
import { useCommands } from './useCommands'
import { useSidebarToggle } from './useSidebarToggle'

/** The commands the whole window carries out, wherever the focus is: the palette, the settings
 *  window, the history, the list's side, the update check the system menu also offers, and the
 *  environments sheet. What each wears comes from the registry, so rebinding one in the settings
 *  rebinds it here. */
export function useGlobalShortcuts(envStore: ReturnType<typeof useEnvironmentsStore>): void {
  const search = useSearchStore()
  const tabs = useTabsStore()
  const { matches } = useCommands()
  const { toggle: toggleSidebar } = useSidebarToggle()

  const actions: [command: string, run: () => void][] = [
    ['palette', () => search.openPalette()],
    ['history', () => void tabs.openHistory()],
    ['newTab', () => void tabs.newTab()],
    ['closeTab', () => tabs.activeId && void tabs.close(tabs.activeId)],
    ['closeOthers', () => tabs.activeId && void tabs.closeOthers(tabs.activeId)],
    ['settings', () => SystemService.ShowSettings('')],
    ['sidebar', toggleSidebar],
    ['updates', () => UpdateService.RequestCheck()],
    ['environments', () => envStore.sheetOpen || envStore.openSheet()],
    // The key that the menu row wears does what the row does: a new collection, not the level the
    // window happens to be showing.
    ['import', () => useTransferStore().askImport()],
    ['export', () => useTransferStore().askExport(useCollectionsStore().collectionId ?? '')],
  ]

  useEventListener(window, 'keydown', (event: KeyboardEvent) => {
    const action = actions.find(([command]) => matches(command, event))
    if (!action) return
    event.preventDefault()
    action[1]()
  })
}
