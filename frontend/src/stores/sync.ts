import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { SyncService } from '../../bindings/schemo/internal/transport/wails'
import type { Status } from '../../bindings/schemo/internal/usecase/sync'
import type { QueuedChange, SyncCounts, SyncSpace } from '../../bindings/schemo/internal/domain'
import { SyncState } from '../../bindings/schemo/internal/domain'
import { formatAgo, t } from '../i18n'

export const useSyncStore = defineStore('sync', () => {
  const status = ref<Status | null>(null)

  const spaces = computed(() => status.value?.spaces ?? [])
  const contentAt = computed(() => status.value?.contentAt ?? 0)
  const historyAt = computed(() => status.value?.historyAt ?? 0)
  const linked = computed(() => spaces.value.filter((one) => one.remoteId !== ''))
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
    return spaces.value.flatMap((space) =>
      (queued[space.id] ?? []).map((change: QueuedChange) => ({ space, change }))
    )
  })

  // Every call answers with the whole status; Go also pushes it on its own (see useSyncEvents).
  async function take(call: Promise<Status>) {
    status.value = await call
  }

  return {
    status,
    spaces,
    contentAt,
    historyAt,
    linked,
    counts,
    waitingCount,
    waiting,
    set: (next: Status) => (status.value = next),
    load: () => take(SyncService.Status()),
    wake: () => SyncService.Wake(),
    runRound: () => take(SyncService.RunRound()),
    pause: () => take(SyncService.Pause()),
    resume: () => take(SyncService.Resume()),
    resync: (spaceID: string) => take(SyncService.Resync(spaceID)),
  }
})

export function syncingOff(spaces: SyncSpace[]): boolean {
  const linked = spaces.filter((one) => one.remoteId !== '')
  return linked.length > 0 && linked.every((one) => one.state === SyncState.SyncPaused)
}

export function syncLine(spaces: SyncSpace[], activeId: string, waiting = 0): string {
  const linked = spaces.filter((one) => one.remoteId !== '')
  const state = ((): string => {
    if (!linked.length) return t('sync.local')

    const held = linked.find((one) => one.id === activeId) ?? linked[0]
    if (held.state === SyncState.SyncPaused) return t('sync.paused')
    return held.syncedAt ? t('sync.synced', { when: formatAgo(held.syncedAt) }) : t('sync.local')
  })()

  if (!waiting) return state
  return [state, t('sync.notSent', { n: waiting })].join(' · ')
}