import { useWailsEvent } from './useWailsEvent'

/** Opens the settings category another window asked for. */
export function useSettingsEvents(onCategory: (category: string) => void): void {
  useWailsEvent('settings-tab', (ev) => {
    const asked = ev.data as string
    if (asked) onCategory(asked)
  })
}
