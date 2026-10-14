import { AuthType, DraftID, type Auth } from '../../bindings/schemo/internal/domain'
import type { paneSource } from '../stores/content'

export type RequestSource = ReturnType<typeof paneSource>

export const NO_AUTH: Auth = { type: AuthType.AuthNone, fields: {} }

export type ChipName = 'params' | 'headers' | 'auth' | 'body' | 'scripts' | 'docs' | 'examples'

/** Whether a draft belongs to the strip — the command line's or a tab's — and has no tree above it.
 *  What Go calls IsTabDraft, said once more where the window needs the same answer. */
export function isTabDraft(id: string | null): boolean {
  return id === DraftID.DraftCommandLine || (id?.startsWith('tab:') ?? false)
}
