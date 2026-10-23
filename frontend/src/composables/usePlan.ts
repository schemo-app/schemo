import { computed, ref } from 'vue'
import { Plan as PlanService } from '../../bindings/schemo/internal/cloud'
import type { Plan } from '../../bindings/schemo/internal/domain'
import { TIER_FREE } from '../lib/onboardingFeatures'

// Cached subscription plan state shared across the UI
const held = ref<Plan | null>(null)

/** Composable for inspecting subscription tier features, limits, and plan status. */
export function usePlan() {
  /** True if subscription tier metadata has been loaded. */
  const known = computed(() => !!held.value?.tier)

  /**
   * Checks whether the current plan includes the specified feature flag.
   * Returns true when plan state is unknown.
   */
  function includes(feature: string): boolean {
    if (!known.value) return true
    return held.value?.features?.includes(feature) ?? false
  }

  /** Formatted paid subscription details for settings UI, or null if free/unsubscribed. */
  const paid = computed(() => {
    const plan = held.value
    if (!plan?.tier || plan.tier === TIER_FREE) return null
    return { name: plan.tier, seats: plan.seats }
  })

  return { plan: held, known, includes, paid, loadPlan, refreshPlan }
}

/** Loads cached plan details from the local backend store. */
async function loadPlan(): Promise<void> {
  try {
    held.value = await PlanService.Current()
  } catch {
    // Unhandled errors leave feature checks unrestrictive
  }
}

/** Synchronizes subscription plan status with the remote server. */
async function refreshPlan(): Promise<void> {
  try {
    held.value = await PlanService.Refresh()
  } catch {
    await loadPlan()
  }
}