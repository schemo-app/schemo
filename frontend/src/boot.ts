import { createApp, type Component } from 'vue'
import { createPinia } from 'pinia'
import { useTheme } from './composables/useTheme'
import { useAppearance } from './composables/useAppearance'
import { useZoom } from './composables/useZoom'
import { startLocale } from './composables/useLocale'
import { markPlatform } from './lib/windowLook'

export async function mountWindow(root: Component): Promise<void> {
  markPlatform()
  useTheme()
  useAppearance()
  useZoom()
  await startLocale()

  createApp(root).use(createPinia()).mount('#app')
}
