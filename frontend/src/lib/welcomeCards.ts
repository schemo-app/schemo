import type { MessageKey } from '../i18n'

/** What a card of the overview is about: the picture the window hands `FeatureMock`. */
export type WelcomeKind = 'req' | 'tabs' | 'col' | 'env' | 'hist' | 'docs' | 'import' | 'ws'

/**
 * One card of the overview: the picture it draws, and the two catalogued words it carries. The order
 * is the layout's: the first is the leading card, the next the narrower one beside it, the rest the
 * grid.
 */
export interface WelcomeCard {
  kind: WelcomeKind
  title: MessageKey
  desc: MessageKey
}

/** The overview's cards. A new one is a line here and its two words in the catalogues: nothing
 *  more is asked of it. */
export const WELCOME: WelcomeCard[] = [
  { kind: 'req', title: 'welcome.cards.req.title', desc: 'welcome.cards.req.desc' },
  { kind: 'tabs', title: 'welcome.cards.tabs.title', desc: 'welcome.cards.tabs.desc' },
  { kind: 'col', title: 'welcome.cards.col.title', desc: 'welcome.cards.col.desc' },
  { kind: 'env', title: 'welcome.cards.env.title', desc: 'welcome.cards.env.desc' },
  { kind: 'hist', title: 'welcome.cards.hist.title', desc: 'welcome.cards.hist.desc' },
  { kind: 'docs', title: 'welcome.cards.docs.title', desc: 'welcome.cards.docs.desc' },
  { kind: 'import', title: 'welcome.cards.import.title', desc: 'welcome.cards.import.desc' },
  { kind: 'ws', title: 'welcome.cards.ws.title', desc: 'welcome.cards.ws.desc' },
]

/** One step of the getting-started checklist. `key` is the name Go keeps (`domain.OnboardingStep`),
 *  and is what the window sends when the step is walked. */
export interface WelcomeStep {
  key: string
  label: MessageKey
}

/** The checklist's steps, in the order the card draws them. */
export const WELCOME_STEPS: WelcomeStep[] = [
  { key: 'send', label: 'gettingStarted.steps.send' },
  { key: 'save', label: 'gettingStarted.steps.save' },
  { key: 'env', label: 'gettingStarted.steps.env' },
  { key: 'import', label: 'gettingStarted.steps.import' },
  { key: 'capture', label: 'gettingStarted.steps.capture' },
  { key: 'ws', label: 'gettingStarted.steps.ws' },
]
