import { computed, ref } from 'vue'
import { System } from '@wailsio/runtime'

export type Platform = 'darwin' | 'windows' | 'linux'

// The user agent answers first, and Go's own answer replaces it: the window reports the system it is
// really running on, and the two agree inside the engine that draws it.
function fromAgent(userAgent: string): Platform {
  if (/Windows/i.test(userAgent)) return 'windows'
  if (/Android|Linux|X11/i.test(userAgent)) return 'linux'
  return 'darwin'
}

const platform = ref<Platform>(fromAgent(navigator.userAgent))

System.Environment()
  .then((env) => {
    if (env.OS) platform.value = env.OS as Platform
  })
  .catch(() => {})

/** The running platform: which modifiers a shortcut wears, who draws the titlebar, and what a key is
 *  called — the four questions the window asks about the system under it. */
export function usePlatform() {
  const mac = () => platform.value === 'darwin'

  return {
    isMac: computed(mac),
    customTitlebar: computed(() => platform.value === 'windows' || platform.value === 'linux'),
    shortcut: (key: string) => (mac() ? `⌘${key}` : `Ctrl+${key}`),
    chord: (key: 'N' | 'D' | 'R') => key !== 'R' || mac(),
    keyLabel: (key: 'enter' | 'delete') =>
      mac() ? (key === 'enter' ? '↩' : '⌫') : key === 'enter' ? 'Enter' : 'Delete',
  }
}
