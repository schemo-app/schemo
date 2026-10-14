import { computed, watch } from 'vue'
import { useWailsEvent } from './events/useWailsEvent'
import { usePersistedChoice } from './usePersistedChoice'
import { useSettings } from './useSettings'
import { i18n, loadCatalogue, locales, systemLocale, t, type Locale } from '../i18n'
import { SystemService } from '../../bindings/schemo/internal/transport/wails'
import { acceleratorOf } from '../lib/commands'
import { useCommands } from './useCommands'

export type Language = 'system' | Locale
const LANGUAGES: Language[] = ['system', ...locales]

const language = usePersistedChoice<Language>(
  'schemo-lang',
  'lang',
  LANGUAGES,
  'system'
)
const systemLanguage = systemLocale()

/** Resolves a language choice to a concrete locale code. */
function localeOf(next: Language): Locale {
  return next === 'system' ? systemLanguage : next
}

const locale = computed<Locale>(() => localeOf(language.value))

/** Syncs localized string sets for system menus and auth pages via Wails bridge. */
function applyLanguage() {
  void SystemService.ApplyLanguage(
    {
      about: t('menu.about'),
      file: t('menu.file'),
      loadSample: t('menu.loadSample'),
      settings: t('menu.settingsItem'),
      checkUpdates: t('menu.checkUpdates'),
      closeWindow: t('menu.closeWindow'),
      // The system's own menu draws the accelerators, so what each command is bound to has to be
      // handed over rather than drawn: what a window resolves is not what the menu can read.
      settingsKey: acceleratorOf(combo('settings')),
      updateKey: acceleratorOf(combo('updates')),
    },
    {
      waiting: { title: t('signIn.waiting.title'), text: t('signIn.waiting.text') },
      done: { title: t('signIn.done.title'), text: t('signIn.done.text') },
      refused: { title: t('signIn.refused.title'), text: t('signIn.refused.text') },
      failed: t('signIn.failed'),
    },
  ).catch(() => {
    // Ignore bridge errors; menu items will retain current labels.
  })
}

let asked = 0

/** Loads the target locale catalog and updates global i18n state and HTML attributes. */
async function choose(next: Language) {
  const ticket = ++asked
  const wanted = localeOf(next)
  await loadCatalogue(wanted)
  if (ticket !== asked) return

  language.value = next
  i18n.global.locale.value = wanted
  document.documentElement.lang = wanted
  applyLanguage()
}

// Sync language changes across multiple application windows.
useWailsEvent('settings:language', (ev) => {
  const next = (ev.data as { language: Language }).language
  if (!LANGUAGES.includes(next) || next === language.value) return
  void choose(next)
})

/** Initializes the application locale on startup and applies persisted settings. */
export async function startLocale(): Promise<void> {
  await choose(language.value)

  // The settings window can rebind a combination and the system menu is drawn once: every window
  // hands them over again, the one owning the menu for all of them. Watching from here, with the
  // first catalogue in, is what keeps an early change from reading a word no catalogue holds.
  watch(() => settings.value?.shortcuts, () => applyLanguage(), { deep: true })

  void loadSettings().then(() => {
    const stored = settings.value?.language
    if (stored && LANGUAGES.includes(stored as Language) && stored !== language.value) void choose(stored as Language)
  })
}

const { settings, loadSettings, setLanguage: saveLanguage } = useSettings()
const { combo } = useCommands()

/** Composable for managing locale selection and application language state. */
export function useLocale() {
  return {
    language,
    locale,
    async setLanguage(next: Language): Promise<boolean> {
      const before = locale.value
      await choose(next)
      void saveLanguage(next).catch(() => {
        // Ignore persistence failure as UI is already updated.
      })
      return before !== locale.value
    },
  }
}
