import { computed } from 'vue'
import { ListSide } from '../../bindings/schemo/internal/domain'
import { useSetting, useSettings } from './useSettings'

/** Hiding the list and bringing it back. What it comes back to is the side it was on, which is why
 *  the side before the hiding is kept beside the answer. */
export function useSidebarToggle() {
  const { settings, setLayout } = useSettings()

  const last = useSetting('listSideLast')
  const hidden = computed(() => settings.value?.listSide === ListSide.ListSideHidden)

  function toggle(): void {
    setLayout({ listSide: hidden.value ? last.value : ListSide.ListSideHidden })
  }

  return { hidden, toggle }
}
