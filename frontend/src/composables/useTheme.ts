import { computed, watch } from 'vue'
import { usePreferredDark } from '@vueuse/core'
import { useWailsEvent } from './events/useWailsEvent'
import { usePersistedChoice } from './usePersistedChoice'
import { useSettings } from './useSettings'
import { Theme as DomainTheme } from '../../bindings/schemo/internal/domain'

export type Theme = DomainTheme

const THEMES = Object.values(DomainTheme).filter((one) => one !== DomainTheme.$zero)

type ViewTransition = { skipTransition(): void }
type TransitionDocument = Document & { startViewTransition?: (cb: () => void) => ViewTransition }

const theme = usePersistedChoice<Theme>('schemo-theme', 'theme', THEMES, DomainTheme.ThemeSystem)
const systemIsDark = usePreferredDark()

/** Computed flag evaluating active dark mode state. */
const isDark = computed(() =>
    theme.value === DomainTheme.ThemeSystem ? systemIsDark.value : theme.value === DomainTheme.ThemeDark
)

let running: ViewTransition | null = null

/** Applies theme CSS classes to root HTML element. */
function paint(dark: boolean): void {
  document.documentElement.classList.toggle('dark', dark)
  document.documentElement.classList.toggle('light', !dark)
}

/** Applies theme changes using View Transitions API when available. */
function apply(dark: boolean, animate: boolean): void {
  const start = (document as TransitionDocument).startViewTransition?.bind(document)
  if (!animate || !start) {
    paint(dark)
    return
  }

  running?.skipTransition()
  running = start((): void => paint(dark))
}

/** Updates local theme state and returns whether visual theme mode shifted. */
function choose(next: Theme): boolean {
  const before = isDark.value
  theme.value = next
  return isDark.value !== before
}

watch(systemIsDark, (dark) => {
  if (theme.value === DomainTheme.ThemeSystem) apply(dark, true)
})

// Sync theme changes emitted across windows
useWailsEvent('settings:theme', (ev) => {
  const next = (ev.data as { theme: Theme }).theme
  if (!next || next === theme.value) return
  if (choose(next)) apply(isDark.value, true)
})

const { settings, loadSettings, setTheme: saveTheme } = useSettings()

watch(() => settings.value?.theme, (stored) => {
  if (stored && stored !== theme.value) {
    theme.value = stored
    apply(isDark.value, false)
  }
})

void loadSettings()

apply(isDark.value, false)

/** Composable providing active theme state and smooth theme switching. */
export function useTheme() {
  return {
    theme,
    isDark,
    /** Sets theme preference and syncs choice with backend. */
    setTheme(next: Theme): void {
      const fading = choose(next)
      void saveTheme(next).finally(() => {
        if (fading) apply(isDark.value, true)
      })
    },
  }
}
