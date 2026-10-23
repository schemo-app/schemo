import { computed, ref } from 'vue'
import { useEventListener } from '@vueuse/core'
import { Application, Browser } from '@wailsio/runtime'
import { Theme, UISize } from '../../bindings/schemo/internal/domain'
import { System } from '../../bindings/schemo/internal/app'
import { Updates } from '../../bindings/schemo/internal/desktop'
import type { MessageKey } from '../i18n'
import { acceleratorOf, capsOf } from '../lib/commands'
import { DEFAULTS } from '../lib/layout'
import { useCollectionsStore } from '../stores/collections'
import { useSearchStore } from '../stores/search'
import { useTabsStore } from '../stores/tabs'
import { useTransferStore } from '../stores/transfer'
import { inspectorState, setInspectorState, viewState } from '../stores/content'
import { responseSearchable, saveRequest, searchResponse } from '../lib/intents'
import { useCommands } from './useCommands'
import { usePlatform } from './usePlatform'
import { useSetting } from './useSettings'
import { useSidebarToggle } from './useSidebarToggle'
import { useTheme } from './useTheme'
import { zoomIn, zoomOut, zoomReset } from './useZoom'

// The window's own menu, for the platforms whose title bar it draws: Go builds no native menu there
// (`desktop.ApplyMenu` steps aside), so this is the only way to Settings, About and the rest. Where
// the system draws the bar, the same rows are sent to it instead (see useNativeMenu).
//
// Each row is bound to a command of the registry or a call of a store and adds no behavior of its
// own; one the window cannot do yet is drawn disabled rather than left out.

/** A row the menu draws: what it says, what it wears, its state, and what it does. */
export interface MenuRow {
  /** The name the row is known by: the system's bar hands this back when the row is picked, and it
   *  is what the drawn menu needs it for — the two draw the same rows. */
  id: string
  label: MessageKey
  /** The combination as the eye reads it, and the same combination as the system takes it. */
  kbd: string
  accel: string
  checked: boolean
  disabled: boolean
  run?: () => void
}

/** A group's rows, cut into sections — a rule stands between two sections, never inside one. */
export interface MenuGroup {
  /** Which submenu of the bar the rows are: the drawn menu shows them as its own groups, and the
   *  system's bar takes each group to the submenu the platform keeps for it. */
  id: 'file' | 'edit' | 'view' | 'go' | 'help'
  label: MessageKey
  sections: MenuRow[][]
}

/** The menu's groups and rows, and whether the popup is open — F10 opens it too. */
export function useAppMenu() {
  const open = ref(false)
  const tabs = useTabsStore()
  const search = useSearchStore()
  const collections = useCollectionsStore()
  const transfer = useTransferStore()
  const { combo } = useCommands()
  const appearance = useSetting('appearance')
  const { isMac, customTitlebar } = usePlatform()
  const { hidden: sidebarHidden, toggle: toggleSidebar } = useSidebarToggle()
  const { theme, setTheme } = useTheme()

  // The zoom's three are keys the window listens for and no command of the registry: `capsOf` is
  // still the one place a combination is spelled for the eye, so they are read through it.
  const caps = (combination: string) => capsOf(combination, isMac.value).join(isMac.value ? '' : '+')

  const size = computed(() => appearance.value.size)

  function row(
    id: string,
    label: MessageKey,
    run: () => void,
    state: { combo?: string; kbd?: string; checked?: boolean; disabled?: boolean } = {}
  ): MenuRow {
    const combination = state.combo ?? ''
    return {
      id,
      label,
      run,
      kbd: state.kbd ?? (combination ? caps(combination) : ''),
      accel: combination ? acceleratorOf(combination) : '',
      checked: state.checked ?? false,
      disabled: state.disabled ?? false,
    }
  }

  /** A row the app has no command for yet: drawn, so the shape of the menu is whole, and disabled. */
  function offRow(id: string, label: MessageKey): MenuRow {
    return { id, label, kbd: '', accel: '', checked: false, disabled: true }
  }

  function cycleTab(delta: number) {
    const list = tabs.tabs
    const at = list.findIndex((tab) => tab.id === tabs.activeId)
    if (at < 0) return
    void tabs.activate(list[(at + delta + list.length) % list.length].id)
  }

  const groups = computed<MenuGroup[]>(() => {
    const inspector = inspectorState(tabs.pane)
    return [
      {
        id: 'file',
        label: 'appMenu.groups.file',
        sections: [
          [
            row('newRequest', 'appMenu.item.newRequest', () => void tabs.newTab(), {
              combo: combo('newTab'),
            }),
            offRow('newCollection', 'appMenu.item.newCollection'),
          ],
          [
            row('save', 'appMenu.item.save', () => void saveRequest.trigger(), {
              combo: combo('save'),
              disabled: !tabs.pane?.canSave,
            }),
            row('closeTab', 'appMenu.item.closeTab', () => void tabs.close(tabs.activeId), {
              combo: combo('closeTab'),
              disabled: !tabs.activeId,
            }),
          ],
          [
            // The menu's import is not about the level on screen: it lands as a new collection, and
            // "Into the collection…" is the tree row's own item.
            row('importCollection', 'appMenu.item.importCollection', () => transfer.askImport(), {
              combo: combo('import'),
            }),
            row('exportCollection', 'appMenu.item.exportCollection', () =>
              transfer.askExport(collections.collectionId ?? ''), { combo: combo('export') }
            ),
          ],
          [
            row('settings', 'appMenu.item.settings', () => System.ShowSettings(''), {
              combo: combo('settings'),
            }),
            row('quit', 'appMenu.item.quit', () => void Application.Quit(), { kbd: 'Alt+F4' }),
          ],
        ],
      },
      {
        id: 'edit',
        label: 'appMenu.groups.edit',
        sections: [
          [
            offRow('undo', 'appMenu.item.undo'),
            offRow('redo', 'appMenu.item.redo'),
          ],
          [
            offRow('cut', 'appMenu.item.cut'),
            offRow('copy', 'appMenu.item.copy'),
            offRow('paste', 'appMenu.item.paste'),
          ],
          [
            row('findInResponse', 'appMenu.item.findInResponse', () => void searchResponse.trigger(), {
              combo: combo('find'),
              disabled: !responseSearchable.value,
            }),
          ],
        ],
      },
      {
        id: 'view',
        label: 'appMenu.groups.view',
        sections: [
          [
            row('sidebar', 'appMenu.item.sidebar', () => toggleSidebar(), {
              combo: combo('sidebar'),
              checked: !sidebarHidden.value,
            }),
            row(
              'inspector',
              'appMenu.item.inspector',
              () => setInspectorState(tabs.pane, { open: !inspector.open }),
              {
                combo: combo('inspector'),
                checked: inspector.open,
                disabled: !tabs.pane || viewState(tabs.pane).tab !== 'body',
              }
            ),
          ],
          [
            row('lightTheme', 'appMenu.item.lightTheme', () => setTheme(Theme.ThemeLight), {
              checked: theme.value === Theme.ThemeLight,
            }),
            row('darkTheme', 'appMenu.item.darkTheme', () => setTheme(Theme.ThemeDark), {
              checked: theme.value === Theme.ThemeDark,
            }),
          ],
          [
            row('zoomIn', 'appMenu.item.zoomIn', () => zoomIn(), {
              combo: 'mod+=',
              disabled: size.value === UISize.SizeLarge,
            }),
            row('zoomOut', 'appMenu.item.zoomOut', () => zoomOut(), {
              combo: 'mod+-',
              disabled: size.value === UISize.SizeSmall,
            }),
            row('zoomReset', 'appMenu.item.zoomReset', () => zoomReset(), {
              combo: 'mod+0',
              disabled: size.value === DEFAULTS.appearance.size,
            }),
          ],
        ],
      },
      {
        id: 'go',
        label: 'appMenu.groups.go',
        sections: [
          [
            row('history', 'appMenu.item.history', () => void tabs.openHistory(), {
              combo: combo('history'),
            }),
            row('capture', 'appMenu.item.capture', () => void tabs.openCapture()),
            row('search', 'appMenu.item.search', () => search.openPalette(), {
              combo: combo('palette'),
            }),
          ],
          [
            row('nextTab', 'appMenu.item.nextTab', () => cycleTab(1), {
              disabled: tabs.tabs.length < 2,
            }),
            row('previousTab', 'appMenu.item.previousTab', () => cycleTab(-1), {
              disabled: tabs.tabs.length < 2,
            }),
          ],
        ],
      },
      {
        id: 'help',
        label: 'appMenu.groups.help',
        sections: [
          [
            row('documentation', 'appMenu.item.documentation', () =>
              void Browser.OpenURL('https://docs.schemo.app')
            ),
            row('overview', 'appMenu.item.overview', () => void tabs.openWelcome()),
            row('shortcuts', 'appMenu.item.shortcuts', () => System.ShowSettings('shortcuts')),
          ],
          [
            row('checkUpdates', 'appMenu.item.checkUpdates', () => void Updates.RequestCheck(), {
              combo: combo('updates'),
            }),
            row('about', 'appMenu.item.about', () => System.ShowAbout()),
          ],
        ],
      },
    ]
  })

  useEventListener(window, 'keydown', (event: KeyboardEvent) => {
    if (!customTitlebar.value || event.key !== 'F10') return
    event.preventDefault()
    open.value = !open.value
  })

  return { open, groups }
}
