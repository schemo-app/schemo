<script setup lang="ts">
import { computed } from 'vue'
import { RecordSource, TabKind } from '../../../bindings/schemo/internal/domain'
import { useTabsStore } from '../../stores/tabs'
import { t } from '../../i18n'
import { useCommands } from '../../composables/useCommands'
import HistoryPage from '../history/HistoryPage.vue'
import WelcomePage from '../onboarding/WelcomePage.vue'
import CapturePage from '../history/CapturePage.vue'
import CollectionPage from '../collections/CollectionPage.vue'
import PublicationsPage from '../cloud/PublicationsPage.vue'
import RequestBuilder from '../request/RequestBuilder.vue'
import BlankRequest from '../request/BlankRequest.vue'
import CapturedRequestBar from '../response/CapturedRequestBar.vue'
import ResponseViewer from '../response/ResponseViewer.vue'
import Spinner from '../ui/Spinner.vue'
import { focusUrlField } from '../../lib/intents'

const tabs = useTabsStore()

const { hint } = useCommands()

const awaiting = computed(() => {
  const tab = tabs.activeTab
  if (tab?.kind !== TabKind.TabKindRequest) return false
  const instance = tabs.activeInstance
  if (!instance) return true
  if (tabs.pane?.record) return false
  return instance.reading.value
})

const responseSource = computed<'request' | 'browser' | 'collection'>(() => {
  const instance = tabs.activeInstance
  if (tabs.activeKind === TabKind.TabKindRequest && instance?.kind === TabKind.TabKindRequest) {
    if (instance.node.value) return 'collection'
  }
  const record = tabs.pane?.record
  if (record && record.source === RecordSource.SourceBrowser) return 'browser'
  return 'request'
})

const capturedRecord = computed(() =>
  responseSource.value === 'browser' ? (tabs.pane?.record ?? null) : null
)

async function openCapturedInRequest() {
  const record = capturedRecord.value
  if (!record) return
  await tabs.editRecord(record.id)
  focusUrlField()
}
</script>

<template>
  <div class="work-area">
    <WelcomePage v-if="tabs.activeKind === TabKind.TabKindWelcome" />
    <HistoryPage v-else-if="tabs.activeKind === TabKind.TabKindHistory" />
    <CapturePage v-else-if="tabs.activeKind === TabKind.TabKindCapture" />
    <CollectionPage v-else-if="tabs.activeKind === TabKind.TabKindCollections" :key="tabs.activeId" />
    <PublicationsPage v-else-if="tabs.activeKind === TabKind.TabKindPubs" />
    <template v-else-if="tabs.activeKind === TabKind.TabKindRequest">
      <CapturedRequestBar
        v-if="capturedRecord"
        :record="capturedRecord"
        :can-open="true"
        @open-in-request="openCapturedInRequest"
      />
      <RequestBuilder
        v-else-if="tabs.pane && (!awaiting || tabs.pane.draftId)"
        :key="tabs.activeTab?.draftId || tabs.activeId"
        :source="tabs.pane"
      />
      <BlankRequest v-if="tabs.pane?.blank && !awaiting" :source="tabs.pane" />
      <template v-else-if="tabs.pane?.record">
        <ResponseViewer :record="tabs.pane.record" :source="responseSource" :pane="tabs.pane" />
      </template>
      <div
        v-else-if="!tabs.pane || tabs.pane.blank || tabs.pane.loading || awaiting"
        class="empty"
      >
        <Spinner class="text-accent" />
      </div>
      <div v-else class="resp-hint">{{ t('response.hintSend') }}</div>
    </template>
    <div v-else class="work-empty">
      <span>{{ t('tabs.empty.title') }}</span>
      <div class="work-empty-actions">
        <button type="button" class="work-empty-start" @click="tabs.newTab()">
          {{ t('tabs.empty.newTab') }} · {{ hint('newTab') }}
        </button>
        <button type="button" class="work-empty-history" @click="tabs.openHistory()">
          {{ t('tabs.empty.history') }} · {{ hint('history') }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.work-area {
  @apply flex-1 min-w-0 min-h-0 flex flex-col bg-bg;
  position: relative;
  z-index: 5;
  --wails-draggable: no-drag;
}

.work-area > :last-child {
  @apply flex-1 min-h-0;
}

.work-area > .request-block:has(.req-pane-wrap) {
  border-bottom: 1px solid var(--border);
}

.work-empty {
  @apply flex-1 flex flex-col items-center justify-center;
  gap: 0.875rem;
  color: var(--text-secondary);
}

.work-empty-actions {
  @apply flex;
  gap: 0.5rem;
}

.work-empty-actions button {
  @apply inline-flex items-center justify-center cursor-pointer whitespace-nowrap;
  height: 2rem;
  padding: 0 0.875rem;
  border: 0;
  border-radius: 0.5rem;
  font-family: inherit;
  font-size: inherit;
}

.work-empty-start {
  background: var(--accent);
  color: var(--accent-text);
  font-weight: 500;
}

.work-empty-history {
  background: var(--chip);
  color: var(--text);
}

.resp-hint {
  @apply text-13 text-text-tertiary;
  padding: 0.875rem 1.25rem;
}
</style>
