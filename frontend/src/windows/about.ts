import '@wailsio/runtime'

import { createApp } from 'vue'
import AboutWindow from './AboutWindow.vue'
import '../style.css'
import { loadCatalogue, resolveLocale, setLocale } from '../i18n'
import { applyLook, lookFromUrl, markPlatform } from '../lib/windowLook'

// The window is a mark, a name and a release, so it boots without the settings, the stores and the
// events the other windows share: they are most of what a window costs to open.
async function mount(): Promise<void> {
  markPlatform()
  applyLook(lookFromUrl(location.search))

  // The page's own script has already settled the language and hung it on the document.
  const locale = resolveLocale([document.documentElement.lang])
  await loadCatalogue(locale)
  setLocale(locale)
  document.documentElement.lang = locale

  createApp(AboutWindow).mount('#app')
}

void mount()
