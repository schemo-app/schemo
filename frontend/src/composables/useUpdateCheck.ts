import { onMounted, ref } from 'vue'
import { createSharedComposable } from '@vueuse/core'
import { UpdateService } from '../../bindings/schemo/internal/transport/wails'
import type { Status as UpdateStatus } from '../../bindings/schemo/internal/usecase/update'
import { describeFailure, t as tr } from '../i18n'
import { useWailsEvent } from './events/useWailsEvent'

/** Current stage of the application update lifecycle. */
export type UpdatePhase = 'idle' | 'checking' | 'uptodate' | 'available' | 'installing' | 'error'

// One state per window however many views draw it — the settings window draws it twice — and its
// listeners live as long as any of them does.
const useUpdateState = createSharedComposable(() => {
  const phase = ref<UpdatePhase>('idle')
  const latest = ref('')
  const notes = ref<string[]>([])
  const checkedAt = ref(0)
  const error = ref('')
  const development = ref(false)

  /** Synchronizes local state with backend update information. */
  function apply(info: UpdateStatus | null): void {
    if (!info) return
    latest.value = info.latest ?? ''
    notes.value = info.notes ?? []
    checkedAt.value = info.checkedAt ?? 0
    development.value = info.development ?? false

    if (phase.value === 'installing') return
    if (!info.checkedAt) {
      phase.value = 'idle'
      return
    }
    phase.value = info.available ? 'available' : 'uptodate'
  }

  /** Triggers an asynchronous update check via backend service. */
  async function check(): Promise<void> {
    if (phase.value === 'checking' || phase.value === 'installing') return
    phase.value = 'checking'
    error.value = ''
    try {
      apply(await UpdateService.Check())
    } catch {
      error.value = tr('errors.updateCheckFailed')
      phase.value = 'error'
    }
  }

  /** Initiates application binary update installation process. */
  async function install(): Promise<void> {
    if (!latest.value || phase.value === 'installing') return
    phase.value = 'installing'
    error.value = ''
    try {
      await UpdateService.Install(latest.value)
    } catch (e) {
      error.value = tr('errors.updateFailed', { error: describeFailure(e) })
      phase.value = 'error'
    }
  }

  /** Skips the currently available release version. */
  async function skip(): Promise<void> {
    try {
      apply(await UpdateService.Skip())
    } catch (e) {
      error.value = tr('errors.updateFailed', { error: describeFailure(e) })
      phase.value = 'error'
    }
  }

  /** Opens standalone update management window. */
  function openWindow(): void {
    UpdateService.ShowWindow()
  }

  /** Runs a check the menu or the window's key parked for the updates page: the page takes it as
   *  it mounts, or the moment it hears `update-check` while already open. */
  async function checkIfRequested(): Promise<void> {
    try {
      if (await UpdateService.TakeRequest()) void check()
    } catch {
      // A failed take leaves the page to its own button: nothing to report about it.
    }
  }

  useWailsEvent('update-changed', (ev) => apply(ev.data as UpdateStatus))
  useWailsEvent('wails:updater:download-started', () => {
    phase.value = 'installing'
    error.value = ''
  })
  useWailsEvent('wails:updater:error', (event) => {
    if (event.data.stage === 'check') return
    error.value = tr('errors.updateFailed', { error: event.data.message })
    phase.value = 'error'
  })

  return {
    phase,
    latest,
    notes,
    checkedAt,
    error,
    development,
    apply,
    check,
    install,
    skip,
    openWindow,
    checkIfRequested,
  }
})

/** The update state every window draws, refreshed from Go as the view mounts. */
export function useUpdateCheck() {
  const state = useUpdateState()
  onMounted(async () => {
    try {
      state.apply(await UpdateService.Status())
    } catch {
      // No backend answer: the line stays on "no check yet".
    }
  })
  const { phase, latest, notes, checkedAt, error, development, check, install, skip, openWindow, checkIfRequested } = state
  return { phase, latest, notes, checkedAt, error, development, check, install, skip, openWindow, checkIfRequested }
}
