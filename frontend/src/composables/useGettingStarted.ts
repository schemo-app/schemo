import { computed } from 'vue'
import { useSetting, useSettings } from './useSettings'
import { WELCOME_STEPS } from '../lib/welcome'

/** The getting-started checklist as the window reads it: which steps have been walked, and whether
 *  the card was put away. Both live in the settings, so they outlive the window. */
export function useGettingStarted() {
  const onboarding = useSetting('onboarding')
  const { setOnboarding } = useSettings()

  const walked = (key: string) => (onboarding.value.done ?? []).includes(key)
  const count = computed(() => WELCOME_STEPS.filter((step) => walked(step.key)).length)
  const percent = computed(() => Math.round((count.value / WELCOME_STEPS.length) * 100))

  /** Once the card is put away it is gone; the Help menu opens the overview from then on. */
  const hidden = computed(() => onboarding.value.dismissed)

  /** Writes down that a step has been walked. Go keeps the steps it knows and drops the repeats, so
   *  a place that may fire twice needs no guard of its own. */
  function mark(key: string): void {
    if (walked(key)) return
    void setOnboarding({ done: [...(onboarding.value.done ?? []), key] })
  }

  function dismiss(): void {
    void setOnboarding({ dismissed: true })
  }

  return { onboarding, walked, count, percent, hidden, mark, dismiss }
}

/** The one thing the stores need of the checklist — that a step of theirs was walked. */
export function markStep(key: string): void {
  useGettingStarted().mark(key)
}
