import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { Sync, type SyncStatus } from '../../bindings/schemo/internal/cloud'
import type { QueuedChange, SyncCounts, SyncWorkspace } from '../../bindings/schemo/internal/domain'
import { SyncState } from '../../bindings/schemo/internal/domain'
import { formatAgo, t } from '../i18n'

export const useSyncStore = defineStore('sync', () => {
  const status = ref<SyncStatus | null>(null)

  const workspaces = computed(() => status.value?.workspaces ?? [])
  const contentAt = computed(() => status.value?.contentAt ?? 0)
  const historyAt = computed(() => status.value?.historyAt ?? 0)
  const linked = computed(() => workspaces.value.filter((one) => one.remoteId !== ''))
  const counts = computed<Record<string, SyncCounts | undefined>>(() => status.value?.counts ?? {})

  // How many changes are owed, from the counts: the list below describes only the oldest of them.
  const waitingCount = computed(() =>
    Object.values(counts.value).reduce(
      (sum, one) => sum + (one ? one.pending + one.sending + one.failed + one.parked : 0),
      0
    )
  )

  const waiting = computed(() => {
    const queued = status.value?.waiting ?? {}
    return workspaces.value.flatMap((workspace) =>
      (queued[workspace.id] ?? []).map((change: QueuedChange) => ({ workspace, change }))
    )
  })

  // Every call answers with the whole status; Go also pushes it on its own (see useSyncEvents).
  async function take(call: Promise<SyncStatus>) {
    status.value = await call
  }

  return {
    status,
    workspaces,
    contentAt,
    historyAt,
    linked,
    counts,
    waitingCount,
    waiting,
    set: (next: SyncStatus) => (status.value = next),
    load: () => take(Sync.Status()),
    wake: () => Sync.Wake(),
    runRound: () => take(Sync.RunRound()),
    pause: () => take(Sync.Pause()),
    resume: () => take(Sync.Resume()),
    resync: (workspaceID: string) => take(Sync.Resync(workspaceID)),
  }
})

export function syncingOff(workspaces: SyncWorkspace[]): boolean {
  const linked = workspaces.filter((one) => one.remoteId !== '')
  return linked.length > 0 && linked.every((one) => one.state === SyncState.SyncPaused)
}

export function syncLine(workspaces: SyncWorkspace[], activeId: string, waiting = 0): string {
  const linked = workspaces.filter((one) => one.remoteId !== '')
  const state = ((): string => {
    if (!linked.length) return t('sync.local')

    const held = linked.find((one) => one.id === activeId) ?? linked[0]
    if (held.state === SyncState.SyncPaused) return t('sync.paused')
    return held.syncedAt ? t('sync.synced', { when: formatAgo(held.syncedAt) }) : t('sync.local')
  })()

  if (!waiting) return state
  return [state, t('sync.notSent', { n: waiting })].join(' · ')
}