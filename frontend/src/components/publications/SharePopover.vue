<script setup lang="ts">
import { computed } from 'vue'
import { Browser } from '@wailsio/runtime'
import Icon from '../ui/Icon.vue'
import { Popover, PopoverTrigger, PopoverContent } from '../ui/popover'
import PublicationFields from './PublicationFields.vue'
import PublicationPassword from './PublicationPassword.vue'
import { usePublicationsStore, type Visibility } from '../../stores/publications'
import { useEnvironmentsStore } from '../../stores/environments'
import { useTabsStore } from '../../stores/tabs'
import { usePlan } from '../../composables/usePlan'
import { useToast } from '../../composables/useToast'
import { copyToClipboard } from '../../lib/clipboard'
import { FEATURES, featureRefusal } from '../../lib/features'
import { passwordFits, problemText } from '../../lib/publications'
import { PASSWORD } from '../../lib/layout'
import { useMessages } from '../../i18n'

const props = defineProps<{ collectionId: string }>()
const { t, te } = useMessages()
const store = usePublicationsStore()
const env = useEnvironmentsStore()
const tabs = useTabsStore()
const toast = useToast()
const { includes } = usePlan()

const open = computed({
  get: () => store.open && store.collection === props.collectionId,
  set: (value: boolean) => value ? store.openShare(props.collectionId) : store.closeShare(),
})
const mark = computed(() => store.markOf(props.collectionId))
const live = computed(() => mark.value?.page?.status === 'active' && !mark.value.page.pendingUnpublish)
const behind = computed(() => live.value && mark.value?.behind === true)
const label = computed(() => {
  if (!live.value) return t('publications.share')
  if (!behind.value) return t('publications.published')
  const count = mark.value?.changes?.length ?? 0
  return t('publications.changes') + (count ? ' · ' + count : '')
})
const publication = computed(() => store.publication)
const readOnly = computed(() => !!publication.value && !publication.value.canManage)
const disabled = computed(() => readOnly.value || store.busy || store.reading)
const passwordOk = computed(() => store.visibility !== 'password' || passwordFits(store.password) ||
  (!store.password && publication.value?.visibility === 'password'))
const canPublish = computed(() => includes(FEATURES.shareLinks) && !disabled.value && passwordOk.value)
const pending = computed(() => store.behind || store.visibility !== publication.value?.visibility)
const envLabel = computed(() => store.environments.find(choice => choice.id === store.environment)?.name ??
  env.activeEnvironment?.name ?? t('publications.envNone'))
const lede = computed(() => t(store.visibility === 'public' ? 'publications.draftPublic' :
  store.visibility === 'password' ? 'publications.draftPassword' : 'publications.draftUnlisted'))
const hidden = computed(() => store.report?.hidden ?? [])
const problems = computed(() => (store.report?.problems ?? []).map(problemText).filter(Boolean))
const refusal = computed(() => includes(FEATURES.shareLinks) ? '' : featureRefusal(t, te, FEATURES.shareLinks))
const passwordRange = { min: PASSWORD.min, max: PASSWORD.max }

function pickVisibility(value: Visibility) {
  store.visibility = value
}
async function publish() {
  if (canPublish.value) await store.publish(true)
}
function openPage() {
  if (publication.value?.url) void Browser.OpenURL(publication.value.url)
}
async function copy() {
  const url = publication.value?.url
  if (url && await copyToClipboard(url)) toast.show(t('publications.copied'))
}
function settings() {
  store.selectedCollection = props.collectionId
  store.closeShare()
  void tabs.openPublications()
}
function selectAddress(event: FocusEvent) {
  (event.target as HTMLInputElement).select()
}
</script>

<template>
  <Popover v-model:open="open">
    <PopoverTrigger as-child>
      <button type="button" class="share-btn" :class="{ open }" :title="mark?.page?.url || t('publications.shareTitle')">
        <span v-if="live" class="share-dot" :class="behind ? 'behind' : 'live'"></span>
        <Icon v-else name="Radio" :size="15" />
        <span class="cap-line">{{ label }}</span>
      </button>
    </PopoverTrigger>
    <PopoverContent class="share-pop" align="start" :side-offset="8" @open-auto-focus.prevent>
      <template v-if="!publication">
        <div class="pop-head">
          <span class="pop-title">{{ t('publications.headline') }}</span>
          <span class="pop-lede">{{ lede }}</span>
        </div>
        <div class="draft-fields">
          <PublicationFields :visibility="store.visibility" :environment="store.environment"
            :environment-label="envLabel" :environments="store.environments" :disabled="disabled"
            @visibility="pickVisibility" @environment="store.choose" />
          <PublicationPassword v-if="store.visibility === 'password'" v-model="store.password" :disabled="disabled"
            :placeholder="t('publications.passwordPlaceholder', passwordRange)" />
          <span v-if="store.password && !passwordOk" class="pass-note">{{ t('publications.passwordLength', passwordRange) }}</span>
        </div>
        <div class="pop-frame">
          <div class="frame-line">
            <Icon name="Shield" :size="15" class="text-green" />
            <span v-if="hidden.length">{{ t('publications.hiddenHead') }} <code>{{ hidden.map(item => item.name).join(', ') }}</code></span>
            <span v-else>{{ t('publications.hiddenNothing') }}</span>
          </div>
          <div v-for="(problem, index) in problems" :key="index" class="frame-line">
            <Icon name="TriangleAlert" :size="15" class="text-orange" /><span>{{ problem }}</span>
          </div>
          <div v-if="refusal" class="frame-line"><Icon name="TriangleAlert" :size="15" class="text-orange" /><span>{{ refusal }}</span></div>
        </div>
        <div class="pop-go">
          <button type="button" class="primary" :disabled="!canPublish" @click="publish">{{ t(store.busy ? 'publications.publishing' : 'publications.publish') }}</button>
        </div>
      </template>
      <template v-else>
        <div class="live-head"><span class="pop-title">{{ t('publications.published') }}</span></div>
        <div class="live-address">
          <div class="address-field">
            <input :value="publication.url" readonly :aria-label="t('publications.publicationUrl')" @focus="selectAddress" />
            <button type="button" class="icon-button" :title="t('publications.copyLink')" :aria-label="t('publications.copyLink')" @click="copy"><Icon name="Copy" :size="14" /></button>
          </div>
        </div>
        <div class="quick-actions">
          <button type="button" class="action-row" @click="openPage"><Icon name="ExternalLink" :size="15" /><span>{{ t('publications.openPage') }}</span></button>
          <button v-if="pending && !readOnly" type="button" class="action-row update" :disabled="!canPublish" @click="publish">
            <Icon name="ArrowUp" :size="15" /><span>{{ t(store.busy ? 'publications.updating' : 'publications.updatePage') }}</span><span class="pending-dot"></span>
          </button>
        </div>
        <div class="divider"></div>
        <div class="quick-fields">
          <PublicationFields :visibility="store.visibility" :environment="store.environment" :environment-label="envLabel"
            :environments="store.environments" :disabled="disabled" access-only @visibility="pickVisibility" @environment="store.choose" />
          <div v-if="store.visibility === 'password'" class="quick-password">
            <PublicationPassword :key="store.collection" v-model="store.password" tools save :disabled="disabled"
              :placeholder="t('publications.newPassword')" :save-disabled="!canPublish || !passwordFits(store.password)" @save="publish" />
            <span v-if="store.password && !passwordOk" class="pass-note">{{ t('publications.passwordLength', passwordRange) }}</span>
          </div>
        </div>
        <div class="divider"></div>
        <div class="quick-actions last">
          <button type="button" class="action-row" @click="settings"><Icon name="Settings2" :size="15" /><span>{{ t('publications.publicationSettings') }}</span></button>
        </div>
      </template>
    </PopoverContent>
  </Popover>
</template>

<style scoped>
@reference "../../style.css";

.share-btn {
  @apply flex items-center justify-center flex-none cursor-pointer border-0 whitespace-nowrap text-text;
  height: 2rem;
  padding: 0 0.6875rem;
  gap: 0.4375rem;
  border-radius: 0.5rem;
  background: transparent;
  font: inherit;
}

.share-btn:hover,
.share-btn.open {
  background: var(--bg-hover);
}

.share-dot {
  @apply flex-none rounded-full;
  width: 0.4375rem;
  height: 0.4375rem;
}

.share-dot.live {
  background: var(--green);
}

.share-dot.behind,
.pending-dot {
  background: var(--orange);
}

:global(.popover.share-pop) {
  @apply flex flex-col;
  width: 372px;
  max-width: calc(100vw - 16px);
  padding: 0;
  font-size: var(--text-13);
  font-weight: 400;
  text-align: left;
  line-height: normal;
  border: 0;
  border-radius: 0.875rem;
  background: var(--pop);
  backdrop-filter: var(--blur-overlay);
  box-shadow: var(--popshadow);
}

.pop-head {
  @apply flex flex-col;
  padding: 1rem 1rem 0.375rem;
  gap: 0.3125rem;
}

.pop-title {
  font-size: var(--text-14);
  font-weight: 600;
}

.pop-lede {
  @apply text-text-secondary;
  line-height: 1.45;
  text-wrap: pretty;
}

.draft-fields {
  padding: 0.375rem 1rem 0.625rem;
}

.pass-note {
  @apply text-orange text-11-5;
}

.pop-frame {
  @apply flex flex-col;
  margin: 0 1rem;
  padding: 0.625rem 0.75rem;
  gap: 0.5625rem;
  border-radius: 0.625rem;
  background: var(--chip);
}

.frame-line {
  @apply flex items-start;
  gap: 0.5rem;
  font-size: var(--text-12-5);
  line-height: 1.45;
  overflow-wrap: anywhere;
}

.frame-line :deep(svg) {
  flex: none;
  margin-top: 1px;
}

.frame-line code {
  font-family: var(--mono);
  font-size: var(--text-12);
}

.pop-go {
  padding: 0.875rem 1rem 1rem;
}

.primary {
  @apply w-full border-0 cursor-pointer font-semibold;
  height: 2.125rem;
  border-radius: 0.5rem;
  background: var(--accent);
  color: var(--accent-text);
  font: inherit;
  font-weight: 600;
}

.live-head {
  padding: 0.875rem 1rem 0.625rem;
}

.live-address {
  padding: 0 1rem 0.625rem;
}

.address-field {
  @apply flex items-center;
  height: 2.125rem;
  padding: 0 0.25rem 0 0.625rem;
  gap: 0.25rem;
  border-radius: 0.5rem;
  background: var(--bg-inset);
  box-shadow: inset 0 0 0 1px var(--border-strong);
}

.address-field input {
  @apply flex-1 min-w-0 border-0 outline-none bg-transparent text-text;
  font-family: var(--mono);
  font-size: var(--text-12-5);
}

.icon-button {
  @apply flex items-center justify-center flex-none border-0 bg-transparent text-text-secondary cursor-pointer;
  width: 1.625rem;
  height: 1.625rem;
  border-radius: 0.375rem;
}

.icon-button:hover,
.action-row:hover:not(:disabled) {
  background: var(--bg-hover);
}

.quick-actions,
.quick-fields {
  padding: 0 0.375rem;
}

.quick-actions.last {
  padding-bottom: 0.375rem;
}

.action-row {
  @apply flex items-center w-full border-0 bg-transparent text-text cursor-pointer text-left;
  height: 2.125rem;
  padding: 0 0.625rem;
  border-radius: 0.4375rem;
  gap: 0.625rem;
  font: inherit;
}

.action-row :deep(svg) {
  @apply flex-none text-text-secondary;
  margin: 0 0.09375rem;
}

.action-row > span:first-of-type {
  flex: 1;
}

.action-row.update,
.action-row.update :deep(svg) {
  color: var(--accent);
  font-weight: 500;
}

.pending-dot {
  @apply flex-none;
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 0.25rem;
}

.divider {
  height: 1px;
  margin: 0.25rem 0.625rem;
  background: var(--border);
}

.quick-password {
  padding: 0 0.625rem 0.5rem;
}

button:disabled {
  @apply cursor-default opacity-45;
}
</style>
