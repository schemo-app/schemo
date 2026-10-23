import { computed, ref } from 'vue'
// The service's own file, not the barrel: a window that shows a name and a version has no business
// loading the twenty-odd services it would be handed with the rest of them.
import * as System from '../../bindings/schemo/internal/app/system'
import { formatVersion } from '../lib/format'

const name = ref('')
const version = ref('')
const build = ref('')
const versionLabel = computed(() =>
  [formatVersion(version.value), build.value && `(${build.value})`].filter(Boolean).join(' ')
)
let loading: Promise<unknown> | null = null

/** Who the app is: its name, version and build, read once for the window however many views draw
 *  them. The label is the version and build together, as the about and update windows write it. */
export function useAppInfo() {
  // Cosmetic fields: a failed call leaves them blank, which is what a dev build shows anyway.
  loading ??= System.Info()
    .then((info) => {
      name.value = info.name
      version.value = info.version
      build.value = info.build
    })
    .catch(() => {})
  return { name, version, build, versionLabel }
}
