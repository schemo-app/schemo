<script setup lang="ts">
import { computed } from 'vue'
import type { WelcomeKind } from '../../lib/welcome'
import { formatBytes, formatMicros, useMessages } from '../../i18n'
import { methodInkClass, statusTone } from '../../lib/format'
import { usePlatform } from '../../composables/usePlatform'
import Icon from '../ui/Icon.vue'

defineProps<{ kind: WelcomeKind }>()
const { t } = useMessages()
const { shortcut } = usePlatform()

const base = '{{base_url}}'

const tabActions = computed(() => [
  t('welcome.mocks.drag'),
  t('welcome.mocks.middle'),
  shortcut('W'),
])

const answer = computed(() => `${formatMicros(142_000)} · ${formatBytes(1229)}`)

const colRows = [
  { method: 'GET', name: 'welcome.mocks.catalog' },
  { method: 'POST', name: 'welcome.mocks.addToCart' },
  { method: 'DELETE', name: 'welcome.mocks.removeFromCart' },
] as const

const envRows = [
  { name: 'Local · dev', dot: 'var(--green)', on: true },
  { name: 'Staging', dot: 'var(--orange)', on: false },
  { name: 'Production', dot: 'var(--red)', on: false },
] as const

const histRows = [
  { at: '10:42', method: 'GET', path: '/catalog?page=2', status: 200 },
  { at: '10:42', method: 'POST', path: '/cart/items', status: 201 },
  { at: '10:41', method: 'GET', path: '/promo/active', status: 200 },
  { at: '10:39', method: 'PATCH', path: '/profile', status: 422 },
] as const

const files = [
  { name: 'Postman', ext: '.json' },
  { name: 'OpenAPI', ext: '.yaml' },
  { name: 'Insomnia', ext: '.json' },
] as const

const spaces = computed(() => {
  const personal = t('workspaces.personal')
  return [
    { letter: personal.charAt(0), name: personal, tint: 'var(--purple)', on: false, meta: '' },
    { letter: 'A', name: 'Acme', tint: 'var(--teal)', on: true, meta: t('workspaces.kindTeam') },
  ]
})

const people = [
  { label: 'АК', tint: 'var(--orange)' },
  { label: 'ДМ', tint: 'var(--green)' },
  { label: 'ЕС', tint: 'var(--purple)' },
  { label: '+3', tint: 'var(--text-tertiary)' },
] as const
</script>

<template>
  <div class="feature-mock">
    <template v-if="kind === 'req'">
      <div class="row">
        <div class="box bar mono">
          <span class="verb method-post">POST</span>
          <span class="ell">curl -X POST https://api.shop.dev/cart -d '{"sku":4812}'</span>
        </div>
        <span class="send">{{ t('request.send') }}</span>
      </div>
      <div class="box pad mono">
        <div class="row head">
          <span class="pill ok">201 Created</span>
          <span class="muted">{{ answer }}</span>
        </div>
        <span><span class="tok-key">"id"</span>: <span class="tok-num">4812</span>,</span>
        <span><span class="tok-key">"qty"</span>: <span class="tok-num">2</span></span>
      </div>
    </template>

    <template v-else-if="kind === 'tabs'">
      <div class="strip">
        <div class="tab on">
          <span class="verb method-get">GET</span>
          <span>{{ t('welcome.mocks.catalog') }}</span>
          <span class="temp">{{ t('welcome.mocks.temp') }}</span>
        </div>
        <div class="tab">
          <span class="verb method-post">POST</span>
          <span>{{ t('welcome.mocks.cart') }}</span>
          <span class="dot"></span>
        </div>
      </div>
      <div class="row wide">
        <span class="kbd">
          <Icon name="MousePointer2" :size="11" />
          {{ t('counts.clicks', 2) }}
        </span>
        <Icon name="ArrowRight" :size="14" class="arrow" />
        <span class="box pillreq">
          <span class="verb method-get">GET</span>
          <span class="ell">{{ t('welcome.mocks.catalog') }}</span>
        </span>
      </div>
      <div class="acts">
        <span v-for="word in tabActions" :key="word" class="act">{{ word }}</span>
      </div>
    </template>

    <template v-else-if="kind === 'col'">
      <div class="box tree">
        <div class="treehead">
          <Icon name="ChevronDown" :size="9" :stroke-width="3" class="faint" />
          <Icon name="Folder" :size="13" class="folder" />
          <span>Example API</span>
        </div>
        <div v-for="row in colRows" :key="row.name" class="treerow">
          <span class="verb" :class="methodInkClass(row.method)">{{ row.method }}</span>
          <span class="ell">{{ t(row.name) }}</span>
          <Icon name="Check" :size="11" :stroke-width="3" class="tick" />
        </div>
      </div>
      <div class="row wide">
        <span class="rail"></span>
        <span class="muted">{{ t('collections.runProgress', { i: 6, n: 6 }) }}</span>
      </div>
    </template>

    <template v-else-if="kind === 'env'">
      <div class="box bar mono">
        <span class="verb method-get">GET</span>
        <span><span class="token">{{ base }}</span><span class="muted">/catalog</span></span>
      </div>
      <div class="env-list">
        <div v-for="env in envRows" :key="env.name" class="envrow" :class="{ on: env.on }">
          <span class="envdot" :style="{ background: env.dot }"></span>
          <span class="ell">{{ env.name }}</span>
          <Icon v-if="env.on" name="Check" :size="10" :stroke-width="3" class="check" />
        </div>
      </div>
    </template>

    <template v-else-if="kind === 'hist'">
      <div class="row wide">
        <span class="cap">
          <span class="capdot"></span>
          {{ t('sidebar.capture') }} · Chrome
        </span>
        <span class="muted">{{ t('history.today') }}</span>
      </div>
      <div class="box hist">
        <div v-for="record in histRows" :key="record.path" class="histrow">
          <span class="clock">{{ record.at }}</span>
          <span class="verb" :class="methodInkClass(record.method)">{{ record.method }}</span>
          <span class="ell">{{ record.path }}</span>
          <span class="code" :class="statusTone(record)">{{ record.status }}</span>
        </div>
      </div>
    </template>

    <template v-else-if="kind === 'docs'">
      <div class="segs">
        <span class="seg on">{{ t('request.chips.docs') }}</span>
        <span class="seg">{{ t('request.chips.examples') }} · 3</span>
      </div>
      <div class="box pad doc">
        <span class="doctitle">{{ t('welcome.mocks.product') }}</span>
        <span class="muted body">{{ t('welcome.mocks.productNote') }}</span>
        <div class="pills">
          <span class="pill ok">200</span>
          <span class="pill bad">404</span>
          <span class="pill bad">422</span>
        </div>
      </div>
    </template>

    <template v-else-if="kind === 'import'">
      <div class="row wide">
        <div class="files">
          <div v-for="file in files" :key="file.name" class="box file">
            <span class="ell">{{ file.name }}</span>
            <span class="ext">{{ file.ext }}</span>
          </div>
        </div>
        <Icon name="ArrowRight" :size="16" class="arrow" />
        <div class="box target">
          <Icon name="Folder" :size="18" class="folder-mark" />
          <span class="targetname">Example API</span>
          <span class="muted">{{ t('counts.requests', 24) }}</span>
        </div>
      </div>
    </template>

    <template v-else-if="kind === 'ws'">
      <div class="box spaces">
        <div
          v-for="space in spaces"
          :key="space.name"
          class="spacerow"
          :class="{ on: space.on }"
        >
          <span class="avatar" :style="{ background: space.tint }">{{ space.letter }}</span>
          <span class="ell" :class="{ strong: space.on }">{{ space.name }}</span>
          <span class="meta">{{ space.meta }}</span>
        </div>
        <div class="spacesep"></div>
        <div class="spacerow new">
          <span class="plus">+</span>
          {{ t('workspaces.createMenu') }}
        </div>
      </div>
      <div class="row wide">
        <div class="people">
          <span
            v-for="person in people"
            :key="person.label"
            class="person"
            :style="{ background: person.tint }"
          >{{ person.label }}</span>
        </div>
        <span class="muted">{{ t('counts.members', 6) }}</span>
      </div>
    </template>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.feature-mock {
  @apply flex flex-col justify-center;
  box-sizing: border-box;
  width: 100%;
  height: 100%;
  padding: 1rem 1.125rem;
  gap: 0.5rem;
  border-radius: 0.625rem;
  background: var(--raise);
  color: var(--text);
  font-size: var(--text-11-5);
  overflow: hidden;
}

.row {
  @apply flex items-center;
  gap: 0.375rem;
}

.row.wide {
  gap: 0.625rem;
  padding: 0.25rem 0.125rem 0;
}

.ell {
  @apply min-w-0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mono {
  font-family: var(--mono);
}

.muted {
  color: var(--text-tertiary);
}

.box {
  border-radius: 0.4375rem;
  background: var(--bg);
  box-shadow: inset 0 0 0 1px var(--border);
}

.verb {
  @apply flex-none;
  font-family: var(--mono);
  font-style: normal;
  font-weight: 700;
}

.bar {
  @apply flex items-center flex-1;
  min-width: 0;
  height: 1.75rem;
  padding: 0 0.625rem;
  gap: 0.5rem;
  font-size: var(--text-10-5);
  white-space: nowrap;
  overflow: hidden;
}

.send {
  @apply flex items-center flex-none;
  height: 1.75rem;
  padding: 0 0.625rem;
  border-radius: 0.4375rem;
  background: var(--accent);
  color: var(--accent-text);
  font-weight: 600;
}

.pad {
  @apply flex flex-col;
  padding: 0.5625rem 0.625rem;
  gap: 0.1875rem;
  font-size: var(--text-10-5);
}

.row.head {
  gap: 0.5rem;
  margin-bottom: 0.25rem;
}

.pill {
  padding: 0.0625rem 0.375rem;
  border-radius: 0.25rem;
  font-weight: 600;
}

.pill.ok {
  background: var(--green-soft);
  color: var(--green);
}

.pill.bad {
  background: var(--red-soft);
  color: var(--red);
}

.tok-key {
  color: var(--tok-key);
}

.tok-num {
  color: var(--tok-num);
}

.strip {
  @apply flex items-end;
  gap: 0.125rem;
  padding: 0 0.375rem;
  border-bottom: 1px solid var(--border);
  white-space: nowrap;
  overflow: hidden;
}

.tab {
  @apply flex items-center flex-none;
  height: 1.875rem;
  padding: 0 0.625rem;
  gap: 0.375rem;
  border-radius: 0.4375rem 0.4375rem 0 0;
  color: var(--text-secondary);
}

.tab.on {
  background: var(--bg);
  box-shadow: inset 0 0 0 1px var(--border);
  font-style: italic;
}

.strip .verb,
.pillreq .verb {
  font-size: 0.59375rem;
}

.temp {
  margin-left: 0.25rem;
  padding: 0 0.3125rem;
  border-radius: 0.25rem;
  background: var(--accent-soft);
  color: var(--accent);
  font-size: var(--text-10);
}

.dot {
  width: 0.375rem;
  height: 0.375rem;
  margin-left: 0.125rem;
  border-radius: 50%;
  background: var(--text-tertiary);
}

.kbd {
  @apply flex items-center flex-none;
  height: 1.375rem;
  padding: 0 0.5rem;
  gap: 0.3125rem;
  border-radius: 0.375rem;
  background: var(--bg);
  box-shadow: inset 0 0 0 1px var(--border);
  color: var(--text-secondary);
}

.pillreq {
  @apply flex items-center;
  height: 1.625rem;
  padding: 0 0.625rem;
  gap: 0.375rem;
  white-space: nowrap;
  overflow: hidden;
}

.arrow {
  @apply flex-none;
  color: var(--text-tertiary);
}

.acts {
  @apply flex flex-wrap;
  gap: 0.3125rem;
  padding-top: 0.25rem;
}

.act {
  @apply flex items-center;
  height: 1.25rem;
  padding: 0 0.4375rem;
  border-radius: 0.3125rem;
  background: var(--chip);
  color: var(--text-secondary);
  font-size: var(--text-10-5);
  white-space: nowrap;
}

.tree {
  @apply flex flex-col;
  padding: 0.375rem;
  gap: 0.0625rem;
}

.treehead {
  @apply flex items-center;
  height: 1.5rem;
  padding: 0 0.375rem;
  gap: 0.375rem;
  font-weight: 600;
}

.treerow {
  @apply flex items-center;
  height: 1.375rem;
  padding: 0 0.375rem 0 1.375rem;
  gap: 0.375rem;
}

.treerow .verb {
  width: 2.375rem;
  font-size: 0.59375rem;
}

.faint {
  color: var(--text-tertiary);
}

.folder {
  color: var(--text-secondary);
}

.tick {
  @apply flex-none;
  color: var(--green);
}

.rail {
  flex: 1;
  height: 0.25rem;
  border-radius: 0.125rem;
  background: var(--green);
}

.env-list {
  @apply flex flex-col self-end;
  width: 10.625rem;
  padding: 0.25rem;
  gap: 0.0625rem;
  border-radius: 0.5rem;
  background: var(--bg);
  box-shadow: var(--popshadow);
}

.envrow {
  @apply flex items-center;
  height: 1.5rem;
  padding: 0 0.5rem;
  gap: 0.5rem;
  border-radius: 0.3125rem;
}

.envrow.on {
  background: var(--bg-hover);
}

.envdot {
  @apply flex-none;
  width: 0.375rem;
  height: 0.375rem;
  border-radius: 50%;
}

.check {
  color: var(--text-secondary);
}

.token {
  padding: 0.0625rem 0.125rem;
  border-radius: 0.25rem;
  background: var(--accent-soft);
  color: var(--accent);
}

.cap {
  @apply flex items-center;
  height: 1.25rem;
  padding: 0 0.5rem;
  gap: 0.375rem;
  border-radius: 0.625rem;
  background: var(--red-soft);
  color: var(--red);
  font-weight: 500;
}

.capdot {
  @apply flex-none;
  width: 0.375rem;
  height: 0.375rem;
  border-radius: 50%;
  background: var(--red);
}

.hist {
  @apply flex flex-col;
  padding: 0.1875rem 0;
}

.histrow {
  display: grid;
  grid-template-columns: 2.125rem 2.625rem minmax(0, 1fr) 1.625rem;
  align-items: center;
  height: 1.375rem;
  padding: 0 0.625rem;
  font-family: var(--mono);
  font-size: var(--text-10);
}

.clock {
  color: var(--text-tertiary);
}

.code {
  text-align: right;
}

.code.is-ok {
  color: var(--green-text);
}

.code.is-bad {
  color: var(--red-text);
}

.segs {
  @apply flex self-start;
  padding: 0.125rem;
  border-radius: 0.4375rem;
  background: var(--chip);
}

.seg {
  @apply flex items-center;
  height: 1.25rem;
  padding: 0 0.5625rem;
  border-radius: 0.3125rem;
  color: var(--text-secondary);
}

.seg.on {
  background: var(--bg);
  box-shadow: 0 0.5px 1.5px rgba(0, 0, 0, 0.12);
  color: var(--text);
  font-weight: 500;
}

.doc {
  padding: 0.5625rem 0.75rem;
  gap: 0.3125rem;
}

.doctitle {
  font-size: var(--text-12-5);
  font-weight: 600;
}

.body {
  line-height: 1.45;
}

.pills {
  @apply flex flex-wrap;
  gap: 0.375rem;
}

.files {
  @apply flex flex-col flex-1;
  min-width: 0;
  gap: 0.3125rem;
}

.file {
  @apply flex items-center;
  height: 1.625rem;
  padding: 0 0.4375rem;
  gap: 0.3125rem;
  white-space: nowrap;
  overflow: hidden;
}

.file .ell {
  flex: 1 0 auto;
}

.ext {
  @apply flex-none;
  font-family: var(--mono);
  font-size: 0.59375rem;
  color: var(--text-tertiary);
}

.target {
  @apply flex flex-col;
  flex: 0 1 4.875rem;
  min-width: 0;
  padding: 0.5rem;
  gap: 0.25rem;
  white-space: nowrap;
  overflow: hidden;
}

.folder-mark {
  color: var(--accent);
}

.targetname {
  font-weight: 600;
}

.spaces {
  @apply flex flex-col;
  padding: 0.25rem;
  gap: 0.0625rem;
  border-radius: 0.5rem;
}

.spacerow {
  @apply flex items-center;
  height: 1.625rem;
  padding: 0 0.375rem;
  gap: 0.5rem;
  border-radius: 0.3125rem;
}

.spacerow.on {
  background: var(--bg-hover);
}

.spacerow.new {
  color: var(--text-secondary);
}

.avatar {
  @apply flex items-center justify-center flex-none;
  width: 1rem;
  height: 1rem;
  border-radius: 0.3125rem;
  color: var(--accent-text);
  font-size: 0.5625rem;
  font-weight: 700;
}

.strong {
  font-weight: 600;
}

.meta {
  color: var(--text-tertiary);
}

.spacesep {
  height: 1px;
  margin: 0.1875rem 0.375rem;
  background: var(--border);
}

.plus {
  width: 1rem;
  text-align: center;
  font-size: 0.875rem;
}

.people {
  @apply flex;
}

.person {
  @apply flex items-center justify-center;
  width: 1.25rem;
  height: 1.25rem;
  margin-right: -0.3125rem;
  border-radius: 50%;
  color: var(--accent-text);
  font-size: 0.53125rem;
  font-weight: 700;
  box-shadow: 0 0 0 2px var(--raise);
}
</style>
