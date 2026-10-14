import { computed, watch } from 'vue'
import type { MenuGroup as NativeMenu } from '../../bindings/schemo/internal/transport/desktop'
import { SystemService } from '../../bindings/schemo/internal/transport/wails'
import { useMessages } from '../i18n'
import { useWailsEvent } from './events/useWailsEvent'
import { useAppMenu } from './useAppMenu'
import { usePlatform } from './usePlatform'

// Rows the system's own menus already carry on macOS: the Edit menu's items, which work wherever
// text is, and the ones the application menu owns. The window's drawn menu has no such menu to lean
// on and draws them itself; the system's bar would show two of each.

const SYSTEM_ROWS = new Set([
  'undo',
  'redo',
  'cut',
  'copy',
  'paste',
  'settings',
  'quit',
  'checkUpdates',
  'about',
])

/** The window's own menu, sent to the system's bar on the platforms where the system draws one: the
 *  same rows, the same keys, and the same enabled and checked state the drawn menu shows. A row is
 *  not run here — the system hands its id back and the row of the drawn menu runs it. */
export function useNativeMenu() {
  const { customTitlebar } = usePlatform()
  // The platforms that draw their own titlebar draw their own menu too — `AppMenu` owns it there,
  // and there is no system bar to send anything to.
  if (customTitlebar.value) return

  const { t } = useMessages()
  const { groups } = useAppMenu()

  const rows = new Map<string, () => void>()
  for (const group of groups.value) {
    for (const section of group.sections) {
      for (const row of section) if (row.run) rows.set(row.id, row.run)
    }
  }

  const tree = computed<NativeMenu[]>(() =>
    groups.value
      .map((group) => ({
        id: group.id,
        label: t(group.label),
        sections: group.sections
          .map((section) => section.filter((row) => !SYSTEM_ROWS.has(row.id)))
          .filter((section) => section.length > 0)
          .map((section) =>
            section.map((row) => ({
              id: row.id,
              label: t(row.label),
              accel: row.accel,
              checked: row.checked,
              disabled: row.disabled,
            }))
          ),
      }))
      .filter((group) => group.sections.length > 0)
  )

  // The bar is rebuilt whenever what it shows moves; the window's stores move far more often than
  // the rows do, so the rows are compared and identical ones are not sent at all.
  let sent = ''
  watch(
    tree,
    () => {
      const payload = JSON.stringify(tree.value)
      if (payload === sent) return
      sent = payload
      void SystemService.ApplyWindowMenu(tree.value).catch(() => {})
    },
    { immediate: true }
  )

  useWailsEvent('menu-command', (event) => rows.get(String(event.data ?? ''))?.())
}
