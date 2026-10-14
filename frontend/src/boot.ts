import { createApp, type Component } from 'vue'
import { createPinia } from 'pinia'
import { i18n } from './i18n'
import { useTheme } from './composables/useTheme'
import { useAppearance } from './composables/useAppearance'
import { useZoom } from './composables/useZoom'
import { startLocale } from './composables/useLocale'

// The platform marker the stylesheet reads: only macOS claims it, and only its 1× text uses it — the
// stroke there is what reads as the weight Windows hints for free.
function markPlatform(): void {
  if (navigator.userAgent.includes('Mac')) {
    document.documentElement.dataset.mac = '1'
  }
}

export async function mountWindow(root: Component): Promise<void> {
  markPlatform()
  useTheme()
  useAppearance()
  useZoom()
  await startLocale()

  createApp(root).use(i18n).use(createPinia()).mount('#app')
}