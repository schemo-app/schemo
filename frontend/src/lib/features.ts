import type { MessageKey } from '../i18n'

type Say = (key: MessageKey, named?: Record<string, unknown>) => string
type Has = (key: string) => key is MessageKey

export const FEATURES = {
  sharedSpaces: 'shared_spaces',
  roles: 'roles',
  audit: 'audit',
  shareLinks: 'share_links',
} as const

/** The plan a device works on when nobody has bought anything: the vocabulary is the server's. */
export const TIER_FREE = 'free'

function keyOf(feature: string): string {
  return feature ? `errors.features.${feature}` : ''
}

/**
 * Returns the localized name of a feature.
 * Falls back to 'errors.features.unknown' if the feature is unknown to this client build.
 */
function featureWord(t: Say, te: Has, feature: string): string {
  const key = keyOf(feature)
  return key && te(key) ? t(key) : t('errors.features.unknown')
}

/**
 * Formats a refusal error message when a required feature is missing.
 */
export function featureRefusal(t: Say, te: Has, feature: string): string {
  return t('errors.codes.featureRequired', { feature: featureWord(t, te, feature) })
}
