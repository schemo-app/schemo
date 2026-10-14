<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { SystemService } from '../../../bindings/schemo/internal/transport/wails'
import { useTabsStore } from '../../stores/tabs'
import { useRecordsStore } from '../../stores/records'
import { useWorkspacesStore, workspaceName } from '../../stores/workspaces'
import { useSyncStore } from '../../stores/sync'
import { useMcp } from '../../composables/useMcp'
import { useMessages } from '../../i18n'
import Icon from '../ui/Icon.vue'

// The bar along the foot: what is being captured, how the space is doing and what the MCP server is
// up to. Everything here is a reading of a store or of Go's status.
const props = defineProps<{ updateVersion?: string }>()
defineEmits<{ 'open-update': [] }>()

const { t } = useMessages()
const tabs = useTabsStore()
const records = useRecordsStore()
const workspaces = useWorkspacesStore()
const sync = useSyncStore()

const captureTabs = computed(() => records.capture.tabList.length)
const recText = computed(() => {
  if (!records.capture.connected) return t('status.captureIdle')
  if (records.capture.recording) {
    return t('status.captureRecording', { tabs: t('status.captureTabs', captureTabs.value) })
  }
  return t('status.capturePaused')
})

const spaceName = computed(() =>
  workspaces.active ? workspaceName(workspaces.active) : t('workspaces.personal')
)
// A space that reached the server is synced; one that never left this machine says so.
const synced = computed(() => {
  const space = sync.spaces.find((one) => one.id === workspaces.activeId)
  return Boolean(space?.remoteId)
})
const syncText = computed(() =>
  synced.value
    ? t('status.syncSynced', { name: spaceName.value })
    : t('status.syncLocal', { name: spaceName.value })
)

// The composable holds the state and follows the server's own event, so a switch thrown in the
// settings window reaches this bar; the mount reads a server that is already listening.
const { running: mcpRunning, agents: mcpAgents, loadMCP } = useMcp()

onMounted(() => void loadMCP())

const mcpText = computed(() => {
  if (!mcpRunning.value) return t('status.mcpOff')
  return t('status.mcpAgents', { agents: t('status.mcpAgentCount', mcpAgents.value.length) })
})

function openMcp() {
  SystemService.ShowSettings('mcp')
}
</script>

<template>
  <footer class="status-bar">
    <button class="status-btn" :title="t('sidebar.capture')" @click="tabs.openCapture()">
      <span class="status-dot" :class="records.capture.recording ? 'dot-rec' : 'dot-idle'"></span>
      <span class="cap-line">{{ recText }}</span>
    </button>
    <button
      class="status-btn status-icon-btn"
      :title="records.capture.recording ? t('status.pauseCapture') : t('status.resumeCapture')"
      @click="records.toggleCapture()"
    >
      <Icon v-if="records.capture.recording" name="Pause" :size="10" />
      <Icon v-else name="Play" :size="10" />
    </button>

    <span class="status-sep"></span>

    <button class="status-btn" :title="t('workspaces.title')" @click="workspaces.openSheet()">
      <span class="status-dot" :class="synced ? 'dot-ok' : 'dot-local'"></span>
      <span class="cap-line">{{ syncText }}</span>
    </button>

    <span class="status-sep"></span>

    <button class="status-btn" @click="openMcp()">
      <span class="status-dot" :class="mcpRunning ? 'dot-ok' : 'dot-local'"></span>
      <span class="cap-line">{{ mcpText }}</span>
    </button>

    <span class="status-spacer"></span>

    <button v-if="props.updateVersion" class="status-update" @click="$emit('open-update')">
      {{ t('status.updateAvailable', { version: props.updateVersion }) }}
    </button>

    <span class="status-note">{{ t('status.historyCount', { n: records.records.length }) }}</span>
  </footer>
</template>

<style scoped>
@reference "../../style.css";

.status-bar {
  @apply flex items-center flex-none;
  height: 1.75rem;
  gap: 0.25rem;
  padding: 0 0.5rem 0 0.75rem;
  background: var(--chrome);
  border-top: 1px solid var(--border);
  font-size: var(--text-12);
  color: var(--text-secondary);
  white-space: nowrap;
  overflow: hidden;
  z-index: 40;
}

.status-btn {
  @apply flex items-center gap-1.5 flex-none;
  height: 1.375rem;
  padding: 0 0.375rem;
  border: 0;
  border-radius: 0.3125rem;
  background: transparent;
  color: inherit;
  font: inherit;
  cursor: default;
}

.status-btn:hover {
  background: var(--bg-hover);
  color: var(--text);
}

.status-icon-btn {
  @apply flex items-center justify-center flex-none;
  width: 1.5rem;
  height: 1.375rem;
  border: 0;
  border-radius: 0.3125rem;
  background: transparent;
  color: var(--text-tertiary);
  cursor: default;
}

.status-icon-btn:hover {
  background: var(--bg-hover);
  color: var(--text);
}

.status-dot {
  @apply flex-none;
  width: 0.4375rem;
  height: 0.4375rem;
  border-radius: 999px;
}

.dot-rec {
  background: var(--red);
  box-shadow: 0 0 0 0.25rem rgb(200 64 42 / 18%);
}

.dot-idle {
  background: var(--text-tertiary);
}

.dot-ok {
  background: var(--green);
}

.dot-local {
  background: var(--text-tertiary);
}

.status-sep {
  @apply flex-none;
  width: 1px;
  height: 0.75rem;
  background: var(--border);
  margin: 0 0.375rem;
}

.status-spacer {
  @apply flex-1;
}

.status-note {
  @apply flex-none;
  padding: 0 0.5rem;
  color: var(--text-tertiary);
}

.status-update {
  @apply flex-none;
  height: 1.375rem;
  padding: 0 0.5rem;
  border: 0;
  border-radius: 0.3125rem;
  background: transparent;
  color: var(--accent);
  font: inherit;
  cursor: default;
}

.status-update:hover {
  background: var(--accent-soft);
}
</style>
