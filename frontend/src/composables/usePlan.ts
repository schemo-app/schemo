import { computed, ref } from 'vue'
import { Plan as PlanService } from '../../bindings/schemo/internal/cloud'
import type { Feature, Plan } from '../../bindings/schemo/internal/domain'

/** The tier a device works on when nobody has bought anything, in the server's vocabulary. */
export const TIER_FREE = 'free'

// Cached subscription plan state shared across the UI
const held = ref<Plan | null>(null)

/** Composable for inspecting subscription tier features, limits, and plan status. */
export function usePlan() {
  /** True if subscription tier metadata has been loaded. */
  const known = computed(() => !!held.value?.tier)

  /** True when the plan includes the feature, or while the plan is unknown. */
  function includes(feature: Feature): boolean {
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