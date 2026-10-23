<script setup lang="ts">
import { TabKind } from '../../../bindings/schemo/internal/domain'
import { WELCOME } from '../../lib/welcomeCards'
import { t } from '../../i18n'
import { useTabsStore } from '../../stores/tabs'
import FeatureMock from './FeatureMock.vue'
import logoUrl from '../../assets/logo.svg'

// The overview: what the application can do, a card apiece. A card is a line of `lib/welcome`, so a
// new one is added to that list and needs nothing here; the first gets the wide place, the second
// the narrow one beside it, and the rest go in the grid.
const tabs = useTabsStore()

const [hero, pair] = WELCOME
const rest = WELCOME.slice(2)

/** The overview has said what it had to say: it steps out of the strip, and what stood beside it is
 *  back on screen. */
function start(): void {
  if (tabs.activeKind === TabKind.TabKindWelcome) void tabs.close(tabs.activeId)
}
</script>

<template>
  <div class="welcome">
    <div class="welcome-inner">
      <div class="welcome-head">
        <div class="welcome-lead">
          <img :src="logoUrl" class="welcome-mark" alt="" />
          <span class="welcome-title">{{ t('welcome.title') }}</span>
          <span class="welcome-note">{{ t('welcome.subtitle') }}</span>
        </div>
        <button type="button" class="welcome-start" @click="start()">
          {{ t('welcome.start') }}
        </button>
      </div>

      <div class="welcome-section">
        <div class="welcome-row">
          <div class="card card-hero">
            <div class="card-mock"><FeatureMock :kind="hero.kind" /></div>
            <div class="card-text">
              <span class="card-title">{{ t(hero.title) }}</span>
              <span class="card-desc">{{ t(hero.desc) }}</span>
            </div>
          </div>
          <div class="card card-pair">
            <div class="card-mock"><FeatureMock :kind="pair.kind" /></div>
            <div class="card-text">
              <span class="card-title">{{ t(pair.title) }}</span>
              <span class="card-desc">{{ t(pair.desc) }}</span>
            </div>
          </div>
        </div>

        <div class="welcome-grid">
          <div v-for="card in rest" :key="card.kind" class="card">
            <div class="card-mock"><FeatureMock :kind="card.kind" /></div>
            <div class="card-text">
              <span class="card-title">{{ t(card.title) }}</span>
              <span class="card-desc">{{ t(card.desc) }}</span>
            </div>
          </div>
        </div>
      </div>

      <span class="welcome-foot">{{ t('welcome.footnote') }}</span>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.welcome {
  @apply flex-1 min-h-0 overflow-auto;
}

.welcome-inner {
  @apply flex flex-col mx-auto;
  max-width: 65rem;
  padding: 2.5rem 2rem 3rem;
  gap: 2rem;
}

.welcome-head {
  @apply flex items-end flex-wrap;
  gap: 1rem 1.5rem;
}

.welcome-lead {
  @apply flex flex-col flex-1 min-w-0;
  flex-basis: 23.75rem;
  gap: 0.625rem;
}

.welcome-mark {
  @apply flex-none block;
  width: 2.75rem;
  height: 2.75rem;
}

.welcome-title {
  @apply text-26 font-semibold;
  letter-spacing: -0.015em;
  margin-top: 0.5rem;
}

.welcome-note {
  @apply text-text-secondary text-14;
  line-height: 1.55;
  max-width: 36.25rem;
  text-wrap: pretty;
}

.welcome-start {
  @apply flex-none cursor-pointer;
  height: 2.125rem;
  padding: 0 1.125rem;
  border: 0;
  border-radius: 0.5rem;
  font-family: inherit;
  font-size: inherit;
  font-weight: 600;
  background: var(--accent);
  color: var(--accent-text);
}

.welcome-section {
  @apply flex flex-col;
  gap: 1rem;
}

.welcome-row {
  @apply flex flex-wrap;
  gap: 1rem;
}

.welcome-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(15rem, 1fr));
  gap: 1rem;
}

.card {
  @apply flex flex-col;
  padding: 0.3rem;
  border-radius: 0.875rem;
  box-shadow: inset 0 0 0 1px var(--border);
}

.card-hero {
  flex: 2 1 28.75rem;
  min-width: 0;
}

.card-pair {
  flex: 1 1 15rem;
  min-width: 0;
}

.card-mock {
  @apply flex-none;
  height: 10rem;
}

.card-text {
  @apply flex flex-col;
  gap: 0.3125rem;
  padding: 0.875rem 0.625rem 0.625rem;
}

.card-title {
  @apply text-13-5 font-semibold;
}

.card-desc {
  @apply text-text-secondary text-12-5;
  line-height: 1.5;
  text-wrap: pretty;
}

.welcome-foot {
  @apply text-text-tertiary text-12-5;
}
</style>
