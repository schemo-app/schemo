<script setup lang="ts">
import { computed } from 'vue'
import SettingsRow from './SettingsRow.vue'
import SettingsSection from './SettingsSection.vue'
import Button from '../ui/Button.vue'
import type { Meter } from '../../../bindings/schemo/internal/domain'
import { useAccount } from '../../composables/useAccount'
import { usePlan } from '../../composables/usePlan'
import { formatBytes, formatDate, type MessageKey, t } from '../../i18n'
import { TIER_FREE } from '../../composables/usePlan'
import { metaLine } from '../../lib/format'

const { plan } = usePlan()
const { openPanel } = useAccount()

const tariff = computed(() => (plan.value?.tier ? plan.value : null))
const free = computed(() => tariff.value?.tier === TIER_FREE)

// The tier as the server writes it, with the first letter capitalised: the name is an
// identifier, and a name written on its own takes a capital.
const tierLabel = computed(() => {
  const tier = tariff.value?.tier ?? ''
  return tier ? tier[0].toUpperCase() + tier.slice(1) : ''
})

const METERS: Record<string, { label: MessageKey; clause: MessageKey }> = {
  collections: { label: 'account.meter.collections', clause: 'account.limitsWhat.collections' },
  members: { label: 'account.meter.members', clause: 'account.limitsWhat.members' },
  storage: { label: 'account.meter.storage', clause: 'account.limitsWhat.storage' },
}

const MEGABYTE = 1 << 20

// A count is read as itself and storage in the unit its cap is written in — the megabytes the
// server capped it by. The bar is the share, and its colour is how close to the end that is.
function reading(one: Meter): { value: string; share: number; tone: 'ok' | 'warn' | 'bad' } {
  const storage = one.kind === 'storage'
  const used = storage ? formatBytes(one.used * MEGABYTE) : String(one.used)
  const limit = storage ? formatBytes(one.limit * MEGABYTE) : String(one.limit)
  const share = one.limit > 0 ? Math.min(100, Math.round((one.used / one.limit) * 100)) : 100
  return {
    value: t('account.of', { used, limit }),
    share,
    tone: share >= 100 ? 'bad' : share >= 90 ? 'warn' : 'ok',
  }
}

// Only the free plan shows meters; kinds this build has no words for are skipped.
const meters = computed(() => {
  if (!free.value) return []
  return (tariff.value?.meters ?? []).flatMap((one) => {
    const known = METERS[one.kind]
    return known ? [{ kind: one.kind, label: t(known.label), ...reading(one) }] : []
  })
})

const tariffNote = computed(() => {
  const held = tariff.value
  if (!held) return ''
  // A free plan at the end of a limit says which one it is — and whether it is over or almost
  // there, because "almost" for a cap that is spent is one more thing that is not true.
  const tight = meters.value.filter((one) => one.tone !== 'ok')
  if (tight.length) {
    const what = metaLine(...tight.map((one) => t(METERS[one.kind].clause)))
    const lead = tight.some((one) => one.tone === 'bad') ? 'account.limitsSpent' : 'account.limitsAlmost'
    return t(lead, { what })
  }
  const seats = held.seats ? t('account.planSeats', { n: held.seats }) : ''
  const until = held.planUntil
    ? t('account.planUntil', {
        date: formatDate(held.planUntil, { day: 'numeric', month: 'long', year: 'numeric' }),
      })
    : ''
  return metaLine(seats, until)
})
</script>

<template>
  <SettingsSection v-if="tariff" :label="t('account.plan')">
    <SettingsRow :title="tierLabel" :note="tariffNote">
      <Button variant="outline" size="field" @click="openPanel()">
        {{ free ? t('account.planCtaFree') : t('account.planCta') }}
      </Button>
    </SettingsRow>
    <!-- The free plan's bars: what stands against each cap the server sends, and how near the end it
         is. -->
    <div v-if="meters.length" class="card-block meters">
      <span v-for="one in meters" :key="one.kind" class="meter">
        <span class="meter-line">
          <span class="meter-label">{{ one.label }}</span>
          <span class="meter-value" :class="one.tone">{{ one.value }}</span>
        </span>
        <span class="meter-track">
          <span class="meter-fill" :class="one.tone" :style="{ width: one.share + '%' }"></span>
        </span>
      </span>
    </div>
  </SettingsSection>
</template>

<style scoped>
@reference "../../style.css";

.meters {
  @apply grid grid-cols-2;
  gap: 1.5rem;
  padding: 0.75rem 1rem 0.875rem;
}

.meter {
  @apply flex flex-col min-w-0;
  gap: 0.3125rem;
}

.meter-line {
  @apply flex items-baseline justify-between gap-2 text-12;
}

.meter-label {
  @apply text-text-secondary truncate;
}

.meter-value {
  @apply flex-none;
  font-variant-numeric: tabular-nums;
}

.meter-value.ok {
  color: var(--accent);
}

.meter-value.warn {
  color: var(--orange-text);
}

.meter-value.bad {
  color: var(--red-text);
}

.meter-track {
  @apply block h-1.25 rounded-full overflow-hidden;
  background: var(--bg-hover);
}

.meter-fill {
  @apply block h-full rounded-full;
}

.meter-fill.ok {
  background: var(--accent);
}

.meter-fill.warn {
  background: var(--orange);
}

.meter-fill.bad {
  background: var(--red-text);
}
</style>
