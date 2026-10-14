<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Browser } from '@wailsio/runtime'
import Icon from '../ui/Icon.vue'
import { Popover, PopoverTrigger, PopoverContent } from '../ui/popover'
import { usePublicationsStore, type Visibility } from '../../stores/publications'
import { useEnvironmentsStore } from '../../stores/environments'
import { useTabsStore } from '../../stores/tabs'
import { usePlan } from '../../composables/usePlan'
import { useToast } from '../../composables/useToast'
import { copyToClipboard } from '../../lib/clipboard'
import { FEATURES, featureRefusal } from '../../lib/features'
import {
  changeNotes,
  changeTally,
  hiddenText,
  passwordFits,
  problemText,
  visibilityOptions,
} from '../../lib/publications'
import { methodInkClass } from '../../lib/format'
import { PASSWORD } from '../../lib/layout'
import { formatAgo, useMessages } from '../../i18n'

const props = defineProps<{ collectionId: string }>()

const { t, te } = useMessages()
const store = usePublicationsStore()
const env = useEnvironmentsStore()
const tabs = useTabsStore()
const toast = useToast()
const { includes } = usePlan()

const open = computed({
  get: () => store.open && store.collection === props.collectionId,
  set: (on: boolean) => {
    if (on) void store.openShare(props.collectionId)
    else store.closeShare()
  },
})

const mark = computed(() => store.markOf(props.collectionId))
const live = computed(() => mark.value?.page != null)
const behind = computed(() => live.value && mark.value?.behind === true)
const label = computed(() =>
  t(live.value ? 'publications.published' : 'publications.share')
)

const publication = computed(() => store.publication)

const inherited = computed(
  () => store.preview?.inheritedFrom ?? mark.value?.inheritedFrom ?? ''
)
const owner = computed(
  () => store.preview?.collectionId ?? mark.value?.page?.collectionId ?? ''
)
const ownerTitle = computed(() => t('publications.insidePage', { name: inherited.value }))
const buttonTitle = computed(() => mark.value?.page?.url || t('publications.shareTitle'))
const shareLinks = computed(() => includes(FEATURES.shareLinks))
const refusal = computed(() => (shareLinks.value ? '' : featureRefusal(t, te, FEATURES.shareLinks)))

const options = computed(() => visibilityOptions())
const visLabel = computed(
  () => options.value.find((option) => option.value === store.visibility)?.label ?? ''
)

const NOTE_ROWS = 3

const environments = computed(() => store.environments)
const envLabel = computed(() => {
  const chosen = environments.value.find((choice) => choice.id === store.environment)
  if (chosen) return chosen.name
  return env.activeEnvironment?.name ?? t('publications.envNone')
})

const menu = ref('')
watch(open, (on) => {
  if (!on) menu.value = ''
})

function pickVisibility(value: Visibility) {
  store.visibility = value
  menu.value = ''
}

const wantsPassword = computed(() => store.visibility === 'password')
const password = computed({
  get: () => store.password,
  set: (held: string) => {
    store.password = held
  },
})
const passwordOk = computed(() => !wantsPassword.value || passwordFits(store.password))
const passwordRange = computed(() => ({ min: PASSWORD.min, max: PASSWORD.max }))

function pickEnvironment(id: string) {
  menu.value = ''
  if (id !== store.environment) void store.choose(id)
}

const hidden = computed(() => store.report?.hidden ?? [])
const hiddenNames = computed(() => {
  const names = hidden.value.map((held) => held.name)
  return names.length ? names.join(', ') : t('publications.hiddenNothing')
})
const problems = computed(() => (store.report?.problems ?? []).map(problemText).filter(Boolean))

const canPublish = computed(
  () => shareLinks.value && !store.busy && !store.reading && passwordOk.value
)
const publishLabel = computed(() =>
  t(store.busy ? 'publications.publishing' : 'publications.publish')
)

// The block's own button. The address above it already carries the revision, so the next number
// would be the page's twice here; the page of publications, which has no address beside it, says it.
const blockLabel = computed(() => t(store.busy ? 'publications.updating' : 'publications.update'))

// A page already up is behind when the collection moved on, and it is about to change when the
// person picks another access — which the document does not carry, so no digest can say it.
const accessChanged = computed(
  () => publication.value !== null && store.visibility !== publication.value.visibility
)
const pending = computed(() => store.behind || accessChanged.value)

// The rows the block under the address draws, and the tally that stands in for them once there are
// more than the block holds: a list this long belongs to the page of publications, and the link at
// its foot is where a person reads it whole.
const notes = computed(() => changeNotes(store.behind ? store.changes : []))
const rows = computed(() => (notes.value.length <= NOTE_ROWS ? notes.value : []))
const tally = computed(() => (notes.value.length > NOTE_ROWS ? changeTally(store.changes) : []))
const pendingText = computed(() => {
  if (!store.behind) return t('publications.settingsChanged')
  if (notes.value.length === 0) return t('publications.changesPending')
  return t('publications.changesCounted', { n: notes.value.length })
})

const when = computed(() => formatAgo(publication.value?.updatedAt ?? 0))
const statusText = computed(() =>
  t('publications.status', { rev: publication.value?.revision ?? 0, when: when.value })
)

async function publish() {
  if (canPublish.value) await store.publish(true)
}

function openPage() {
  const url = publication.value?.url
  if (url) void Browser.OpenURL(url)
}

async function copy() {
  const url = publication.value?.url
  if (!url) return
  if (await copyToClipboard(url)) toast.show(t('publications.copied'))
}

function openAll() {
  store.closeShare()
  void tabs.openPublications()
}

// The page a folder belongs to is a collection in the tree, so the line naming it is a way there:
// what a person reads is where the page is edited, and the popover is closed behind them.
function openOwner() {
  if (!owner.value) return
  store.closeShare()
  void tabs.openCollection(owner.value)
}

function askUnpublish() {
  const id = props.collectionId
  store.closeShare()
  store.askUnpublish(id)
}
</script>

<template>
  <Popover v-model:open="open">
    <PopoverTrigger as-child>
      <button type="button" class="share-btn" :class="{ open }" :title="buttonTitle">
        <span v-if="live" class="share-dot" :class="behind ? 'behind' : 'live'"></span>
        <Icon v-else name="Radio" :size="15" :stroke-width="1.8" />
        <span class="cap-line">{{ label }}</span>
      </button>
    </PopoverTrigger>

    <PopoverContent class="share-pop" align="start" :side-offset="8" @open-auto-focus.prevent>
      <template v-if="!publication">
        <div class="pop-head">
          <span class="pop-title">{{ t('publications.headline') }}</span>
          <span class="pop-lede">{{ t('publications.lede') }}</span>
        </div>

        <div class="pop-fields">
          <div class="field">
            <span class="field-label">{{ t('publications.visibility') }}</span>
            <button
              type="button"
              class="field-value"
              :class="{ active: menu === 'vis' }"
              :aria-expanded="menu === 'vis'"
              @click="menu = menu === 'vis' ? '' : 'vis'"
            >
              {{ visLabel }}
              <Icon name="ChevronDown" :size="9" :stroke-width="3" class="field-caret" />
            </button>
            <div v-if="menu === 'vis'" class="field-menu">
              <button
                v-for="option in options"
                :key="option.value"
                type="button"
                class="field-option"
                @click="pickVisibility(option.value)"
              >
                <span class="option-mark">
                  <Icon v-if="store.visibility === option.value" name="Check" :size="12" :stroke-width="2.8" />
                </span>
                <span class="option-text">
                  <span>{{ option.label }}</span>
                  <span class="option-sub">{{ option.hint }}</span>
                </span>
              </button>
            </div>
          </div>

          <div class="field">
            <span class="field-label">{{ t('publications.environment') }}</span>
            <button
              type="button"
              class="field-value"
              :class="{ active: menu === 'env' }"
              :disabled="environments.length === 0"
              :aria-expanded="menu === 'env'"
              :title="environments.length === 0 ? t('publications.envNoneHint') : ''"
              @click="menu = menu === 'env' ? '' : 'env'"
            >
              {{ envLabel }}
              <Icon name="ChevronDown" :size="9" :stroke-width="3" class="field-caret" />
            </button>
            <div v-if="menu === 'env'" class="field-menu">
              <button
                v-for="choice in environments"
                :key="choice.id"
                type="button"
                class="field-option"
                @click="pickEnvironment(choice.id)"
              >
                <span class="option-mark">
                  <Icon v-if="store.environment === choice.id" name="Check" :size="12" :stroke-width="2.8" />
                </span>
                <span class="option-text">
                  <span>{{ choice.name }}</span>
                  <span class="option-sub">{{ choice.baseUrl }}</span>
                </span>
              </button>
            </div>
          </div>
        </div>

        <div v-if="wantsPassword" class="field-pass">
          <input
            v-model="password"
            type="password"
            class="pass-input"
            autocomplete="new-password"
            :aria-label="t('publications.password')"
            :placeholder="t('publications.passwordPlaceholder', passwordRange)"
          />
        </div>
        <div v-if="wantsPassword && password && !passwordOk" class="pass-note">
          {{ t('publications.passwordLength', passwordRange) }}
        </div>

        <div class="pop-frame">
          <div class="frame-line">
            <Icon name="Shield" :size="15" :stroke-width="1.9" class="mark-ok" />
            <span>
              {{ t('publications.hiddenHead') }}
              <code class="mono">{{ hiddenNames }}</code>
            </span>
          </div>
          <div v-for="held in hidden" :key="held.name" class="frame-line quiet">
            <span class="mark-space"></span>
            <span><code class="mono">{{ held.name }}</code> — {{ hiddenText(held.reason) }}</span>
          </div>
          <div v-for="(problem, at) in problems" :key="at" class="frame-line">
            <Icon name="TriangleAlert" :size="15" :stroke-width="1.9" class="mark-warn" />
            <span>{{ problem }}</span>
          </div>
          <div v-if="refusal" class="frame-line">
            <Icon name="TriangleAlert" :size="15" :stroke-width="1.9" class="mark-warn" />
            <span>{{ refusal }}</span>
          </div>
        </div>

        <div class="pop-go">
          <button type="button" class="primary" :disabled="!canPublish" @click="publish">
            {{ publishLabel }}
          </button>
        </div>
      </template>

      <template v-else>
        <div class="pop-live">
          <div class="live-head">
            <span class="share-dot" :class="behind ? 'behind' : 'live'"></span>
            <span class="live-word">{{ t('publications.published') }}</span>
            <span class="live-meta">{{ statusText }}</span>
            <span class="live-gap"></span>
            <button type="button" class="live-open" @click="openPage">
              {{ t('publications.open') }}
              <Icon name="ArrowUpRight" :size="11" :stroke-width="2.1" />
            </button>
          </div>

          <div class="live-url">
            <span class="url-text">{{ publication.url }}</span>
            <button type="button" class="copy-btn" @click="copy">
              {{ t('publications.copy') }}
            </button>
          </div>

          <button v-if="inherited" type="button" class="live-owner" @click="openOwner">
            {{ ownerTitle }}
          </button>
        </div>

        <div v-if="pending" class="pop-changes">
          <div class="changes-head">
            <span class="changes-title">{{ pendingText }}</span>
            <button type="button" class="update" :disabled="!canPublish" @click="publish">
              {{ blockLabel }}
            </button>
          </div>

          <div v-for="(row, at) in rows" :key="at" class="chg-row">
            <span class="chg-sign" :class="row.tone">{{ row.sign }}</span>
            <span v-if="row.method" class="chg-method" :class="methodInkClass(row.method)">
              {{ row.method }}
            </span>
            <span class="chg-name">{{ row.name }}</span>
            <span class="chg-what">{{ row.what }}</span>
          </div>

          <div v-if="tally.length" class="chg-tally">
            <span v-for="(held, at) in tally" :key="at" class="chg-count">
              <span class="chg-sign" :class="held.tone">{{ held.sign }}</span>
              {{ held.label }}
            </span>
          </div>
          <button v-if="tally.length" type="button" class="chg-all" @click="openAll">
            {{ t('publications.allChanges') }}
          </button>
        </div>

        <div class="pop-fields">
          <div class="field">
            <span class="field-label">{{ t('publications.visibility') }}</span>
            <button
              type="button"
              class="field-value"
              :class="{ active: menu === 'vis' }"
              :aria-expanded="menu === 'vis'"
              @click="menu = menu === 'vis' ? '' : 'vis'"
            >
              {{ visLabel }}
              <Icon name="ChevronDown" :size="9" :stroke-width="3" class="field-caret" />
            </button>
            <div v-if="menu === 'vis'" class="field-menu">
              <button
                v-for="option in options"
                :key="option.value"
                type="button"
                class="field-option"
                @click="pickVisibility(option.value)"
              >
                <span class="option-mark">
                  <Icon v-if="store.visibility === option.value" name="Check" :size="12" :stroke-width="2.8" />
                </span>
                <span class="option-text">
                  <span>{{ option.label }}</span>
                  <span class="option-sub">{{ option.hint }}</span>
                </span>
              </button>
            </div>
          </div>

          <div class="field">
            <span class="field-label">{{ t('publications.environment') }}</span>
            <button
              type="button"
              class="field-value"
              :class="{ active: menu === 'env' }"
              :disabled="environments.length === 0"
              :aria-expanded="menu === 'env'"
              :title="environments.length === 0 ? t('publications.envNoneHint') : ''"
              @click="menu = menu === 'env' ? '' : 'env'"
            >
              {{ envLabel }}
              <Icon name="ChevronDown" :size="9" :stroke-width="3" class="field-caret" />
            </button>
            <div v-if="menu === 'env'" class="field-menu">
              <button
                v-for="choice in environments"
                :key="choice.id"
                type="button"
                class="field-option"
                @click="pickEnvironment(choice.id)"
              >
                <span class="option-mark">
                  <Icon v-if="store.environment === choice.id" name="Check" :size="12" :stroke-width="2.8" />
                </span>
                <span class="option-text">
                  <span>{{ choice.name }}</span>
                  <span class="option-sub">{{ choice.baseUrl }}</span>
                </span>
              </button>
            </div>
          </div>
        </div>

        <div v-if="wantsPassword" class="field-pass">
          <input
            v-model="password"
            type="password"
            class="pass-input"
            autocomplete="new-password"
            :aria-label="t('publications.password')"
            :placeholder="t('publications.passwordPlaceholder', passwordRange)"
          />
        </div>
        <div v-if="wantsPassword && password && !passwordOk" class="pass-note">
          {{ t('publications.passwordLength', passwordRange) }}
        </div>

        <div class="pop-links">
          <button
            v-if="publication.canManage && !inherited"
            type="button"
            class="link bad"
            @click="askUnpublish"
          >
            {{ t('publications.unpublish') }}
          </button>
          <span class="live-gap"></span>
          <button type="button" class="link" @click="openAll">{{ t('publications.all') }}</button>
        </div>
      </template>
    </PopoverContent>
  </Popover>
</template>

<style scoped>
@reference "../../style.css";

.share-btn {
  @apply flex items-center justify-center flex-none cursor-pointer border-0 whitespace-nowrap;
  height: 2rem;
  padding: 0 0.6875rem;
  gap: 0.4375rem;
  border-radius: 0.5rem;
  background: transparent;
  color: var(--text);
  font-family: inherit;
  font-size: var(--text-13);
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

.share-dot.behind {
  background: var(--orange);
}

.share-pop {
  @apply flex flex-col;
  width: 23.25rem;
  border: 0;
  border-radius: 0.875rem;
  background: var(--pop);
  backdrop-filter: var(--blur-overlay);
  -webkit-backdrop-filter: var(--blur-overlay);
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

.pop-fields {
  @apply flex flex-col;
  padding: 0.375rem 1rem 0.625rem;
}

.field {
  @apply relative flex items-center;
  height: 2.125rem;
  gap: 0.625rem;
}

.field-label {
  @apply flex-1 text-text-secondary;
}

.field-value {
  @apply flex items-center flex-none cursor-pointer border-0 whitespace-nowrap bg-transparent;
  height: 1.75rem;
  padding: 0 0.5rem 0 0.625rem;
  gap: 0.4375rem;
  border-radius: 0.4375rem;
  color: var(--text);
  font-family: inherit;
  font-size: var(--text-13);
}

.field-value:hover:not(:disabled),
.field-value.active {
  background: var(--bg-hover);
}

.field-value:disabled {
  @apply cursor-default;
}

.field-caret {
  @apply text-text-tertiary;
}

.field-pass {
  @apply flex;
  margin: 0.125rem 1rem 0.25rem;
}

.pass-input {
  @apply flex-1 min-w-0 border-0 outline-none;
  height: 1.875rem;
  padding: 0 0.625rem;
  border-radius: 0.4375rem;
  background: var(--bg-inset);
  box-shadow: inset 0 0 0 1px var(--border-strong);
  font-family: inherit;
  font-size: var(--text-13);
  color: var(--text);
}

.pass-input:focus {
  box-shadow: inset 0 0 0 1px var(--accent), 0 0 0 3px var(--accent-soft);
}

.pass-note {
  @apply text-orange;
  margin: 0 1rem 0.25rem;
  font-size: var(--text-11-5);
}

.field-menu {
  @apply absolute flex flex-col;
  right: 0;
  top: 2.0625rem;
  z-index: 32;
  width: 15.625rem;
  padding: 0.3125rem;
  gap: 1px;
  border-radius: 0.625rem;
  background: var(--pop);
  backdrop-filter: var(--blur-overlay);
  -webkit-backdrop-filter: var(--blur-overlay);
  box-shadow: var(--popshadow);
}

.field-option {
  @apply flex items-center w-full cursor-pointer border-0 bg-transparent text-left;
  padding: 0.375rem 0.5rem;
  gap: 0.5rem;
  border-radius: 0.375rem;
  color: var(--text);
  font-family: inherit;
  font-size: var(--text-13);
}

.field-option:hover {
  background: var(--bg-hover);
}

.option-mark {
  @apply flex flex-none text-accent;
  width: 0.875rem;
}

.option-text {
  @apply flex-1 min-w-0 flex flex-col;
  gap: 1px;
}

.option-sub {
  @apply truncate text-text-tertiary;
  font-size: var(--text-11-5);
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

.frame-line.quiet {
  @apply text-text-secondary;
}

.frame-line .mono {
  font-family: var(--mono);
  font-size: var(--text-12);
}

.mark-ok {
  @apply flex-none text-green;
  margin-top: 1px;
}

.mark-warn {
  @apply flex-none text-orange;
  margin-top: 1px;
}

.mark-space {
  @apply flex-none;
  width: 0.9375rem;
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
  font-family: inherit;
  font-size: var(--text-13);
}

.primary:hover:not(:disabled) {
  filter: brightness(1.06);
}

.primary:disabled {
  @apply opacity-45 cursor-default;
}

.pop-live {
  @apply flex flex-col;
  padding: 0.875rem 1rem 0.625rem;
  gap: 0.625rem;
}

.live-head {
  @apply flex items-center;
  gap: 0.5rem;
}

.live-word {
  font-weight: 600;
}

.live-meta {
  @apply min-w-0 truncate text-text-tertiary;
  font-size: var(--text-12-5);
}

.live-gap {
  @apply flex-1;
}

.live-open {
  @apply flex items-center flex-none cursor-pointer border-0 whitespace-nowrap bg-transparent text-accent;
  gap: 0.3125rem;
  font-family: inherit;
  font-size: var(--text-12-5);
}

.live-url {
  @apply flex;
  gap: 0.375rem;
}

.url-text {
  @apply flex-1 min-w-0 truncate;
  height: 2rem;
  padding: 0 0.625rem;
  border-radius: 0.5rem;
  background: var(--bg-inset);
  box-shadow: inset 0 0 0 1px var(--border-strong);
  font-family: var(--mono);
  font-size: var(--text-12);
  line-height: 2rem;
}

.copy-btn {
  @apply flex-none cursor-pointer border-0 font-medium;
  height: 2rem;
  padding: 0 0.75rem;
  border-radius: 0.5rem;
  background: var(--accent);
  color: var(--accent-text);
  font-family: inherit;
  font-size: var(--text-13);
}

.copy-btn:hover {
  filter: brightness(1.06);
}

.live-owner {
  @apply self-start cursor-pointer border-0 bg-transparent p-0 text-accent;
  font-family: inherit;
  font-size: var(--text-12-5);
  text-align: left;
}

.pop-changes {
  @apply flex flex-col;
  margin: 0 1rem 0.375rem;
  padding: 0.625rem 0.75rem;
  gap: 0.375rem;
  border-radius: 0.625rem;
  background: color-mix(in srgb, var(--orange) 7%, transparent);
  box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--orange) 28%, transparent);
}

.changes-head {
  @apply flex items-center;
  margin-bottom: 0.125rem;
  gap: 0.625rem;
}

.changes-title {
  @apply flex-1;
  font-size: var(--text-12-5);
  font-weight: 600;
  line-height: 1.4;
}

.chg-row {
  @apply flex items-center;
  min-height: 1.5rem;
  gap: 0.5rem;
  font-size: var(--text-12-5);
}

.chg-sign {
  @apply flex items-center justify-center flex-none;
  width: 1rem;
  height: 1rem;
  border-radius: 0.25rem;
  font-family: var(--mono);
  font-size: var(--text-11);
  font-weight: 700;
  line-height: 1;
}

.chg-sign.add {
  background: var(--green-soft);
  color: var(--green);
}

.chg-sign.edit {
  background: var(--orange-soft);
  color: var(--orange);
}

.chg-sign.gone {
  background: var(--red-soft);
  color: var(--red);
}

.chg-method {
  @apply flex-none;
  width: 2.75rem;
  font-family: var(--mono);
  font-size: var(--text-10);
  font-weight: 700;
}

.chg-name {
  @apply min-w-0 truncate;
  flex: 1 1 4.375rem;
}

.chg-what {
  @apply min-w-0 truncate text-text-tertiary;
  flex: 0 1 auto;
  font-size: var(--text-12);
}

.chg-tally {
  @apply flex flex-wrap items-center;
  gap: 0.375rem 0.75rem;
  font-size: var(--text-12-5);
}

.chg-count {
  @apply flex items-center;
  gap: 0.375rem;
}

.chg-all {
  @apply self-start cursor-pointer border-0 bg-transparent text-accent;
  padding: 0;
  font-family: inherit;
  font-size: var(--text-12);
}

.update {
  @apply flex-none cursor-pointer border-0;
  height: 1.625rem;
  padding: 0 0.6875rem;
  border-radius: 0.4375rem;
  background: var(--accent);
  color: var(--accent-text);
  font-family: inherit;
  font-size: var(--text-12-5);
  font-weight: 500;
}

.update:disabled {
  @apply opacity-45 cursor-default;
}

.pop-links {
  @apply flex items-center;
  padding: 0.8rem 1rem;
  border-top: 1px solid var(--border);
}

.link {
  @apply cursor-pointer border-0 bg-transparent;
  color: var(--accent);
  font-family: inherit;
  font-size: var(--text-12-5);
}

.link.bad {
  color: var(--red);
}
</style>
