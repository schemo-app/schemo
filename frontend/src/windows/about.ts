import '@wailsio/runtime'

import { createApp } from 'vue'
import About from './About.vue'
import '../style.css'
import { i18n, loadCatalogue, resolveLocale } from '../i18n'
import { applyLook, lookFromUrl } from '../lib/windowLook'

// The window is a mark, a name and a release, so it boots without the settings, the stores and the
// events the other windows share: they are most of what a window costs to open.
async function mount(): Promise<void> {
  applyLook(lookFromUrl(location.search))

  // The page's own script has already settled the language and hung it on the document.
  const locale = resolveLocale([document.documentElement.lang])
  await loadCatalogue(locale)
  i18n.global.locale.value = locale
  document.documentElement.lang = locale

  createApp(About).use(i18n).mount('#app')
}

void mount()
