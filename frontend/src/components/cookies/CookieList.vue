<script setup lang="ts">
import { computed } from 'vue'
import ManagerRail from '../ui/ManagerRail.vue'
import { useCookiesStore } from '../../stores/cookies'
import { useWorkspacesStore } from '../../stores/workspaces'
import { reachLabel } from '../../lib/cookies'
import { useMessages } from '../../i18n'

// Which domain of the jar the pane beside it is about, and the one way to start a cookie. The dot
// is the accent on the domain the last response wrote to and quiet on the rest; the lock marks a
// domain whose responses may not write.
const emit = defineEmits<{ (e: 'select', host: string): void; (e: 'create'): void }>()

const jar = useCookiesStore()
const workspaces = useWorkspacesStore()
const { t } = useMessages()

const freshest = computed(() => {
  let at = 0
  let host = ''
  for (const level of jar.domains) {
    if (level.writtenAt > at) {
      at = level.writtenAt
      host = level.domain
    }
  }
  return host
})

const rows = computed(() =>
  jar.domains.map((level) => {
    const fresh = level.domain === freshest.value
    return {
      id: level.domain as string | null,
      name: reachLabel(level),
      meta: '',
      dot: fresh ? 'var(--accent)' : 'var(--text-tertiary)',
      hint: fresh ? t('cookies.fresh') : undefined,
      count: level.cookies?.length ?? 0,
      active: level.domain === jar.domain,
      readonly: level.blocked,
    }
  })
)

// Every row of this rail is a domain, so the rail's own "no scope" case never comes up here.
function select(id: string | null) {
  if (id !== null) emit('select', id)
}
</script>

<template>
  <ManagerRail
    :rows
    :new-label="t('cookies.newCookie')"
    :can-create="workspaces.canWrite"
    @select="select"
    @create="emit('create')"
  />
</template>
